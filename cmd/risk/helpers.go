package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"risk/validator"
)

type envelope map[string]any

func (a *api) writeJSON(w http.ResponseWriter, status int, data envelope, headers http.Header) error {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(data); err != nil {
		return err
	}

	for key, value := range headers {
		w.Header()[key] = value
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
	return nil
}

func readInt(qs url.Values, key string, defaultValue int, v *validator.Validator) int {
	s := qs.Get(key)
	if s == "" {
		return defaultValue
	}

	i, err := strconv.Atoi(s)
	if err != nil {
		v.AddError(key, "must be an integer value")
		return defaultValue
	}
	return i
}

func readTime(qs url.Values, key string, v *validator.Validator) time.Time {
	s := qs.Get(key)
	if s == "" {
		return time.Time{}
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		v.AddError(key, "must be an RFC 3339 time, e.g. 2026-11-07T14:00:00Z")
		return time.Time{}
	}
	return t
}
