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
