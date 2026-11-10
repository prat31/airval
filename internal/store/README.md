# internal/store

Persistence for airval. One interface, two SQL dialects, one shared implementation.

```
store/              models.go (structs), store.go (Store + per-area interfaces, filters, pagination, errors)
store/migrations/   goose migrations: postgres/*.sql and sqlite/*.sql (embedded), Up/Down/List helpers
store/sqlstore/     the implementation (Postgres via pgx stdlib, SQLite via modernc — CGO-free)
                    one file per area: orgs.go, projects.go, targets.go, datasets.go, suites.go, runs.go,
                    results.go, review.go; dialect.go holds every non-portable bit
store/storetest/    the contract suite; sqlstore/contract_test.go runs it on SQLite always and on
                    Postgres when AIRVAL_TEST_PG_URL is set (each test gets its own schema)
```

We deliberately do **not** use sqlc: both dialects share the same hand-written SQL (`$N` placeholders work on
both drivers), so there is one query per operation instead of two generated copies.

## Conventions

- IDs are UUIDv7 (`store.NewID()`), generated in Go. Timestamps are generated in Go (UTC, µs).
- JSON columns are `jsonb` (PG) / `TEXT` (SQLite). Pass JSON as a **string** (`jsonArg`, `jsonValue`), scan
  with `rawJSON{&field}` or `jsonCol(&typedField)`. Never pass `[]byte` for JSON (SQLite would treat it as JSONB blob).
- Booleans/floats/ints/times scan natively on both drivers; nullable columns scan into pointers.
- Errors: map driver errors through `s.mapErr` → `store.ErrNotFound`, `store.ErrConflict`.
  Use `execAffect` for UPDATE/DELETE by id so missing rows return `ErrNotFound`.
- Lists are cursor-paginated by id: `tail, limit, err := b.paginate("id", p, desc)` then
  `finishPage(items, limit, idFunc)`. Cursors are opaque (`store.EncodeCursor`).
- Batch writes use multi-row `INSERT ... VALUES` built with `valuesSQL(rows, cols)` in chunks (see `results.go`);
  never write per row in a loop outside a prepared statement + transaction.
- Non-portable SQL goes behind the `dialect` interface in `dialect.go` (e.g. `jsonArrayContains`, `like`).

## Adding a query (without conflicting with other work)

1. Add the method to the narrowest area interface in `store.go` (or add a new area interface and embed it in
   `Store`). Add any new model to `models.go` (or a new `models_<area>.go` file if you prefer isolation).
2. Implement it in `sqlstore/<area>.go`. For a brand-new area, create `sqlstore/<area>.go` — don't grow
   unrelated files. Build queries with `qb` (`b.arg(v)` returns the next `$N`).
3. Schema changes: add a **new** numbered migration to **both** `migrations/postgres/` and
   `migrations/sqlite/` (`000NN_<what>.sql`, with `-- +goose Up` / `-- +goose Down`). Never edit an applied one.
4. Add a contract test in `storetest/` (a new `storetest_<area>.go` file with a function wired into
   the table in `Run`) so both dialects are verified.
5. Run `make test` and `make test-pg`.

## Hot path

`InsertResults` is used by workers' ingest batching: on Postgres it COPYs results and scores in one pgx
transaction; on a duplicate `(run_id, example_id, trial)` (re-delivered shard) it falls back to the portable
path (multi-row `INSERT ... ON CONFLICT DO NOTHING RETURNING id`, then replace duplicates), which is also the
SQLite path. Outputs over 8 KiB are stored zstd-compressed (`output_z`) and decompressed transparently
(note: substring search does not look inside compressed outputs).
