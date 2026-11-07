package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"risk/db"
	"risk/validator"
)

func TestCursorRoundTrip(t *testing.T) {
	for _, c := range []db.EvaluatedTxCursor{
		{PostedAt: time.Date(2026, 11, 9, 2, 10, 0, 123456789, time.UTC), ID: "9b2e7c41-0d3f"},
		{PostedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ID: "id|with|pipes"},
	} {
		encoded := encodeCursor(&c)
		v := validator.New()
		got := readCursor(url.Values{"cursor": {*encoded}}, "cursor", v)
		if !v.Valid() || got == nil || !got.PostedAt.Equal(c.PostedAt) || got.ID != c.ID {
			t.Errorf("round trip of %+v = %+v, errors %v", c, got, v.Errors)
		}
	}
	if encodeCursor(nil) != nil {
		t.Error("nil cursor encoded as non-nil")
	}
}

func TestReadCursorRejects(t *testing.T) {
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, raw := range map[string]string{
		"not base64":   "!!!",
		"no separator": b64("2026-11-09T02:10:00Z"),
		"bad time":     b64("yesterday|tx-1"),
		"no id":        b64("2026-11-09T02:10:00Z|"),
	} {
		t.Run(name, func(t *testing.T) {
			v := validator.New()
			if got := readCursor(url.Values{"cursor": {raw}}, "cursor", v); got != nil || v.Valid() {
				t.Errorf("accepted %q as %+v", raw, got)
			}
		})
	}
}

func TestListRejectsBadQuery(t *testing.T) {
	cases := map[string]struct {
		query     string
		wantField string
	}{
		"page size zero":        {"page_size=0", "page_size"},
		"page size too big":     {"page_size=101", "page_size"},
		"page size not integer": {"page_size=ten", "page_size"},
		"unknown risk":          {"risk=SEVERE", "risk"},
		"lowercase risk":        {"risk=high", "risk"},
		"bad from":              {"from=2026-11-09", "from"},
		"to before from":        {"from=2026-11-09T00:00:00Z&to=2026-11-08T00:00:00Z", "to"},
		"garbage cursor":        {"cursor=@@@", "cursor"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testAPI(nil).routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/evaluated-txs?"+tc.query, nil))
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", rec.Code)
			}
			var body struct {
				Error map[string]string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || body.Error[tc.wantField] == "" {
				t.Errorf("error body %v (%v), want a message for %q", body.Error, err, tc.wantField)
			}
		})
	}
}
