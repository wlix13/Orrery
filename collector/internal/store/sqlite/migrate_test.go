package sqlite

import (
	"path/filepath"
	"testing"
)

func TestMigrateConverges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	// Fresh file, then reopen: both must land on current version.
	for range 2 {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}

		var version int
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}

		s.Close()

		if version != len(migrations) {
			t.Fatalf("user_version = %d, want %d", version, len(migrations))
		}
	}
}
