package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type mockBatch struct {
	added []string
}

func (m *mockBatch) Add(repo string) {
	m.added = append(m.added, repo)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func signBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestHandlerTagEvent(t *testing.T) {
	secret := "test-secret"
	repos := map[string]struct{}{"org/repo-a": {}}
	batch := &mockBatch{}
	h := NewHandler(secret, repos, batch, testLogger())

	payload := CreateEvent{
		Ref:     "v1.0.0",
		RefType: "tag",
		Repository: Repository{
			FullName: "org/repo-a",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody(body, secret))
	req.Header.Set("X-GitHub-Event", "create")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
	if len(batch.added) != 1 || batch.added[0] != "org/repo-a" {
		t.Errorf("batch.added = %v, want [org/repo-a]", batch.added)
	}
}

func TestHandlerInvalidSignature(t *testing.T) {
	h := NewHandler("secret", nil, &mockBatch{}, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
	req.Header.Set("X-GitHub-Event", "create")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestHandlerNonCreateEvent(t *testing.T) {
	secret := "secret"
	h := NewHandler(secret, nil, &mockBatch{}, testLogger())

	body := []byte(`{"action":"push"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody(body, secret))
	req.Header.Set("X-GitHub-Event", "push")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandlerBranchRefType(t *testing.T) {
	secret := "secret"
	repos := map[string]struct{}{"org/repo": {}}
	h := NewHandler(secret, repos, &mockBatch{}, testLogger())

	payload := CreateEvent{
		Ref:        "main",
		RefType:    "branch",
		Repository: Repository{FullName: "org/repo"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody(body, secret))
	req.Header.Set("X-GitHub-Event", "create")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandlerRepoNotInAllowlist(t *testing.T) {
	secret := "secret"
	repos := map[string]struct{}{"org/allowed": {}}
	h := NewHandler(secret, repos, &mockBatch{}, testLogger())

	payload := CreateEvent{
		Ref:        "v1.0.0",
		RefType:    "tag",
		Repository: Repository{FullName: "org/not-allowed"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody(body, secret))
	req.Header.Set("X-GitHub-Event", "create")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandlerEmptyAllowlistAcceptsAll(t *testing.T) {
	secret := "secret"
	emptyRepos := map[string]struct{}{}
	batch := &mockBatch{}
	h := NewHandler(secret, emptyRepos, batch, testLogger())

	payload := CreateEvent{
		Ref:        "v2.0.0",
		RefType:    "tag",
		Repository: Repository{FullName: "any-org/any-repo"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody(body, secret))
	req.Header.Set("X-GitHub-Event", "create")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d (empty allowlist should accept all)", rr.Code, http.StatusAccepted)
	}
	if len(batch.added) != 1 || batch.added[0] != "any-org/any-repo" {
		t.Errorf("batch.added = %v, want [any-org/any-repo]", batch.added)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	h := NewHandler("secret", nil, &mockBatch{}, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}
