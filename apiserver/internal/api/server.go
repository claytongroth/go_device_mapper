// Package api holds the HTTP layer: routing and handlers. cmd/apiserver
// just wires this up to a listener and handles process lifecycle.
package api

import (
	"net/http"

	"apiserver/internal/graph"
)

// TODO:
// - middlewares for auth (DB)
// Endpoint for "Device", "All nodes connected to this device"
// Add X,Y Coords for each device (and maybe fake MAC addresses)
// recomment code

// server bundles whatever handlers need access to — just the graph store
// for now, likely more later (a logger, config, ...). Handlers that need
// it become methods on *server instead of plain functions, so they reach
// it via the receiver (s.store) rather than needing it threaded in some
// other way. Unexported since nothing outside this package needs to name
// the type directly — NewMux is the only thing callers touch.
type server struct {
	store *graph.Store
}

// NewMux builds the route table. Go 1.22+'s http.ServeMux supports method
// prefixes ("GET /path") and path wildcards natively, so a plain stdlib mux
// covers this project's API without pulling in a router dependency.
//
// Return type is http.Handler, not *http.ServeMux, since Chain (used below,
// per-route) hands back an http.Handler rather than the concrete mux type.
// main.go's http.Server.Handler field is already typed http.Handler, so
// this doesn't require any change at the call site.
func NewMux(store *graph.Store) http.Handler {
	s := &server{store: store}

	// rateLimit is constructed once and reused across every route below —
	// calling RateLimitMiddleware separately per route would give each
	// route its own independent 10/s budget (30/s combined across three
	// routes), instead of one shared 10/s budget for the whole API.
	rateLimit := RateLimitMiddleware(10, 10)

	mux := http.NewServeMux()
	// mux.Handle (not HandleFunc) is needed everywhere here — HandleFunc
	// only takes a plain func(w, r), but Chain returns an http.Handler, so
	// each handler has to become one first via http.HandlerFunc(...)
	// before it can be wrapped.
	mux.Handle("GET /hello", Chain(http.HandlerFunc(handleHello), LoggingMiddleware, rateLimit))
	mux.Handle("GET /graph", Chain(http.HandlerFunc(s.handleGetGraph), LoggingMiddleware, rateLimit))
	mux.Handle("GET /devices", Chain(http.HandlerFunc(s.handleGetDevices), LoggingMiddleware, rateLimit))

	return mux
}
