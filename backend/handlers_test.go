package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeffreylim24/equilio/backend/db"
)

// TestCreateAccount_BlankName checks that POST /accounts rejects a name with no
// non-whitespace character (D6): 400, a JSON error body, and no row written.
func TestCreateAccount_BlankName(t *testing.T) {
	srv := &Server{queries: db.New(testPool), pool: testPool}

	for _, name := range []string{"", " ", "\t", "\n"} {
		t.Run(fmt.Sprintf("name=%q", name), func(t *testing.T) {
			resetDB(t)

			// json.Marshal escapes \t and \n; hand-built JSON would be invalid and
			// get a 400 for the wrong reason.
			payload, err := json.Marshal(map[string]string{"name": name})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/accounts", bytes.NewReader(payload))
			rec := httptest.NewRecorder()
			srv.handleCreateAccount(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}

			var got struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v (body: %s)", err, rec.Body.String())
			}
			if got.Error == "" {
				t.Errorf("error message is empty, want non-empty")
			}

			var n int64
			err = testPool.QueryRow(t.Context(), "SELECT count(*) FROM accounts").Scan(&n)
			if err != nil {
				t.Fatalf("count accounts: %v", err)
			}
			if n != 0 {
				t.Errorf("accounts = %d, want 0", n)
			}
		})
	}
}

// TestCreateAccount_StartsAtZero checks that POST /accounts returns 201 with the
// new account, and that it starts at balance 0 in the DB, not just in the response (D4).
func TestCreateAccount_StartsAtZero(t *testing.T) {
	srv := &Server{queries: db.New(testPool), pool: testPool}
	resetDB(t)

	payload, err := json.Marshal(map[string]string{"name": "Alice"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/accounts", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	srv.handleCreateAccount(rec, req)

	if rec.Code != http.StatusCreated {
		// Without a 201 there is no account to check, so stop here.
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var got db.Account
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v (body: %s)", err, rec.Body.String())
	}
	if got.ID <= 0 {
		t.Errorf("id = %d, want > 0", got.ID)
	}
	if got.Name != "Alice" {
		t.Errorf("name = %q, want %q", got.Name, "Alice")
	}
	if got.Balance != 0 {
		t.Errorf("balance = %d, want 0", got.Balance)
	}

	// The response could be built by hand and say anything; the stored row is the truth.
	stored, err := srv.queries.GetAccountByID(t.Context(), got.ID)
	if err != nil {
		t.Fatalf("read account %d back: %v", got.ID, err)
	}
	if stored != got {
		t.Errorf("stored = %+v, want it to match response %+v", stored, got)
	}
}

// TestGetAccountByID_NotFound checks that GET /accounts/{id} returns 404 naming the ID
// when no account has it. A decoy account exists, so a handler that ignores the ID and
// returns any row would get 200 and fail this test.
func TestGetAccountByID_NotFound(t *testing.T) {
	srv := &Server{queries: db.New(testPool), pool: testPool}
	resetDB(t)

	existing, err := srv.queries.CreateAccount(t.Context(), "Decoy")
	if err != nil {
		t.Fatalf("create decoy account: %v", err)
	}
	missing := existing.ID + 1 // the only row is existing, so this ID is unused

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/accounts/%d", missing), nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	var got struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v (body: %s)", err, rec.Body.String())
	}
	want := fmt.Sprintf("account %d not found", missing)
	if got.Error != want {
		t.Errorf("error = %q, want %q", got.Error, want)
	}
}
