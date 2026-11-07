package main

import "net/http"

func (a *api) healthcheckHandler(w http.ResponseWriter, r *http.Request) {
	env := envelope{
		"status":  "available",
		"version": version,
	}

	if err := a.writeJSON(w, http.StatusOK, env, nil); err != nil {
		a.serverErrorResponse(w, r, err)
	}
}
