package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"risk/auth"
)

type callerKey struct{}

func callerFrom(ctx context.Context) (auth.Identity, bool) {
	id, ok := ctx.Value(callerKey{}).(auth.Identity)
	return id, ok
}

func (a *api) requireAuth(next http.Handler) http.Handler {
	if a.verifier == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || token == "" {
			a.unauthenticatedResponse(w, r, false)
			return
		}

		id, err := a.verifier.Verify(r.Context(), token)
		if err != nil {
			a.logger.Warn("request rejected", "err", err, "method", r.Method, "path", r.URL.Path)
			if errors.Is(err, auth.ErrForbidden) {
				a.forbiddenResponse(w, r)
				return
			}
			a.unauthenticatedResponse(w, r, true)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, id)))
	})
}

func (a *api) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.Header().Set("Connection", "close")
				a.serverErrorResponse(w, r, fmt.Errorf("%v", err))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
