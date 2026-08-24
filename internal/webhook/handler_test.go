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

	"github.com/ilia/renovate-trigger/internal/graph"
)

type mockBatch struct {
	added []graph.Dependency
}

func (m *mockBatch) Add(dep graph.Dependency) {
	m.added = append(m.added, dep)
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
	batch := &mockBatch{}
	h := NewHandler(secret, batch, testLogger())

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
	want := graph.Dependency{Kind: graph.KindGitHub, Name: "org/repo-a"}
	if len(batch.added) != 1 || batch.added[0] != want {
		t.Errorf("batch.added = %v, want [%v]", batch.added, want)
	}
}

// Any signed tag event is accepted regardless of repo — the opt-in gate is
// resolved later at flush time, not in the handler.
func TestHandlerTagEventNoAllowlist(t *testing.T) {
	secret := "secret"
	batch := &mockBatch{}
	h := NewHandler(secret, batch, testLogger())

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
		t.Errorf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
	want := graph.Dependency{Kind: graph.KindGitHub, Name: "any-org/any-repo"}
	if len(batch.added) != 1 || batch.added[0] != want {
		t.Errorf("batch.added = %v, want [%v]", batch.added, want)
	}
}

func TestHandlerInvalidSignature(t *testing.T) {
	h := NewHandler("secret", &mockBatch{}, testLogger())

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
	h := NewHandler(secret, &mockBatch{}, testLogger())

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
	batch := &mockBatch{}
	h := NewHandler(secret, batch, testLogger())

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
	if len(batch.added) != 0 {
		t.Errorf("batch.added = %v, want empty (branch is not a tag)", batch.added)
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	h := NewHandler("secret", &mockBatch{}, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}
