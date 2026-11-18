package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jeffreylim24/equilio/backend/db"
)

// Server holds the dependencies shared by the HTTP handlers.
type Server struct {
	queries *db.Queries
}

// sendJSON writes v as a JSON response with the given status code.
func sendJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json") // must come before WriteHeader
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status is already sent, so all we can do is log.
		log.Printf("encode response: %v", err)
	}
}

// handleCreateAccount handles POST /accounts. The body is {"name": "..."};
// it returns 201 with the new account. Balance always starts at 0: the API
// never creates money.
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	// The DB's name_not_blank CHECK is the real guarantee; this gives a clear 400.
	if strings.TrimSpace(req.Name) == "" {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "name must be non-empty"})
		return
	}

	account, err := s.queries.CreateAccount(r.Context(), req.Name)
	if err != nil {
		// Log the real error; never send it to the client (it can expose SQL details).
		log.Printf("create account: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	sendJSON(w, http.StatusCreated, account)
}

// handleGetAccountByID handles GET /accounts/{id}. It returns 200 with the
// account, 400 if id is not a positive integer, or 404 if no account has it.
func (s *Server) handleGetAccountByID(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ID"})
		return
	}

	account, err := s.queries.GetAccountByID(r.Context(), n)
	if errors.Is(err, pgx.ErrNoRows) {
		sendJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}
	if err != nil {
		log.Printf("get account by ID: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	sendJSON(w, http.StatusOK, account)
}
