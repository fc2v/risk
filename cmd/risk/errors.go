package main

import (
	"fmt"
	"net/http"
)

func (a *api) logError(r *http.Request, err error) {
	a.logger.Error("request failed", "err", err, "method", r.Method, "path", r.URL.Path)
}

func (a *api) errorResponse(w http.ResponseWriter, r *http.Request, status int, message any) {
	env := envelope{"error": message}

	if err := a.writeJSON(w, status, env, nil); err != nil {
		a.logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func (a *api) serverErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	a.logError(r, err)

	message := "the server encountered a problem and could not process your request"
	a.errorResponse(w, r, http.StatusInternalServerError, message)
}

func (a *api) notFoundResponse(w http.ResponseWriter, r *http.Request) {
	message := "the requested resource could not be found"
	a.errorResponse(w, r, http.StatusNotFound, message)
}

func (a *api) failedValidationResponse(w http.ResponseWriter, r *http.Request, errors map[string]string) {
	a.errorResponse(w, r, http.StatusUnprocessableEntity, errors)
}

// A missing token gets no error code, per RFC 6750 §3.1.
func (a *api) unauthenticatedResponse(w http.ResponseWriter, r *http.Request, tokenSent bool) {
	challenge := `Bearer realm="risk"`
	if tokenSent {
		challenge += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	a.errorResponse(w, r, http.StatusUnauthorized, "valid authentication credentials are required")
}

func (a *api) forbiddenResponse(w http.ResponseWriter, r *http.Request) {
	a.errorResponse(w, r, http.StatusForbidden, "you are not allowed to access this resource")
}

func (a *api) methodNotAllowedResponse(w http.ResponseWriter, r *http.Request) {
	message := fmt.Sprintf("the %s method is not supported for this resource", r.Method)
	a.errorResponse(w, r, http.StatusMethodNotAllowed, message)
}
