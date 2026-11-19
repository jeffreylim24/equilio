package main

import "net/http"

// routes returns the HTTP handler with every endpoint registered. main serves it, and
// tests call it too, so they go through the same method and path matching as production.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /accounts", s.handleCreateAccount)
	mux.HandleFunc("GET /accounts/{id}", s.handleGetAccountByID)
	mux.HandleFunc("POST /transfers", s.handleCreateTransfer)
	return mux
}
