package server

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
)

type Server struct {
	httpServer *http.Server
	k8sClient  kubernetes.Interface
	logger     *slog.Logger

	readyMu    sync.RWMutex
	readyCache bool
	readyAt    time.Time
}

func New(addr string, webhookHandler http.Handler, k8sClient kubernetes.Interface, logger *slog.Logger) *Server {
	s := &Server{
		k8sClient: k8sClient,
		logger:    logger,
	}

	mux := http.NewServeMux()
	mux.Handle("/webhook", webhookHandler)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)

	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

func (s *Server) Start() error {
	s.logger.Info("server starting", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("server shutting down")
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	s.readyMu.RLock()
	cached := s.readyCache
	cachedAt := s.readyAt
	s.readyMu.RUnlock()

	if time.Since(cachedAt) < 5*time.Second {
		if cached {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready"))
		}
		return
	}

	_, err := s.k8sClient.Discovery().ServerVersion()
	ready := err == nil

	s.readyMu.Lock()
	s.readyCache = ready
	s.readyAt = time.Now()
	s.readyMu.Unlock()

	if ready {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	} else {
		s.logger.Warn("readiness check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
	}
}
