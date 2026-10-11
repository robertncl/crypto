package store

import (
	"path/filepath"
	"testing"

	"cryptoex/internal/db"
)

// TestHotStatementsPrepare guards against a typo silently dropping a hot
// statement back to the unprepared path.
func TestHotStatementsPrepare(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := New(conn)
	for _, q := range hotStatements {
		if s.stmts[q] == nil {
			t.Errorf("statement failed to prepare: %s", q)
		}
	}
}
