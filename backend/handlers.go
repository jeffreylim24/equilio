package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeffreylim24/equilio/backend/db"
)

// Server holds the dependencies shared by the HTTP handlers.
type Server struct {
	queries *db.Queries
	pool    *pgxpool.Pool
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

// sendError writes {"error": msg} with the given status code.
func sendError(w http.ResponseWriter, status int, msg string) {
	sendJSON(w, status, map[string]string{"error": msg})
}

// sendInternalError logs err with a label saying where it happened and sends a
// generic 500. The real error never goes to the client: it can expose SQL details.
func sendInternalError(w http.ResponseWriter, label string, err error) {
	log.Printf("%s: %v", label, err)
	sendError(w, http.StatusInternalServerError, "internal error")
}

// handleCreateAccount handles POST /accounts. The body is {"name": "..."};
// it returns 201 with the new account. Balance always starts at 0: the API
// never creates money.
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// The DB's name_not_blank CHECK is the real guarantee; this gives a clear 400.
	if strings.TrimSpace(req.Name) == "" {
		sendError(w, http.StatusBadRequest, "name must be non-empty")
		return
	}

	account, err := s.queries.CreateAccount(r.Context(), req.Name)
	if err != nil {
		sendInternalError(w, "create account", err)
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
		sendError(w, http.StatusBadRequest, "invalid ID")
		return
	}

	account, err := s.queries.GetAccountByID(r.Context(), n)
	if errors.Is(err, pgx.ErrNoRows) {
		sendError(w, http.StatusNotFound, fmt.Sprintf("account %d not found", n))
		return
	}
	if err != nil {
		sendInternalError(w, "get account by ID", err)
		return
	}

	sendJSON(w, http.StatusOK, account)
}

func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromAccountID int64 `json:"from_account_id"`
		ToAccountID   int64 `json:"to_account_id"`
		Amount        int64 `json:"amount"`
	}

	// Validate input fields
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.FromAccountID <= 0 || req.ToAccountID <= 0 {
		sendError(w, http.StatusBadRequest, "invalid ID")
		return
	}
	if req.FromAccountID == req.ToAccountID {
		sendError(w, http.StatusBadRequest, "cannot transfer to the same account")
		return
	}
	if req.Amount <= 0 {
		sendError(w, http.StatusBadRequest, "invalid amount")
		return
	}

	// BEGIN
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		sendInternalError(w, "begin", err)
		return
	}
	defer tx.Rollback(r.Context())
	qtx := s.queries.WithTx(tx)

	// Verify accounts exist and have sufficient balance
	fromAccount, err := qtx.GetAccountByID(r.Context(), req.FromAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		sendError(w, http.StatusNotFound, fmt.Sprintf("from account %d not found", req.FromAccountID))
		return
	}
	if err != nil {
		sendInternalError(w, "get from account", err)
		return
	}
	_, err = qtx.GetAccountByID(r.Context(), req.ToAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		sendError(w, http.StatusNotFound, fmt.Sprintf("to account %d not found", req.ToAccountID))
		return
	}
	if err != nil {
		sendInternalError(w, "get to account", err)
		return
	}
	if req.Amount > fromAccount.Balance {
		sendError(w, http.StatusConflict, "insufficient balance")
		return
	}

	_, err = qtx.DeductAccountBalance(r.Context(), db.DeductAccountBalanceParams{
		Amount: req.Amount,
		ID:     req.FromAccountID,
	})
	if err != nil {
		sendInternalError(w, "deduct balance", err)
		return
	}
	_, err = qtx.AddAccountBalance(r.Context(), db.AddAccountBalanceParams{
		Amount: req.Amount,
		ID:     req.ToAccountID,
	})
	if err != nil {
		sendInternalError(w, "add balance", err)
		return
	}
	transfer, err := qtx.CreateNewTransfer(r.Context(), db.CreateNewTransferParams{
		FromAccountID: req.FromAccountID,
		ToAccountID:   req.ToAccountID,
		Amount:        req.Amount,
	})
	if err != nil {
		sendInternalError(w, "create new transfer", err)
		return
	}

	// COMMIT
	if err := tx.Commit(r.Context()); err != nil {
		sendInternalError(w, "commit", err)
		return
	}
	sendJSON(w, http.StatusCreated, transfer)
}
