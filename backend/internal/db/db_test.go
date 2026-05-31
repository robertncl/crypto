package db_test

import (
	"path/filepath"
	"testing"

	"cryptoex/internal/db"
)

func TestOpenAppliesSchemaAndSeeds(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	counts := map[string]int{
		"assets":       5,
		"markets":      4,
		"perp_markets": 3,
	}
	for table, want := range counts {
		var n int
		if err := conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n < want {
			t.Errorf("%s rows = %d, want >= %d", table, n, want)
		}
	}
}

func TestOpenSeedIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	conn1, err := db.Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	conn1.Close()

	conn2, err := db.Open(path) // re-open should not duplicate seed rows
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer conn2.Close()

	var n int
	if err := conn2.QueryRow("SELECT COUNT(*) FROM assets").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("assets after re-open = %d, want exactly 5 (idempotent seed)", n)
	}
}

func TestOpenInvalidPath(t *testing.T) {
	// A path under a non-existent directory cannot be created.
	_, err := db.Open("/nonexistent-dir-xyz/sub/test.db")
	if err == nil {
		t.Error("Open with unwritable path should return an error")
	}
}

func TestInClause(t *testing.T) {
	clause, args := db.InClause([]string{"a", "b", "c"})
	if clause != "(?,?,?)" {
		t.Errorf("clause = %q, want (?,?,?)", clause)
	}
	if len(args) != 3 || args[0] != "a" || args[2] != "c" {
		t.Errorf("args = %v", args)
	}
}

func TestInClauseEmpty(t *testing.T) {
	clause, args := db.InClause(nil)
	if clause != "(NULL)" {
		t.Errorf("clause = %q, want (NULL)", clause)
	}
	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestInClauseSingle(t *testing.T) {
	clause, args := db.InClause([]string{"only"})
	if clause != "(?)" || len(args) != 1 {
		t.Errorf("single InClause = %q, %v", clause, args)
	}
}
