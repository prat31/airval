package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/prat31/airval/internal/store"
)

// q is a tiny query builder that numbers $N placeholders.
type qb struct {
	args  []any
	conds []string
}

// arg appends v and returns its placeholder.
func (b *qb) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

// in renders "col IN ($a,$b,...)" for the given values.
func inList[T any](b *qb, col string, vals []T) string {
	ph := make([]string, len(vals))
	for i, v := range vals {
		ph[i] = b.arg(v)
	}
	return col + " IN (" + strings.Join(ph, ",") + ")"
}

func (b *qb) where(cond string) { b.conds = append(b.conds, cond) }

func (b *qb) whereSQL() string {
	if len(b.conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.conds, " AND ")
}

// paginate adds the cursor condition and returns the ORDER/LIMIT clause. desc orders newest first.
func (b *qb) paginate(col string, p store.ListParams, desc bool) (string, int, error) {
	limit := p.Normalize()
	cur, ok, err := store.DecodeCursor(p.Cursor)
	if err != nil {
		return "", 0, err
	}
	op, dir := ">", "ASC"
	if desc {
		op, dir = "<", "DESC"
	}
	if ok {
		b.where(col + " " + op + " " + b.arg(cur))
	}
	return fmt.Sprintf(" ORDER BY %s %s LIMIT %d", col, dir, limit+1), limit, nil
}

// finishPage trims the extra row fetched by paginate and sets NextCursor.
func finishPage[T any](items []T, limit int, id func(T) uuid.UUID) store.Page[T] {
	if len(items) > limit {
		items = items[:limit]
		return store.Page[T]{Items: items, NextCursor: store.EncodeCursor(id(items[len(items)-1]))}
	}
	if items == nil {
		items = []T{}
	}
	return store.Page[T]{Items: items}
}

type scanner interface{ Scan(dest ...any) error }

// collect runs a query and scans every row with fn.
func collect[T any](ctx context.Context, s *Store, query string, args []any, fn func(scanner) (T, error)) ([]T, error) {
	rows, err := s.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, s.mapErr(err)
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := fn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, s.mapErr(rows.Err())
}

func one[T any](ctx context.Context, s *Store, query string, args []any, fn func(scanner) (T, error)) (T, error) {
	v, err := fn(s.q.QueryRowContext(ctx, query, args...))
	if err != nil {
		var zero T
		return zero, s.mapErr(err)
	}
	return v, nil
}

// JSON helpers. JSON is always passed to the driver as a string (works for jsonb and TEXT).

// jsonArg returns raw as a string parameter, or def when raw is empty. def "" means NULL.
func jsonArg(raw json.RawMessage, def string) any {
	if len(raw) == 0 || string(raw) == "null" {
		if def == "" {
			return nil
		}
		return def
	}
	return string(raw)
}

// jsonValue marshals v into a string parameter.
func jsonValue(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("sqlstore: marshal %T: %v", v, err))
	}
	return string(b)
}

// rawJSON scans a nullable JSON column into a json.RawMessage.
type rawJSON struct{ dst *json.RawMessage }

func (r rawJSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*r.dst = nil
	case []byte:
		*r.dst = append(json.RawMessage(nil), v...)
	case string:
		*r.dst = json.RawMessage(v)
	default:
		return fmt.Errorf("sqlstore: cannot scan %T into JSON", src)
	}
	return nil
}

// jsonInto scans a JSON column into *T (NULL leaves the zero value).
type jsonInto[T any] struct{ dst *T }

func (j jsonInto[T]) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case nil:
		return nil
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("sqlstore: cannot scan %T into JSON", src)
	}
	return json.Unmarshal(b, j.dst)
}

func jsonCol[T any](dst *T) jsonInto[T] { return jsonInto[T]{dst} }

func strs(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// nullStmt closes prepared statements, ignoring nil.
func closeStmts(stmts ...*sql.Stmt) {
	for _, s := range stmts {
		if s != nil {
			_ = s.Close()
		}
	}
}
