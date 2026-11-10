package sqlstore_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/store/sqlstore"
	"github.com/airval/airval/internal/store/storetest"
)

func TestContractSQLite(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		s, err := sqlstore.Open(context.Background(), filepath.Join(t.TempDir(), "airval.db"), sqlstore.Options{Migrate: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}

// TestContractPostgres runs against AIRVAL_TEST_PG_URL, isolating each test in its own schema.
func TestContractPostgres(t *testing.T) {
	base := os.Getenv("AIRVAL_TEST_PG_URL")
	if base == "" {
		t.Skip("AIRVAL_TEST_PG_URL not set")
	}
	storetest.Run(t, func(t *testing.T) store.Store {
		return OpenPGSchema(t, base)
	})
}

// OpenPGSchema creates a throwaway schema and opens a migrated store bound to it.
func OpenPGSchema(t *testing.T, base string) store.Store {
	t.Helper()
	ctx := context.Background()
	schema := "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	s, err := sqlstore.Open(ctx, fmt.Sprintf("%s%ssearch_path=%s", base, sep, schema), sqlstore.Options{Migrate: true, MaxOpenConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return s
}
