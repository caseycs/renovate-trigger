package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestHealthz(t *testing.T) {
	client := fake.NewSimpleClientset()
	s := New(":0", http.NotFoundHandler(), client, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("healthz status = %d, want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("healthz body = %q, want ok", rr.Body.String())
	}
}

func TestReadyz(t *testing.T) {
	client := fake.NewSimpleClientset()
	s := New(":0", http.NotFoundHandler(), client, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rr, req)

	// fake client should return OK for discovery
	if rr.Code != http.StatusOK {
		t.Errorf("readyz status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestReadyzCached(t *testing.T) {
	client := fake.NewSimpleClientset()
	s := New(":0", http.NotFoundHandler(), client, testLogger())

	// First call populates cache
	req1 := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr1 := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rr1, req1)

	// Second call should use cache
	req2 := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr2 := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rr2, req2)

	if rr2.Code != rr1.Code {
		t.Errorf("cached readyz status = %d, want %d", rr2.Code, rr1.Code)
	}
}

func TestWebhookRouteRegistered(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	s := New(":0", handler, client, testLogger())

	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	rr := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("webhook status = %d, want %d", rr.Code, http.StatusAccepted)
	}
}
