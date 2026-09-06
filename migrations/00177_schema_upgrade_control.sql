-- Owner: data plane. Phase: Gate A/B upgrade rehearsal.
-- DB-021: durable plan and resumable backfill evidence. Business rows remain
-- in their existing owners; this migration stores only upgrade control state.

-- +goose Up

-- storage-disposition: schema_upgrade_plan | authoritative immutable upgrade declaration and lifecycle state | local PostgreSQL | platform control journal | global, non-tenant-scoped.
CREATE TABLE IF NOT EXISTS schema_upgrade_plan (
    plan_id                    uuid        PRIMARY KEY,
    source_release             text        NOT NULL,
    target_release             text        NOT NULL,
    compatibility_class        text        NOT NULL,
    source_digest              text        NOT NULL,
    target_digest              text        NOT NULL,
    rollback_boundary          text        NOT NULL,
    required_adoption_watermark bigint      NOT NULL DEFAULT 0,
    status                     text        NOT NULL DEFAULT 'PLANNED',
    created_at                 timestamptz NOT NULL,
    updated_at                 timestamptz NOT NULL,
    CONSTRAINT schema_upgrade_plan_distinct_releases CHECK (source_release <> target_release),
    CONSTRAINT schema_upgrade_plan_compatibility_allowed CHECK (compatibility_class IN ('BACKWARD_COMPATIBLE', 'FULL')),
    CONSTRAINT schema_upgrade_plan_digests_present CHECK (length(source_digest) = 64 AND length(target_digest) = 64),
    CONSTRAINT schema_upgrade_plan_boundary_present CHECK (length(rollback_boundary) > 0),
    CONSTRAINT schema_upgrade_plan_watermark_nonnegative CHECK (required_adoption_watermark >= 0),
    CONSTRAINT schema_upgrade_plan_status_allowed CHECK (status IN ('PLANNED', 'EXPANDED', 'BACKFILLED', 'SHADOWED', 'CUTOVER', 'CONTRACTED', 'ROLLED_BACK', 'ABORTED'))
);

-- storage-disposition: schema_upgrade_checkpoint | authoritative append-only resumable backfill and shadow evidence | local PostgreSQL | upgrade recovery and audit evidence | global, non-tenant-scoped.
CREATE TABLE IF NOT EXISTS schema_upgrade_checkpoint (
    plan_id          uuid        NOT NULL REFERENCES schema_upgrade_plan (plan_id),
    checkpoint_id    uuid        NOT NULL,
    phase            text        NOT NULL,
    cursor_key       text        NOT NULL DEFAULT '',
    copied_rows      bigint      NOT NULL,
    source_rows      bigint      NOT NULL,
    source_digest    text        NOT NULL,
    copied_digest    text        NOT NULL,
    consumer_watermark bigint    NOT NULL DEFAULT 0,
    completed        boolean     NOT NULL DEFAULT false,
    recorded_at      timestamptz NOT NULL,
    PRIMARY KEY (plan_id, checkpoint_id),
    CONSTRAINT schema_upgrade_checkpoint_phase_allowed CHECK (phase IN ('EXPANDED', 'BACKFILLED')),
    CONSTRAINT schema_upgrade_checkpoint_counts_nonnegative CHECK (copied_rows >= 0 AND source_rows >= 0 AND consumer_watermark >= 0),
    CONSTRAINT schema_upgrade_checkpoint_cannot_overcopy CHECK (copied_rows <= source_rows),
    CONSTRAINT schema_upgrade_checkpoint_digests_present CHECK (length(source_digest) = 64 AND length(copied_digest) = 64)
);

CREATE INDEX IF NOT EXISTS schema_upgrade_checkpoint_resume
    ON schema_upgrade_checkpoint (plan_id, recorded_at DESC);

CREATE TRIGGER schema_upgrade_checkpoint_append_only
    BEFORE UPDATE OR DELETE ON schema_upgrade_checkpoint
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

GRANT SELECT, INSERT, UPDATE ON schema_upgrade_plan TO hcmnext_app;
GRANT SELECT, INSERT ON schema_upgrade_checkpoint TO hcmnext_app;
REVOKE UPDATE, DELETE ON schema_upgrade_checkpoint FROM PUBLIC;

-- +goose Down

REVOKE ALL ON schema_upgrade_checkpoint FROM hcmnext_app;
REVOKE ALL ON schema_upgrade_plan FROM hcmnext_app;
DROP TABLE schema_upgrade_checkpoint;
DROP TABLE schema_upgrade_plan;
