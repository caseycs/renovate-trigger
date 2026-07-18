// Package ghapp is a minimal GitHub App client: it authenticates as the App
// using a client-ID RS256 JWT, discovers the installation for a repository,
// caches installation tokens, and reads a repository's trigger declaration.
package ghapp

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultBaseURL    = "https://api.github.com"
	triggerFileName   = "renovate.trigger.json"
	tokenExpiryBuffer = time.Minute
	jwtLifetime       = 9 * time.Minute  // GitHub caps the App JWT at 10 minutes
	jwtClockSkew      = 60 * time.Second // backdate iat to tolerate clock drift
	requestTimeout    = 10 * time.Second
)

// Client reads trigger declarations from GitHub as an installed GitHub App.
type Client struct {
	clientID   string
	key        *rsa.PrivateKey
	httpClient *http.Client
	baseURL    string
	now        func() time.Time

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the GitHub API base URL (used by tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithClock overrides the time source (used by tests for JWT/token expiry).
func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

// NewClient builds a GitHub App client for the given App client ID and RSA
// private key. Options are applied in order.
func NewClient(clientID string, key *rsa.PrivateKey, opts ...Option) *Client {
	c := &Client{
		clientID:   clientID,
		key:        key,
		httpClient: &http.Client{Timeout: requestTimeout},
		baseURL:    defaultBaseURL,
		now:        time.Now,
		tokens:     make(map[int64]cachedToken),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// FetchTriggerFile reads repo's ("owner/name") renovate.trigger.json from its
// default branch and returns the declared dependents. found is false when the
// repository has no trigger declaration (HTTP 404); any other failure is an
// error.
func (c *Client) FetchTriggerFile(ctx context.Context, repo string) (deps []string, found bool, err error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, false, err
	}

	instID, err := c.installationID(ctx, owner, name)
	if err != nil {
		return nil, false, err
	}
	token, err := c.installationToken(ctx, instID)
	if err != nil {
		return nil, false, err
	}
	return c.readTriggerFile(ctx, owner, name, token)
}

func (c *Client) appJWT() (string, error) {
	now := c.now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    c.clientID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-jwtClockSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(jwtLifetime)),
	})
	signed, err := token.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("signing app jwt: %w", err)
	}
	return signed, nil
}

func (c *Client) installationID(ctx context.Context, owner, name string) (int64, error) {
	appJWT, err := c.appJWT()
	if err != nil {
		return 0, err
	}
	status, body, err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/repos/%s/%s/installation", owner, name), "Bearer "+appJWT, nil)
	if err != nil {
		return 0, fmt.Errorf("getting installation for %s/%s: %w", owner, name, err)
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("getting installation for %s/%s: unexpected status %d", owner, name, status)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, fmt.Errorf("parsing installation for %s/%s: %w", owner, name, err)
	}
	return out.ID, nil
}

func (c *Client) installationToken(ctx context.Context, instID int64) (string, error) {
	c.mu.Lock()
	if t, ok := c.tokens[instID]; ok && c.now().Before(t.expires.Add(-tokenExpiryBuffer)) {
		c.mu.Unlock()
		return t.token, nil
	}
	c.mu.Unlock()

	appJWT, err := c.appJWT()
	if err != nil {
		return "", err
	}
	status, body, err := c.do(ctx, http.MethodPost,
		fmt.Sprintf("/app/installations/%d/access_tokens", instID), "Bearer "+appJWT, nil)
	if err != nil {
		return "", fmt.Errorf("creating installation token for %d: %w", instID, err)
	}
	if status != http.StatusCreated {
		return "", fmt.Errorf("creating installation token for %d: unexpected status %d", instID, status)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parsing installation token for %d: %w", instID, err)
	}

	c.mu.Lock()
	c.tokens[instID] = cachedToken{token: out.Token, expires: out.ExpiresAt}
	c.mu.Unlock()
	return out.Token, nil
}

func (c *Client) readTriggerFile(ctx context.Context, owner, name, token string) ([]string, bool, error) {
	// No ?ref= is sent, so the contents API serves the file from the repo's
	// default branch — which is exactly what we want.
	status, body, err := c.do(ctx, http.MethodGet,
		fmt.Sprintf("/repos/%s/%s/contents/%s", owner, name, triggerFileName), "token "+token, nil)
	if err != nil {
		return nil, false, fmt.Errorf("fetching %s from %s/%s: %w", triggerFileName, owner, name, err)
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if status != http.StatusOK {
		return nil, false, fmt.Errorf("fetching %s from %s/%s: unexpected status %d", triggerFileName, owner, name, status)
	}

	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &content); err != nil {
		return nil, false, fmt.Errorf("parsing contents response for %s/%s: %w", owner, name, err)
	}
	// The contents API returns base64 with embedded newlines.
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return nil, false, fmt.Errorf("decoding %s from %s/%s: %w", triggerFileName, owner, name, err)
	}

	var decl struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(decoded, &decl); err != nil {
		return nil, false, fmt.Errorf("parsing %s from %s/%s: %w", triggerFileName, owner, name, err)
	}
	return decl.Tags, true, nil
}

func (c *Client) do(ctx context.Context, method, path, auth string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

func splitRepo(repo string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("invalid repository %q: want owner/name", repo)
	}
	return owner, name, nil
}
