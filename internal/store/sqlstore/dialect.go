package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/prat31/airval/internal/store"
)

// ---- Postgres ----

type pgDialect struct{}

func openPostgres(url string, opts Options) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	n := opts.MaxOpenConns
	if n == 0 {
		n = 25
	}
	db.SetMaxOpenConns(n)
	db.SetMaxIdleConns(n)
	return db, nil
}

func (pgDialect) name() string               { return "postgres" }
func (pgDialect) like() string               { return "ILIKE" }
func (pgDialect) jsonText(col string) string { return col + "::text" }
func (pgDialect) jsonArrayContains(col, p string) string {
	return col + " @> jsonb_build_array(" + p + "::text)"
}
func (pgDialect) jsonObjectField(col, p string) string { return "(" + col + " ->> " + p + "::text)" }

func (pgDialect) isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (pgDialect) isFKViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23503"
}

func pgUUID(id [16]byte) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func pgJSON(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return string(raw)
}

// bulkInsertResults COPYs results and scores in one pgx transaction. On a unique violation
// (re-delivered shard) it reports ok=false so the caller falls back to the upserting path.
func (pgDialect) bulkInsertResults(ctx context.Context, s *Store, results []store.Result, scores []store.Score) (bool, error) {
	if s.tx != nil {
		return false, nil // inside a database/sql transaction: use the generic path
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	var dup bool
	err = conn.Raw(func(dc any) error {
		pc := dc.(*stdlib.Conn).Conn()
		tx, err := pc.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
		if len(results) > 0 {
			_, err = tx.CopyFrom(ctx, pgx.Identifier{"results"}, resultCols,
				pgx.CopyFromSlice(len(results), func(i int) ([]any, error) {
					r := &results[i]
					out, z := encodeOutput(r.Output)
					var ttft any
					if r.TTFTMs != nil {
						ttft = *r.TTFTMs
					}
					return []any{pgUUID(r.ID), pgUUID(r.RunID), pgUUID(r.ExampleID), pgUUID(r.ExampleRevisionID),
						r.Trial, r.Status, out, z, pgJSON(r.OutputJSON), pgJSON(r.Context), pgJSON(r.ToolCalls),
						pgJSON(r.Trace), pgJSON(r.Transcript), pgJSON(r.Request), pgJSON(r.Response),
						r.LatencyMs, ttft, r.TokensIn, r.TokensOut, r.CostUSD, r.Error, r.Cached, r.CreatedAt}, nil
				}))
			if err != nil {
				return err
			}
		}
		if len(scores) > 0 {
			_, err = tx.CopyFrom(ctx, pgx.Identifier{"scores"}, scoreCols,
				pgx.CopyFromSlice(len(scores), func(i int) ([]any, error) {
					c := &scores[i]
					var val, passed any
					if c.Value != nil {
						val = *c.Value
					}
					if c.Passed != nil {
						passed = *c.Passed
					}
					return []any{pgUUID(c.ResultID), pgUUID(c.EvaluatorID), pgUUID(c.RunID), val, c.Label, passed,
						c.Reason, pgJSON(c.Meta), c.LatencyMs, c.CostUSD, c.CreatedAt}, nil
				}))
			if err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	})
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			dup = true
		} else {
			return false, s.mapErr(err)
		}
	}
	return !dup, nil
}

// ---- SQLite ----

type sqliteDialect struct{}

// sqlitePath extracts a filesystem path from sqlite://, file: or bare forms.
func sqlitePath(url string) string {
	p := strings.TrimPrefix(url, "sqlite://")
	p = strings.TrimPrefix(p, "sqlite:")
	p = strings.TrimPrefix(p, "file:")
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return p
}

func openSQLite(url string, opts Options) (*sql.DB, error) {
	path := sqlitePath(url)
	if path == "" {
		return nil, fmt.Errorf("sqlstore: empty sqlite path in %q", url)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)&_txlock=immediate&_time_format=sqlite"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	n := opts.MaxOpenConns
	if n == 0 {
		n = 8
	}
	db.SetMaxOpenConns(n)
	db.SetMaxIdleConns(n)
	return db, nil
}

func (sqliteDialect) name() string               { return "sqlite" }
func (sqliteDialect) like() string               { return "LIKE" }
func (sqliteDialect) jsonText(col string) string { return col }
func (sqliteDialect) jsonArrayContains(col, p string) string {
	return "EXISTS (SELECT 1 FROM json_each(" + col + ") WHERE json_each.value = " + p + ")"
}
func (sqliteDialect) jsonObjectField(col, p string) string { return "(" + col + " ->> " + p + ")" }

func sqliteCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

func (sqliteDialect) isUniqueViolation(err error) bool {
	c := sqliteCode(err)
	return c == sqlite3.SQLITE_CONSTRAINT_UNIQUE || c == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

func (sqliteDialect) isFKViolation(err error) bool {
	return sqliteCode(err) == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
}

func (sqliteDialect) bulkInsertResults(context.Context, *Store, []store.Result, []store.Score) (bool, error) {
	return false, nil
}
