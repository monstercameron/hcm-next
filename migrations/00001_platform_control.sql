-- Owner: data plane. Phase: P1A.
-- DB-006: schema release and migration journal. These two tables are the only
-- platform-control tables that are not tenant scoped; every other authoritative
-- table in this tree carries tenant_id NOT NULL (DATA-001).

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION
        'table % is append-only; % is forbidden', TG_TABLE_NAME, TG_OP
        USING ERRCODE = '23514';
END;
$$;
-- +goose StatementEnd

COMMENT ON FUNCTION forbid_mutation() IS
    'Append-only enforcement shared by ledger_event, proposal_revision and definition_version.';

CREATE TABLE schema_release (
    release_id          uuid        PRIMARY KEY,
    release_version     text        NOT NULL UNIQUE,
    artifact_digest     text        NOT NULL,
    digest_algorithm    text        NOT NULL,
    source_digest       text        NOT NULL,
    tool_version        text        NOT NULL,
    compatibility_class text        NOT NULL,
    owner               text        NOT NULL,
    reversible          boolean     NOT NULL,
    recorded_at         timestamptz NOT NULL DEFAULT now(),
    trusted_time_source text        NOT NULL,
    CONSTRAINT schema_release_compatibility_class_allowed CHECK (
        compatibility_class IN ('BACKWARD_COMPATIBLE', 'FORWARD_COMPATIBLE', 'FULL', 'BREAKING')
    ),
    CONSTRAINT schema_release_digest_algorithm_allowed CHECK (digest_algorithm IN ('sha256')),
    CONSTRAINT schema_release_artifact_digest_present CHECK (length(artifact_digest) > 0),
    CONSTRAINT schema_release_tool_version_present CHECK (length(tool_version) > 0)
);

CREATE TABLE migration_journal (
    journal_id          uuid        PRIMARY KEY,
    release_id          uuid        NOT NULL REFERENCES schema_release (release_id),
    migration_version   bigint      NOT NULL,
    migration_name      text        NOT NULL,
    direction           text        NOT NULL,
    checksum            text        NOT NULL,
    checksum_algorithm  text        NOT NULL,
    tool_version        text        NOT NULL,
    applied_by          text        NOT NULL,
    status              text        NOT NULL,
    started_at          timestamptz NOT NULL,
    finished_at         timestamptz,
    trusted_time_source text        NOT NULL,
    failure_detail      text,
    -- Set when a recorded up-migration is later rolled back. The DOWN row is the
    -- evidence; this marks the UP row as no longer in effect so the migration can
    -- legitimately be applied again.
    rolled_back_at      timestamptz,
    CONSTRAINT migration_journal_direction_allowed CHECK (direction IN ('UP', 'DOWN')),
    CONSTRAINT migration_journal_status_allowed CHECK (
        status IN ('PLANNED', 'RUNNING', 'APPLIED', 'FAILED', 'ROLLED_FORWARD')
    ),
    CONSTRAINT migration_journal_checksum_present CHECK (length(checksum) > 0),
    CONSTRAINT migration_journal_checksum_algorithm_allowed CHECK (checksum_algorithm IN ('sha256')),
    CONSTRAINT migration_journal_version_positive CHECK (migration_version > 0),
    -- Half-open observation window: started_at is inclusive, finished_at exclusive.
    CONSTRAINT migration_journal_window_half_open CHECK (
        finished_at IS NULL OR started_at < finished_at
    ),
    CONSTRAINT migration_journal_terminal_is_finished CHECK (
        status IN ('PLANNED', 'RUNNING') OR finished_at IS NOT NULL
    ),
    CONSTRAINT migration_journal_failure_detail_only_on_failure CHECK (
        failure_detail IS NULL OR status = 'FAILED'
    ),
    CONSTRAINT migration_journal_rollback_marks_an_applied_up CHECK (
        rolled_back_at IS NULL OR (direction = 'UP' AND status = 'APPLIED')
    ),
    CONSTRAINT migration_journal_rollback_after_finish CHECK (
        rolled_back_at IS NULL OR (finished_at IS NOT NULL AND rolled_back_at >= finished_at)
    )
);

-- Duplicate application of the same release/version is idempotent: at most one
-- APPLIED up-row can be in effect per (release, migration version). A row that
-- was rolled back is no longer in effect, so the migration may be applied again
-- and that attempt is journaled in its own right.
CREATE UNIQUE INDEX migration_journal_applied_once
    ON migration_journal (release_id, migration_version)
    WHERE status = 'APPLIED' AND direction = 'UP' AND rolled_back_at IS NULL;

CREATE INDEX migration_journal_release_started
    ON migration_journal (release_id, started_at);

-- +goose Down
DROP TABLE migration_journal;
DROP TABLE schema_release;
DROP FUNCTION forbid_mutation();
