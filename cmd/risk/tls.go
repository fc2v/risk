package main

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// certReloader picks up certificates renewed in place (e.g. by cert-manager) without a restart.
// A renewal that fails to load is logged and the previous certificate keeps serving.
type certReloader struct {
	certFile, keyFile string
	logger            *slog.Logger

	mu      sync.Mutex
	cert    *tls.Certificate
	modTime [2]time.Time
}

func newCertReloader(certFile, keyFile string, logger *slog.Logger) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile, logger: logger}
	modTime, err := r.modTimes()
	if err != nil {
		return nil, err
	}
	if err := r.load(modTime); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	modTime, err := r.modTimes()
	if err == nil && modTime != r.modTime {
		err = r.load(modTime)
	}
	if err != nil {
		r.logger.Warn("tls certificate reload failed, keeping the previous one", "err", err)
		r.modTime = modTime
	}
	return r.cert, nil
}

func (r *certReloader) load(modTime [2]time.Time) error {
	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return fmt.Errorf("load tls certificate: %w", err)
	}
	r.cert, r.modTime = &cert, modTime
	return nil
}

func (r *certReloader) modTimes() ([2]time.Time, error) {
	var times [2]time.Time
	for i, f := range []string{r.certFile, r.keyFile} {
		info, err := os.Stat(f)
		if err != nil {
			return times, fmt.Errorf("stat tls file: %w", err)
		}
		times[i] = info.ModTime()
	}
	return times, nil
}

func serverTLSConfig(r *certReloader) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.GetCertificate}
}
