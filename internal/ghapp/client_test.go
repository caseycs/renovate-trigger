package ghapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testClientID = "Iv23liTESTCLIENT"

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return key
}

func encodeContent(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// fakeGitHub is an httptest server standing in for the GitHub REST API. It
// records the App JWTs it receives and counts token mints.
type fakeGitHub struct {
	mu             sync.Mutex
	installStatus  int
	tokenStatus    int
	contentsStatus int
	contentsBody   string
	tokenExpiry    time.Time
	tokenMints     int
	lastAppJWT     string
}

func (f *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/org/lib/installation", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastAppJWT = r.Header.Get("Authorization")
		status := f.installStatus
		f.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			fmt.Fprint(w, `{"id": 42}`)
		}
	})
	mux.HandleFunc("/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokenMints++
		status := f.tokenStatus
		expiry := f.tokenExpiry
		f.mu.Unlock()
		if status == 0 {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
		if status == http.StatusCreated {
			fmt.Fprintf(w, `{"token":"ghs_test","expires_at":%q}`, expiry.Format(time.RFC3339))
		}
	})
	mux.HandleFunc("/repos/org/lib/contents/renovate.trigger.json", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		status := f.contentsStatus
		body := f.contentsBody
		f.mu.Unlock()
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			fmt.Fprintf(w, `{"encoding":"base64","content":%q}`, body)
		}
	})
	return mux
}

func newFixture(t *testing.T, f *fakeGitHub, now func() time.Time) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	opts := []Option{WithBaseURL(srv.URL)}
	if now != nil {
		opts = append(opts, WithClock(now))
	}
	return NewClient(testClientID, testKey(t), opts...), srv
}

func TestFetchTriggerFileSuccess(t *testing.T) {
	f := &fakeGitHub{
		contentsBody: encodeContent(`{"tags":["org/app-a","org/app-b"]}`),
		tokenExpiry:  time.Now().Add(time.Hour),
	}
	c, _ := newFixture(t, f, nil)

	deps, found, err := c.FetchTriggerFile(context.Background(), "org/lib")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if len(deps) != 2 || deps[0] != "org/app-a" || deps[1] != "org/app-b" {
		t.Errorf("deps = %v, want [org/app-a org/app-b]", deps)
	}
}

func TestAppJWTHasClientIDAndRS256(t *testing.T) {
	key := testKey(t)
	f := &fakeGitHub{
		contentsBody: encodeContent(`{"tags":[]}`),
		tokenExpiry:  time.Now().Add(time.Hour),
	}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	fixedNow := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	c := NewClient(testClientID, key, WithBaseURL(srv.URL), WithClock(func() time.Time { return fixedNow }))

	if _, _, err := c.FetchTriggerFile(context.Background(), "org/lib"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	f.mu.Lock()
	auth := f.lastAppJWT
	f.mu.Unlock()
	raw, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok {
		t.Fatalf("Authorization = %q, want Bearer <jwt>", auth)
	}

	var claims jwt.RegisteredClaims
	tok, err := jwt.ParseWithClaims(raw, &claims, func(tok *jwt.Token) (any, error) {
		if tok.Method.Alg() != "RS256" {
			return nil, fmt.Errorf("alg = %s, want RS256", tok.Method.Alg())
		}
		return &key.PublicKey, nil
	}, jwt.WithTimeFunc(func() time.Time { return fixedNow })) // validate exp against the same fixed clock the token was minted with
	if err != nil {
		t.Fatalf("parsing app jwt: %v", err)
	}
	if !tok.Valid {
		t.Fatal("app jwt not valid")
	}
	if claims.Issuer != testClientID {
		t.Errorf("iss = %q, want %q", claims.Issuer, testClientID)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.After(fixedNow.Add(10*time.Minute)) {
		t.Errorf("exp = %v, want within 10 minutes of %v", claims.ExpiresAt, fixedNow)
	}
}

func TestFetchTriggerFileNotFound(t *testing.T) {
	f := &fakeGitHub{
		contentsStatus: http.StatusNotFound,
		tokenExpiry:    time.Now().Add(time.Hour),
	}
	c, _ := newFixture(t, f, nil)

	deps, found, err := c.FetchTriggerFile(context.Background(), "org/lib")
	if err != nil {
		t.Fatalf("404 should not be an error, got: %v", err)
	}
	if found {
		t.Error("found = true, want false for 404")
	}
	if deps != nil {
		t.Errorf("deps = %v, want nil", deps)
	}
}

func TestFetchTriggerFileServerError(t *testing.T) {
	f := &fakeGitHub{
		contentsStatus: http.StatusInternalServerError,
		tokenExpiry:    time.Now().Add(time.Hour),
	}
	c, _ := newFixture(t, f, nil)

	if _, _, err := c.FetchTriggerFile(context.Background(), "org/lib"); err == nil {
		t.Fatal("expected error for 500 from contents endpoint")
	}
}

func TestFetchTriggerFileMalformedJSON(t *testing.T) {
	f := &fakeGitHub{
		contentsBody: encodeContent(`{"tags": not-json}`),
		tokenExpiry:  time.Now().Add(time.Hour),
	}
	c, _ := newFixture(t, f, nil)

	if _, _, err := c.FetchTriggerFile(context.Background(), "org/lib"); err == nil {
		t.Fatal("expected error for malformed trigger declaration")
	}
}

func TestInstallationNotFoundIsError(t *testing.T) {
	f := &fakeGitHub{
		installStatus: http.StatusNotFound,
		tokenExpiry:   time.Now().Add(time.Hour),
	}
	c, _ := newFixture(t, f, nil)

	if _, _, err := c.FetchTriggerFile(context.Background(), "org/lib"); err == nil {
		t.Fatal("expected error when the App is not installed on the repo")
	}
}

func TestInstallationTokenCachedAndReminted(t *testing.T) {
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	current := base
	f := &fakeGitHub{
		contentsBody: encodeContent(`{"tags":["org/app-a"]}`),
		tokenExpiry:  base.Add(time.Hour),
	}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := NewClient(testClientID, testKey(t), WithBaseURL(srv.URL), WithClock(func() time.Time { return current }))

	// First two fetches within the token's validity → one mint (cached).
	for i := range 2 {
		if _, _, err := c.FetchTriggerFile(context.Background(), "org/lib"); err != nil {
			t.Fatalf("fetch %d error: %v", i, err)
		}
	}
	f.mu.Lock()
	mints := f.tokenMints
	f.mu.Unlock()
	if mints != 1 {
		t.Fatalf("token mints = %d, want 1 (cached)", mints)
	}

	// Advance past expiry → re-mint.
	current = base.Add(2 * time.Hour)
	if _, _, err := c.FetchTriggerFile(context.Background(), "org/lib"); err != nil {
		t.Fatalf("post-expiry fetch error: %v", err)
	}
	f.mu.Lock()
	mints = f.tokenMints
	f.mu.Unlock()
	if mints != 2 {
		t.Errorf("token mints = %d, want 2 (re-minted after expiry)", mints)
	}
}

func TestSplitRepoInvalid(t *testing.T) {
	c := NewClient(testClientID, testKey(t))
	for _, bad := range []string{"noslash", "", "org/", "/repo", "org/repo/extra"} {
		if _, _, err := c.FetchTriggerFile(context.Background(), bad); err == nil {
			t.Errorf("FetchTriggerFile(%q) expected error", bad)
		}
	}
}
