package main

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"risk/db"
	"risk/risk"
	"risk/risk/contract"
	"risk/validator"
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
)

type evaluatedTxResponse struct {
	contract.EvaluatedTx
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

func (a *api) showEvaluatedTxHandler(w http.ResponseWriter, r *http.Request) {
	et, found, err := a.evaluatedTxs.GetEvaluated(r.Context(), r.PathValue("txID"))
	if err != nil {
		a.serverErrorResponse(w, r, err)
		return
	}
	if !found {
		a.notFoundResponse(w, r)
		return
	}

	if err := a.writeJSON(w, http.StatusOK, envelope{"evaluatedTx": toResponse(et)}, nil); err != nil {
		a.serverErrorResponse(w, r, err)
	}
}

func (a *api) listEvaluatedTxsHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	v := validator.New()

	f := db.EvaluatedTxFilter{
		AccountID:    qs.Get("account_id"),
		PostedFrom:   readTime(qs, "from", v),
		PostedBefore: readTime(qs, "to", v),
		Risk:         risk.Risk(qs.Get("risk")),
		After:        readCursor(qs, "cursor", v),
		PageSize:     readInt(qs, "page_size", defaultPageSize, v),
	}

	v.Check(f.PageSize >= 1 && f.PageSize <= maxPageSize, "page_size", "must be between 1 and "+strconv.Itoa(maxPageSize))
	v.Check(f.Risk == "" || validator.In(string(f.Risk),
		string(risk.RiskUnknown), string(risk.RiskLow), string(risk.RiskMedium), string(risk.RiskHigh)),
		"risk", "must be UNKNOWN, LOW, MEDIUM or HIGH")
	v.Check(f.PostedFrom.IsZero() || f.PostedBefore.IsZero() || f.PostedFrom.Before(f.PostedBefore), "to", "must be after from")
	if !v.Valid() {
		a.failedValidationResponse(w, r, v.Errors)
		return
	}

	page, next, err := a.evaluatedTxs.List(r.Context(), f)
	if err != nil {
		a.serverErrorResponse(w, r, err)
		return
	}

	evaluatedTxs := make([]evaluatedTxResponse, 0, len(page))
	for _, et := range page {
		evaluatedTxs = append(evaluatedTxs, toResponse(et))
	}

	env := envelope{"evaluatedTxs": evaluatedTxs, "nextCursor": encodeCursor(next)}
	if err := a.writeJSON(w, http.StatusOK, env, nil); err != nil {
		a.serverErrorResponse(w, r, err)
	}
}

func toResponse(et risk.EvaluatedTx) evaluatedTxResponse {
	return evaluatedTxResponse{EvaluatedTx: contract.FromEvaluated(et), EvaluatedAt: et.CreatedAt.UTC()}
}

func encodeCursor(c *db.EvaluatedTxCursor) *string {
	if c == nil {
		return nil
	}
	s := base64.RawURLEncoding.EncodeToString([]byte(c.PostedAt.Format(time.RFC3339Nano) + "|" + c.ID))
	return &s
}

func readCursor(qs url.Values, key string, v *validator.Validator) *db.EvaluatedTxCursor {
	s := qs.Get(key)
	if s == "" {
		return nil
	}

	raw, decodeErr := base64.RawURLEncoding.DecodeString(s)
	postedAt, id, found := strings.Cut(string(raw), "|")
	t, parseErr := time.Parse(time.RFC3339Nano, postedAt)
	if decodeErr != nil || parseErr != nil || !found || id == "" {
		v.AddError(key, "must be a nextCursor from a previous page")
		return nil
	}
	return &db.EvaluatedTxCursor{PostedAt: t, ID: id}
}
