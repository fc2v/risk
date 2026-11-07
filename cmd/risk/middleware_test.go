package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"risk/auth"
	"risk/metrics"
)

type fakeVerifier map[string]error

func (f fakeVerifier) Verify(_ context.Context, token string) (auth.Identity, error) {
	if err, ok := f[token]; ok {
		return auth.Identity{}, err
	}
	return auth.Identity{Issuer: "cluster", Subject: "svc-" + token}, nil
}

func testAPI(v tokenVerifier) *api {
	return &api{
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics:  metrics.New(),
		verifier: v,
	}
}

func TestRequireAuth(t *testing.T) {
	verifier := fakeVerifier{
		"bad":    fmt.Errorf("%w: expired", auth.ErrUnauthenticated),
		"denied": fmt.Errorf("%w: not allowed", auth.ErrForbidden),
	}
	const unchallenged, invalid = `Bearer realm="risk"`, `Bearer realm="risk", error="invalid_token"`

	cases := []struct {
		name          string
		verifier      tokenVerifier
		authorization string
		wantStatus    int
		wantChallenge string
		wantCaller    string
	}{
		{"auth off", nil, "", http.StatusOK, "", ""},
		{"no header", verifier, "", http.StatusUnauthorized, unchallenged, ""},
		{"not bearer", verifier, "Basic dXNlcjpwYXNz", http.StatusUnauthorized, unchallenged, ""},
		{"empty bearer", verifier, "Bearer ", http.StatusUnauthorized, unchallenged, ""},
		{"valid", verifier, "Bearer good", http.StatusOK, "", "cluster:svc-good"},
		{"scheme is case-insensitive", verifier, "bearer good", http.StatusOK, "", "cluster:svc-good"},
		{"rejected token", verifier, "Bearer bad", http.StatusUnauthorized, invalid, ""},
		{"caller not allowed", verifier, "Bearer denied", http.StatusForbidden, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var caller string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if id, ok := callerFrom(r.Context()); ok {
					caller = id.Issuer + ":" + id.Subject
				}
			})

			req := httptest.NewRequest(http.MethodGet, "/v1/evaluated-txs", nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			rec := httptest.NewRecorder()
			testAPI(tc.verifier).requireAuth(next).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.wantChallenge {
				t.Errorf("WWW-Authenticate %q, want %q", got, tc.wantChallenge)
			}
			if caller != tc.wantCaller {
				t.Errorf("caller in context %q, want %q", caller, tc.wantCaller)
			}
		})
	}
}

func TestRoutesProtectOnlyTheAPI(t *testing.T) {
	handler := testAPI(fakeVerifier{}).routes()

	for path, want := range map[string]int{
		"/v1/evaluated-txs":     http.StatusUnauthorized,
		"/v1/evaluated-txs/tx1": http.StatusUnauthorized,
		"/v1/healthcheck":       http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s without a token = %d, want %d", path, rec.Code, want)
		}
	}
}
