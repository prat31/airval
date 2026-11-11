-- +goose Up
-- Execution engine: one row per shard of a run. Workers mark shards done; the last one triggers the finalizer.
CREATE TABLE run_shards (
    run_id  uuid NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    shard   integer NOT NULL,
    items   integer NOT NULL DEFAULT 0,
    stats   jsonb,
    done_at timestamptz,
    PRIMARY KEY (run_id, shard)
);
CREATE INDEX run_shards_pending_idx ON run_shards (run_id) WHERE done_at IS NULL;

-- +goose Down
DROP TABLE run_shards;
