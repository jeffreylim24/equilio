package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeffreylim24/equilio/backend/db"
)

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

	// The response alone doesn't prove what was stored.
	stored, err := srv.queries.GetAccountByID(t.Context(), got.ID)
	if err != nil {
		t.Fatalf("read account %d back: %v", got.ID, err)
	}
	if stored != got {
		t.Errorf("stored = %+v, want it to match response %+v", stored, got)
	}
}

// TestGetAccountByID_NotFound creates a decoy account so that a handler ignoring the
// ID would return 200 instead of 404.
func TestGetAccountByID_NotFound(t *testing.T) {
	srv := &Server{queries: db.New(testPool), pool: testPool}
	resetDB(t)

	existing, err := srv.queries.CreateAccount(t.Context(), "Decoy")
	if err != nil {
		t.Fatalf("create decoy account: %v", err)
	}
	missing := existing.ID + 1

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

// TestCreateTransfer_RollsBackWhenInsertFails forces the transfer insert to fail after
// both balance updates have run, and checks that neither update sticks.
func TestCreateTransfer_RollsBackWhenInsertFails(t *testing.T) {
	srv := &Server{queries: db.New(testPool), pool: testPool}
	resetDB(t)

	from, err := srv.queries.CreateAccount(t.Context(), "Sender")
	if err != nil {
		t.Fatalf("create from account: %v", err)
	}
	to, err := srv.queries.CreateAccount(t.Context(), "Receiver")
	if err != nil {
		t.Fatalf("create to account: %v", err)
	}

	// The API can't add money, so seed it directly.
	for id, balance := range map[int64]int64{from.ID: 100, to.ID: 50} {
		_, err := testPool.Exec(t.Context(), "UPDATE accounts SET balance = $1 WHERE id = $2", balance, id)
		if err != nil {
			t.Fatalf("seed account %d: %v", id, err)
		}
	}

	// Makes every insert into transfers fail.
	_, err = testPool.Exec(t.Context(),
		"ALTER TABLE transfers ADD CONSTRAINT test_block_transfers CHECK (false) NOT VALID")
	if err != nil {
		t.Fatalf("add blocking constraint: %v", err)
	}
	t.Cleanup(func() {
		// Not t.Context(): it is already canceled when cleanup runs.
		_, err := testPool.Exec(context.Background(),
			"ALTER TABLE transfers DROP CONSTRAINT IF EXISTS test_block_transfers")
		if err != nil {
			t.Errorf("drop blocking constraint: %v", err)
		}
	})

	payload, err := json.Marshal(map[string]int64{
		"from_account_id": from.ID,
		"to_account_id":   to.ID,
		"amount":          30,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)

	// Any other status means the request never reached the writes.
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	for _, c := range []struct {
		name string
		id   int64
		want int64
	}{
		{"from", from.ID, 100},
		{"to", to.ID, 50},
	} {
		acc, err := srv.queries.GetAccountByID(t.Context(), c.id)
		if err != nil {
			t.Fatalf("read %s account: %v", c.name, err)
		}
		if acc.Balance != c.want {
			t.Errorf("%s balance = %d, want %d (unchanged)", c.name, acc.Balance, c.want)
		}
	}

	var n int64
	err = testPool.QueryRow(t.Context(), "SELECT count(*) FROM transfers").Scan(&n)
	if err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if n != 0 {
		t.Errorf("transfers = %d, want 0", n)
	}
}
