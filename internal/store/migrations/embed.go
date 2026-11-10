// Package migrations holds goose migrations for both SQL dialects and helpers to apply them.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed postgres/*.sql sqlite/*.sql
var files embed.FS

func provider(db *sql.DB, dialect string) (*goose.Provider, error) {
	var d goose.Dialect
	switch dialect {
	case "postgres":
		d = goose.DialectPostgres
	case "sqlite":
		d = goose.DialectSQLite3
	default:
		return nil, fmt.Errorf("migrations: unknown dialect %q", dialect)
	}
	sub, err := fs.Sub(files, dialect)
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(d, db, sub)
}

// Up applies all pending migrations.
func Up(ctx context.Context, db *sql.DB, dialect string) error {
	p, err := provider(db, dialect)
	if err != nil {
		return err
	}
	_, err = p.Up(ctx)
	return err
}

// Down rolls back the most recent migration.
func Down(ctx context.Context, db *sql.DB, dialect string) error {
	p, err := provider(db, dialect)
	if err != nil {
		return err
	}
	_, err = p.Down(ctx)
	return err
}

// Status describes one migration's state.
type Status struct {
	Version int64
	Source  string
	Applied bool
}

// List returns the status of every migration.
func List(ctx context.Context, db *sql.DB, dialect string) ([]Status, error) {
	p, err := provider(db, dialect)
	if err != nil {
		return nil, err
	}
	st, err := p.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(st))
	for _, s := range st {
		out = append(out, Status{Version: s.Source.Version, Source: s.Source.Path, Applied: s.State == goose.StateApplied})
	}
	return out, nil
}
