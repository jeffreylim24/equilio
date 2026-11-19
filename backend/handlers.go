package main

import (
	"encoding/json"
	"errors"
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

func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromAccountID int64 `json:"from_account_id"`
		ToAccountID   int64 `json:"to_account_id"`
		Amount        int64 `json:"amount"`
	}

	// Validate input fields
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.FromAccountID <= 0 || req.ToAccountID <= 0 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid ID"})
		return
	}
	if req.Amount <= 0 {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid amount"})
		return
	}

	// BEGIN
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		log.Printf("begin: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	defer tx.Rollback(r.Context())
	qtx := s.queries.WithTx(tx)

	// Verify accounts exist and have sufficient balance
	fromAccount, err1 := qtx.GetAccountByID(r.Context(), req.FromAccountID)
	toAccount, err2 := qtx.GetAccountByID(r.Context(), req.ToAccountID)
	if err := errors.Join(err1, err2); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
			return
		}
		log.Printf("get account by ID: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if fromAccount == toAccount {
		sendJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot transfer to the same account"})
		return
	}
	if req.Amount > fromAccount.Balance {
		sendJSON(w, http.StatusConflict, map[string]string{"error": "insufficient balance"})
		return
	}

	_, err = qtx.DeductAccountBalance(r.Context(), db.DeductAccountBalanceParams{
		Amount: req.Amount,
		ID:     req.FromAccountID,
	})
	if err != nil {
		log.Printf("deduct balance: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	_, err = qtx.AddAccountBalance(r.Context(), db.AddAccountBalanceParams{
		Amount: req.Amount,
		ID:     req.ToAccountID,
	})
	if err != nil {
		log.Printf("add balance: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	transfer, err := qtx.CreateNewTransfer(r.Context(), db.CreateNewTransferParams{
		FromAccountID: req.FromAccountID,
		ToAccountID:   req.ToAccountID,
		Amount:        req.Amount,
	})
	if err != nil {
		log.Printf("create new transfer: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// COMMIT
	if err := tx.Commit(r.Context()); err != nil {
		log.Printf("commit: %v", err)
		sendJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	sendJSON(w, http.StatusCreated, transfer)
}
