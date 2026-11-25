-- +goose Up
-- +goose StatementBegin
-- One row per run of an official-catalogue scraper. The row is written before
-- the network fetch so a crash leaves a "running" record, and finished with the
-- counters the run reported. It is the audit trail for the global catalogue:
-- which source changed how many rows, and why a run failed.
CREATE TABLE craft_catalog_sync_runs (
    id          bigserial   PRIMARY KEY,
    source      text        NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status      text        NOT NULL DEFAULT 'running',
    created     integer     NOT NULL DEFAULT 0,
    updated     integer     NOT NULL DEFAULT 0,
    unchanged   integer     NOT NULL DEFAULT 0,
    error       text        NOT NULL DEFAULT '',
    CONSTRAINT craft_catalog_sync_runs_source_check CHECK (source <> ''),
    CONSTRAINT craft_catalog_sync_runs_status_check CHECK (status IN ('running', 'ok', 'failed')),
    CONSTRAINT craft_catalog_sync_runs_counts_check CHECK (
        created >= 0 AND updated >= 0 AND unchanged >= 0
    )
);

CREATE INDEX craft_catalog_sync_runs_source_idx
    ON craft_catalog_sync_runs (source, started_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE craft_catalog_sync_runs;
-- +goose StatementEnd
