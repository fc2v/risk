package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

const readinessTimeout = 2 * time.Second

func (a *api) readycheckHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	checks := make(map[string]string, len(a.readinessChecks))
	status, code := "ready", http.StatusOK

	for name, check := range a.readinessChecks {
		wg.Go(func() {
			err := check(ctx)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				a.logger.Warn("readiness check failed", "check", name, "err", err)
				checks[name] = "unavailable"
				status, code = "unavailable", http.StatusServiceUnavailable
				return
			}
			checks[name] = "ok"
		})
	}
	wg.Wait()

	if err := a.writeJSON(w, code, envelope{"status": status, "checks": checks}, nil); err != nil {
		a.serverErrorResponse(w, r, err)
	}
}
