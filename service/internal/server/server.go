// Package server wires the broker handlers into an http.Server.
package server

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"time"

	"github.com/kube-argos/kargos/service/internal/broker"
)

// New builds the broker HTTP server. Routing uses the stdlib ServeMux
// method+path patterns (Go 1.22+); no router dependency.
func New(addr string, b *broker.Broker, log *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", b.Discovery)
	mux.HandleFunc("GET /jwks", b.JWKS)
	mux.HandleFunc("GET /authorize", b.Authorize)
	mux.HandleFunc("GET /callback", b.Callback)
	mux.HandleFunc("POST /token", b.Token)
	mux.HandleFunc("GET /healthz", b.Healthz)

	return &http.Server{
		Addr:              addr,
		Handler:           logging(log, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // callback waits on the operator
		IdleTimeout:       120 * time.Second,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

// logging records one line per request, skipping health probes to avoid noise.
func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		log.Info("request",
			"method", r.Method, "path", r.URL.Path,
			"status", sw.status, "dur", time.Since(start).String())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
