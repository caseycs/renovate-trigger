package webhook

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

type BatchAdder interface {
	Add(repo string)
}

type Handler struct {
	secret  string
	repos   map[string]struct{}
	batch   BatchAdder
	logger  *slog.Logger
}

func NewHandler(secret string, repos map[string]struct{}, batch BatchAdder, logger *slog.Logger) *Handler {
	return &Handler{
		secret: secret,
		repos:  repos,
		batch:  batch,
		logger: logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20)) // 1MB limit
	if err != nil {
		h.logger.Warn("failed to read request body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if !ValidateSignature(body, h.secret, sig) {
		h.logger.Warn("invalid webhook signature", "remote_addr", r.RemoteAddr)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	if event != "create" {
		h.logger.Debug("event ignored", "reason", "not a create event", "event", event)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "not a create event"})
		return
	}

	var payload CreateEvent
	if err := json.Unmarshal(body, &payload); err != nil {
		h.logger.Warn("failed to parse webhook payload", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if payload.RefType != "tag" {
		h.logger.Debug("event ignored", "reason", "ref_type is not tag", "ref_type", payload.RefType)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "ref_type is not tag"})
		return
	}

	repo := payload.Repository.FullName
	if _, ok := h.repos[repo]; !ok {
		h.logger.Info("repo not in allowlist", "repo", repo)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": "repo not in allowlist"})
		return
	}

	h.logger.Info("tag event accepted", "repo", repo, "tag", payload.Ref)
	h.batch.Add(repo)

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "repo": repo, "tag": payload.Ref})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
