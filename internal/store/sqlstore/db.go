// Package sqlstore implements store.Store for Postgres (pgx) and SQLite (modernc, CGO-free).
//
// Both dialects share the SQL in this package: queries use $N placeholders (understood by
// both drivers), JSON is stored as jsonb (PG) or TEXT (SQLite) and passed as strings, and the
// few non-portable fragments go through the dialect interface. The hot path (InsertResults)
// is specialized: COPY on Postgres, one prepared-statement transaction on SQLite.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/airval/airval/internal/store"
	"github.com/airval/airval/internal/store/migrations"
)

// dbtx is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// dialect captures the non-portable bits between Postgres and SQLite.
type dialect interface {
	name() string
	// like returns the case-insensitive LIKE operator.
	like() string
	// jsonText renders a JSON column as text for substring search.
	jsonText(col string) string
	// jsonArrayContains renders "the JSON string array col contains the string at placeholder p".
	jsonArrayContains(col, p string) string
	// jsonObjectField renders "col->>key" for a placeholder key.
	jsonObjectField(col, keyPlaceholder string) string
	isUniqueViolation(err error) bool
	isFKViolation(err error) bool
	// bulkInsertResults is the dialect's fast path; ok=false means "use the generic path".
	bulkInsertResults(ctx context.Context, s *Store, results []store.Result, scores []store.Score) (ok bool, err error)
}

// Store is the SQL implementation of store.Store.
type Store struct {
	db  *sql.DB
	q   dbtx
	tx  *sql.Tx
	d   dialect
	log *slog.Logger
}

var _ store.Store = (*Store)(nil)

// Options configures Open.
type Options struct {
	// Migrate applies pending migrations on open.
	Migrate bool
	Logger  *slog.Logger
	// MaxOpenConns overrides the pool size (0 = dialect default).
	MaxOpenConns int
}

// Open connects to the database identified by url:
//   - postgres://... or postgresql://...  → Postgres
//   - sqlite://path/to.db, file:path.db, or a bare *.db path → SQLite
func Open(ctx context.Context, url string, opts Options) (*Store, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	var (
		db  *sql.DB
		d   dialect
		err error
	)
	switch {
	case strings.HasPrefix(url, "postgres://"), strings.HasPrefix(url, "postgresql://"):
		db, err = openPostgres(url, opts)
		d = pgDialect{}
	default:
		db, err = openSQLite(url, opts)
		d = sqliteDialect{}
	}
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlstore: ping %s: %w", d.name(), err)
	}
	s := &Store{db: db, q: db, d: d, log: opts.Logger}
	if opts.Migrate {
		if err := migrations.Up(ctx, db, d.name()); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlstore: migrate: %w", err)
		}
	}
	return s, nil
}

// DB exposes the underlying pool (migrations CLI, diagnostics).
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Dialect() string { return s.d.name() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Close() error { return s.db.Close() }

// WithTx runs fn inside a transaction; nested calls reuse the outer one.
func (s *Store) WithTx(ctx context.Context, fn func(tx store.Store) error) error {
	return s.withTx(ctx, func(tx *Store) error { return fn(tx) })
}

func (s *Store) withTx(ctx context.Context, fn func(tx *Store) error) error {
	if s.tx != nil {
		return fn(s)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	ts := &Store{db: s.db, q: tx, tx: tx, d: s.d, log: s.log}
	if err := fn(ts); err != nil {
		_ = tx.Rollback()
		return err
	}
	return s.mapErr(tx.Commit())
}

// mapErr converts driver errors into store sentinel errors.
func (s *Store) mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return store.ErrNotFound
	case s.d.isUniqueViolation(err):
		return fmt.Errorf("%w: %v", store.ErrConflict, err)
	case s.d.isFKViolation(err):
		return fmt.Errorf("%w: referenced row does not exist: %v", store.ErrNotFound, err)
	}
	return err
}

// exec runs a statement and returns ErrNotFound when it affected no rows and mustAffect is set.
func (s *Store) execAffect(ctx context.Context, query string, args ...any) error {
	res, err := s.q.ExecContext(ctx, query, args...)
	if err != nil {
		return s.mapErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) exec(ctx context.Context, query string, args ...any) error {
	_, err := s.q.ExecContext(ctx, query, args...)
	return s.mapErr(err)
}

func now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

// stamp fills id/created/updated for inserts.
func stampTimes(created, updated *time.Time) {
	t := now()
	if created != nil && created.IsZero() {
		*created = t
	}
	if updated != nil {
		*updated = t
	}
}
