package api

import (
	"apiserver/internal/graph"
	"encoding/json"
	"net/http"
)

// writeJSON marshals v to JSON and writes it as the response body, setting
// the status code and Content-Type header. Every handler funnels its
// response through here so that behavior stays consistent in one place.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// simple test struct for hello
type helloResponse struct {
	Message string `json:"message"`
}

// Struct for the entire graph response
type wholeGraphResponse struct {
	Graph []graph.NodeSnapshot `json:"graph"`
}

// handler for hello route
func handleHello(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, helloResponse{Message: "Hello, world!"})
}

// handleGetGraph is a method on *server (not a plain function like the
// others) so it can reach s.store via the receiver.
func (s *server) handleGetGraph(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, wholeGraphResponse{Graph: s.store.Snapshot()})
}

// handleGetDevices is also method on *server (not a plain function like the
// others) so it can reach s.store via the receiver.
func (s *server) handleGetDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, wholeGraphResponse{Graph: s.store.GetAllNodes()})
}
