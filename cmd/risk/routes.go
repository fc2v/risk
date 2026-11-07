package main

import "net/http"

func (a *api) routes() http.Handler {
	r := newRouter(http.HandlerFunc(a.methodNotAllowedResponse))
	r.mux.HandleFunc("/", a.notFoundResponse)

	r.handle(http.MethodGet, "/v1/healthcheck", http.HandlerFunc(a.healthcheckHandler))
	r.handle(http.MethodGet, "/v1/readycheck", http.HandlerFunc(a.readycheckHandler))
	r.handle(http.MethodGet, "/metrics", a.metrics.Handler())

	r.handle(http.MethodGet, "/v1/evaluated-txs", a.requireAuth(http.HandlerFunc(a.listEvaluatedTxsHandler)))
	r.handle(http.MethodGet, "/v1/evaluated-txs/{txID}", a.requireAuth(http.HandlerFunc(a.showEvaluatedTxHandler)))

	return a.recoverPanic(r.mux)
}

// router answers a known path with the wrong method with 405 rather than the catch-all 404,
// since a method-less pattern loses to the same path with a method.
type router struct {
	mux        *http.ServeMux
	paths      map[string]bool
	notAllowed http.Handler
}

func newRouter(notAllowed http.Handler) *router {
	return &router{mux: http.NewServeMux(), paths: make(map[string]bool), notAllowed: notAllowed}
}

func (r *router) handle(method, path string, h http.Handler) {
	r.mux.Handle(method+" "+path, h)
	if !r.paths[path] {
		r.paths[path] = true
		r.mux.Handle(path, r.notAllowed)
	}
}
