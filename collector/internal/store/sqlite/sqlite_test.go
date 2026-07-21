package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/store/sqlite"
	"github.com/wlix13/orrery/collector/internal/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		t.Helper()

		s, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { s.Close() })

		return s
	})
}
