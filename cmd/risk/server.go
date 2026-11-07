package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"risk/auth"
	"risk/db"
	"risk/metrics"
)

const httpShutdownTimeout = 5 * time.Second

type api struct {
	logger       *slog.Logger
	metrics      *metrics.Metrics
	evaluatedTxs db.EvaluatedTxModel
	verifier     tokenVerifier

	readinessChecks map[string]func(context.Context) error
}

type tokenVerifier interface {
	Verify(ctx context.Context, token string) (auth.Identity, error)
}

// serve listens before returning, so a taken port fails startup rather than going unnoticed.
// With a nil tlsConfig, TLS is left to the mesh or gateway in front.
func (a *api) serve(addr string, tlsConfig *tls.Config) (stop func(), err error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("http listen: %w", err)
	}

	srv := &http.Server{
		Handler:           a.routes(),
		ErrorLog:          slog.NewLogLogger(a.logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
		TLSConfig:         tlsConfig,
	}

	go func() {
		serve := func() error { return srv.Serve(ln) }
		if tlsConfig != nil {
			serve = func() error { return srv.ServeTLS(ln, "", "") }
		}
		if err := serve(); !errors.Is(err, http.ErrServerClosed) {
			a.logger.Error("http server stopped", "err", err)
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), httpShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			a.logger.Error("http shutdown failed", "err", err)
		}
	}, nil
}
