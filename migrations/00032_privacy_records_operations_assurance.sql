-- Owner: privacy/records/operations lane. Phase: P1B.
-- DB-015: privacy, records, operations and assurance metadata.
--
-- Scope
-- -----
-- This migration materializes the privacy/records lifecycle from
-- planning/data/models/assurance-intelligence-platform.md and the
-- incident/backup/recovery/SLO/control-evidence lifecycle from
-- planning/data/models/operations-production.md. It creates no table any
-- other migration owns: the evidence/obligation/jurisdiction tables belong
-- to 00021, artifact bytes to 00010/00030, and configuration objects,
-- workflow scheduling, ledger checkpoints and tenant bootstrap receipts to
-- 00024-00029.
--
-- The four invariants DB-015's RED clause names, enforced in the schema:
--
--  1. Nothing loses tenant. Every table is tenant scoped and every foreign
--     key is composite on tenant_id, so a copy, hold, disposition,
--     incident, recovery run or control evidence row cannot reference
--     another tenant's row.
--  2. Nothing loses scope or version. record_declaration carries its
--     retention schedule key AND version, legal_hold carries a scope
--     predicate plus its digest and its own version, control_evidence
--     carries the control version and its window, slo_observation carries
--     the slo version it was measured against.
--  3. Nothing loses retention. A retention_disposition may not record an
--     execution while a hold blocks it
--     (retention_disposition_hold_blocks_execution), and every
--     record_declaration names the schedule that produced its eligibility.
--  4. Nothing loses correction lineage. record_declaration and
--     retention_disposition both carry a self-referencing corrects_* link,
--     so a correction supersedes its target instead of overwriting it, and
--     both tables reject a row that corrects itself.
--
-- Retention. data_copy_inventory, retention_disposition receipts,
-- slo_observation, control_evidence and audit_package are append-only
-- evidence and carry 00001's forbid_mutation trigger. Operational tables
-- (legal_hold, hold_intersection, record_declaration, operational_incident,
-- backup_run, recovery_run and the two registries) are live serving state.
--
-- REFACTOR (DB-015): operational telemetry stays separate from business and
-- assurance evidence. Nothing here stores a metric series, a log line or a
-- span: slo_observation stores the aggregated good/total counts an SLO
-- decision is actually made from, and control_evidence stores a digest of
-- the collected artifact, never the artifact.

-- +goose Up

-- ---------------------------------------------------------------------------
-- Privacy: purpose declarations and the copy inventory
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS processing_purpose_declaration (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    declaration_id     uuid           NOT NULL,
    purpose_key        semantic_key   NOT NULL,
    declaration_version cas_version   NOT NULL,
    lawful_basis       text           NOT NULL,
    data_categories    jsonb          NOT NULL DEFAULT '{}'::jsonb,
    allowed_operations jsonb          NOT NULL DEFAULT '{}'::jsonb,
    recipients         jsonb          NOT NULL DEFAULT '{}'::jsonb,
    retention_schedule_key semantic_key NOT NULL,
    transfer_policy_ref text          NOT NULL DEFAULT '',
    content_digest     content_digest NOT NULL,
    effective_from     timestamptz    NOT NULL,
    effective_to       timestamptz,
    status             text           NOT NULL DEFAULT 'DRAFT',
    created_at         timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, declaration_id),
    CONSTRAINT processing_purpose_declaration_version_unique UNIQUE (tenant_id, purpose_key, declaration_version),
    CONSTRAINT processing_purpose_declaration_basis_allowed CHECK (lawful_basis IN (
        'CONSENT','CONTRACT','LEGAL_OBLIGATION','VITAL_INTEREST','PUBLIC_TASK','LEGITIMATE_INTEREST'
    )),
    CONSTRAINT processing_purpose_declaration_categories_object CHECK (jsonb_typeof(data_categories) = 'object'),
    CONSTRAINT processing_purpose_declaration_operations_object CHECK (jsonb_typeof(allowed_operations) = 'object'),
    CONSTRAINT processing_purpose_declaration_recipients_object CHECK (jsonb_typeof(recipients) = 'object'),
    CONSTRAINT processing_purpose_declaration_status_allowed CHECK (status IN ('DRAFT','ACTIVE','SUPERSEDED','WITHDRAWN')),
    CONSTRAINT processing_purpose_declaration_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);

-- data_copy_inventory is one immutable, watermarked census of where a
-- canonical asset's copies live. completeness/unknown_count keep a partial
-- census honest instead of letting it read as complete.
CREATE TABLE IF NOT EXISTS data_copy_inventory (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    inventory_id        uuid           NOT NULL,
    canonical_asset_key semantic_key   NOT NULL,
    as_of               timestamptz    NOT NULL,
    source_watermark    text           NOT NULL,
    completeness        text           NOT NULL,
    unknown_count       integer        NOT NULL DEFAULT 0,
    content_digest      content_digest NOT NULL,
    created_at          timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, inventory_id),
    CONSTRAINT data_copy_inventory_asset_unique UNIQUE (tenant_id, canonical_asset_key, as_of),
    CONSTRAINT data_copy_inventory_completeness_allowed CHECK (completeness IN ('COMPLETE','PARTIAL','UNKNOWN')),
    CONSTRAINT data_copy_inventory_unknown_nonnegative CHECK (unknown_count >= 0),
    CONSTRAINT data_copy_inventory_complete_has_no_unknowns CHECK (completeness <> 'COMPLETE' OR unknown_count = 0)
);

CREATE TABLE IF NOT EXISTS data_copy (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    copy_id              uuid         NOT NULL,
    inventory_id         uuid         NOT NULL,
    canonical_asset_key  semantic_key NOT NULL,
    copy_type            text         NOT NULL,
    store_ref            semantic_key NOT NULL,
    processor_ref        text         NOT NULL DEFAULT '',
    region               text         NOT NULL,
    field_scope          jsonb        NOT NULL DEFAULT '{}'::jsonb,
    encryption_key_ref   text         NOT NULL DEFAULT '',
    retention_schedule_key semantic_key NOT NULL,
    hold_state           text         NOT NULL DEFAULT 'NONE',
    deletion_capability  text         NOT NULL,
    last_verified_at     timestamptz,
    created_at           timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, copy_id),
    FOREIGN KEY (tenant_id, inventory_id) REFERENCES data_copy_inventory (tenant_id, inventory_id),
    CONSTRAINT data_copy_unique UNIQUE (tenant_id, inventory_id, store_ref, copy_type),
    CONSTRAINT data_copy_type_allowed CHECK (copy_type IN (
        'AUTHORITATIVE','PROJECTION','SEARCH','VECTOR','CACHE','TELEMETRY','AGENT_MEMORY','EXPORT','BACKUP','PROVIDER'
    )),
    CONSTRAINT data_copy_hold_allowed CHECK (hold_state IN ('NONE','HELD','RELEASED')),
    CONSTRAINT data_copy_deletion_allowed CHECK (deletion_capability IN ('DELETE','ANONYMIZE','TOMBSTONE','NONE')),
    CONSTRAINT data_copy_field_scope_object CHECK (jsonb_typeof(field_scope) = 'object'),
    -- A copy held by a processor always names the processor it sits with.
    CONSTRAINT data_copy_provider_names_processor CHECK (copy_type <> 'PROVIDER' OR length(processor_ref) > 0)
);
CREATE INDEX IF NOT EXISTS data_copy_asset ON data_copy (tenant_id, canonical_asset_key);

-- ---------------------------------------------------------------------------
-- Records: declarations, holds and dispositions
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS record_declaration (
    tenant_id                tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    declaration_id           uuid         NOT NULL,
    subject_entity_ref       semantic_key NOT NULL,
    artifact_ref             text         NOT NULL DEFAULT '',
    record_series            semantic_key NOT NULL,
    record_class             text         NOT NULL,
    owner_ref                semantic_key NOT NULL,
    custodian_ref            semantic_key NOT NULL,
    cutoff_trigger           text         NOT NULL,
    cutoff_at                timestamptz,
    retention_schedule_key   semantic_key NOT NULL,
    retention_schedule_version cas_version NOT NULL,
    disposition_eligible_at  timestamptz,
    corrects_declaration_id  uuid,
    status                   text         NOT NULL DEFAULT 'DECLARED',
    created_at               timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, declaration_id),
    FOREIGN KEY (tenant_id, corrects_declaration_id) REFERENCES record_declaration (tenant_id, declaration_id),
    CONSTRAINT record_declaration_trigger_allowed CHECK (
        cutoff_trigger IN ('CREATION','TERMINATION','EVENT','FISCAL_YEAR_END','SUPERSESSION')
    ),
    CONSTRAINT record_declaration_status_allowed CHECK (
        status IN ('DECLARED','ELIGIBLE','HELD','DISPOSED','SUPERSEDED')
    ),
    CONSTRAINT record_declaration_not_self_correcting CHECK (
        corrects_declaration_id IS NULL OR corrects_declaration_id <> declaration_id
    ),
    -- Eligibility is always derived from a cutoff, never asserted alone.
    CONSTRAINT record_declaration_eligibility_needs_cutoff CHECK (
        disposition_eligible_at IS NULL OR cutoff_at IS NOT NULL
    ),
    CONSTRAINT record_declaration_eligible_after_cutoff CHECK (
        disposition_eligible_at IS NULL OR disposition_eligible_at >= cutoff_at
    ),
    CONSTRAINT record_declaration_eligible_status_has_date CHECK (
        status <> 'ELIGIBLE' OR disposition_eligible_at IS NOT NULL
    )
);
CREATE INDEX IF NOT EXISTS record_declaration_subject ON record_declaration (tenant_id, subject_entity_ref);

CREATE TABLE IF NOT EXISTS legal_hold (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    hold_id         uuid           NOT NULL,
    matter_ref      semantic_key   NOT NULL,
    authority_ref   semantic_key   NOT NULL,
    hold_version    cas_version    NOT NULL DEFAULT 1,
    reason          text           NOT NULL,
    scope_predicate jsonb          NOT NULL DEFAULT '{}'::jsonb,
    scope_digest    content_digest NOT NULL,
    placed_by       semantic_key   NOT NULL,
    placed_at       timestamptz    NOT NULL,
    released_by     semantic_key,
    released_at     timestamptz,
    release_reason  text           NOT NULL DEFAULT '',
    status          text           NOT NULL DEFAULT 'ACTIVE',
    PRIMARY KEY (tenant_id, hold_id),
    CONSTRAINT legal_hold_matter_unique UNIQUE (tenant_id, matter_ref, hold_version),
    CONSTRAINT legal_hold_scope_object CHECK (jsonb_typeof(scope_predicate) = 'object'),
    CONSTRAINT legal_hold_status_allowed CHECK (status IN ('ACTIVE','PARTIALLY_RELEASED','RELEASED')),
    CONSTRAINT legal_hold_reason_present CHECK (length(reason) > 0),
    CONSTRAINT legal_hold_released_after_placed CHECK (released_at IS NULL OR released_at >= placed_at),
    -- A release always names who released it and why.
    CONSTRAINT legal_hold_release_is_attributed CHECK (
        status <> 'RELEASED' OR (released_at IS NOT NULL AND released_by IS NOT NULL AND length(release_reason) > 0)
    )
);

CREATE TABLE IF NOT EXISTS hold_intersection (
    tenant_id       tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    intersection_id uuid         NOT NULL,
    hold_id         uuid         NOT NULL,
    declaration_id  uuid         NOT NULL,
    copy_id         uuid,
    matched_reason  text         NOT NULL,
    matched_at      timestamptz  NOT NULL,
    released_at     timestamptz,
    state           text         NOT NULL DEFAULT 'ACTIVE',
    PRIMARY KEY (tenant_id, intersection_id),
    FOREIGN KEY (tenant_id, hold_id) REFERENCES legal_hold (tenant_id, hold_id),
    FOREIGN KEY (tenant_id, declaration_id) REFERENCES record_declaration (tenant_id, declaration_id),
    FOREIGN KEY (tenant_id, copy_id) REFERENCES data_copy (tenant_id, copy_id),
    CONSTRAINT hold_intersection_unique UNIQUE (tenant_id, hold_id, declaration_id),
    CONSTRAINT hold_intersection_state_allowed CHECK (state IN ('ACTIVE','RELEASED')),
    CONSTRAINT hold_intersection_released_state CHECK (
        (state = 'RELEASED') = (released_at IS NOT NULL)
    ),
    CONSTRAINT hold_intersection_released_after_matched CHECK (released_at IS NULL OR released_at >= matched_at)
);
CREATE INDEX IF NOT EXISTS hold_intersection_declaration ON hold_intersection (tenant_id, declaration_id) WHERE state = 'ACTIVE';

-- retention_disposition executes a record's end of life. blocking_hold_id is
-- the schema's own answer to DB-015's RED clause: while a hold names this
-- disposition, it cannot record an execution at all.
CREATE TABLE IF NOT EXISTS retention_disposition (
    tenant_id               tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    disposition_id          uuid         NOT NULL,
    declaration_id          uuid         NOT NULL,
    action                  text         NOT NULL,
    retention_schedule_key  semantic_key NOT NULL,
    retention_schedule_version cas_version NOT NULL,
    due_at                  timestamptz  NOT NULL,
    executed_at             timestamptz,
    method                  text         NOT NULL DEFAULT '',
    receipt_digest          content_digest,
    certificate_digest      content_digest,
    verification_result     text         NOT NULL DEFAULT 'PENDING',
    blocking_hold_id        uuid,
    corrects_disposition_id uuid,
    status                  text         NOT NULL DEFAULT 'PLANNED',
    created_at              timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, disposition_id),
    FOREIGN KEY (tenant_id, declaration_id) REFERENCES record_declaration (tenant_id, declaration_id),
    FOREIGN KEY (tenant_id, blocking_hold_id) REFERENCES legal_hold (tenant_id, hold_id),
    FOREIGN KEY (tenant_id, corrects_disposition_id) REFERENCES retention_disposition (tenant_id, disposition_id),
    CONSTRAINT retention_disposition_action_allowed CHECK (action IN ('DESTROY','ANONYMIZE','RESTRICT','TRANSFER')),
    CONSTRAINT retention_disposition_status_allowed CHECK (
        status IN ('PLANNED','BLOCKED','EXECUTED','VERIFIED','CANCELLED')
    ),
    CONSTRAINT retention_disposition_verification_allowed CHECK (
        verification_result IN ('PENDING','PASS','FAIL','PARTIAL','UNKNOWN')
    ),
    CONSTRAINT retention_disposition_not_self_correcting CHECK (
        corrects_disposition_id IS NULL OR corrects_disposition_id <> disposition_id
    ),
    -- A hold blocks execution outright.
    CONSTRAINT retention_disposition_hold_blocks_execution CHECK (
        blocking_hold_id IS NULL OR executed_at IS NULL
    ),
    CONSTRAINT retention_disposition_blocked_names_hold CHECK (
        status <> 'BLOCKED' OR blocking_hold_id IS NOT NULL
    ),
    -- An executed disposition always carries its receipt; a verified one
    -- also carries its destruction certificate.
    CONSTRAINT retention_disposition_executed_has_receipt CHECK (
        status NOT IN ('EXECUTED','VERIFIED') OR (executed_at IS NOT NULL AND receipt_digest IS NOT NULL AND length(method) > 0)
    ),
    CONSTRAINT retention_disposition_verified_has_certificate CHECK (
        status <> 'VERIFIED' OR (certificate_digest IS NOT NULL AND verification_result = 'PASS')
    ),
    CONSTRAINT retention_disposition_executed_not_before_due CHECK (executed_at IS NULL OR executed_at >= due_at)
);
CREATE INDEX IF NOT EXISTS retention_disposition_declaration ON retention_disposition (tenant_id, declaration_id);

-- ---------------------------------------------------------------------------
-- Operations: incidents, backup and recovery
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS operational_incident (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    incident_id      uuid           NOT NULL,
    incident_key     semantic_key   NOT NULL,
    severity         text           NOT NULL,
    impact_revision  cas_version    NOT NULL DEFAULT 1,
    scope            jsonb          NOT NULL DEFAULT '{}'::jsonb,
    correlation_key  semantic_key   NOT NULL,
    evidence_digest  content_digest NOT NULL,
    declared_at      timestamptz    NOT NULL,
    contained_at     timestamptz,
    resolved_at      timestamptz,
    residual_risk    text           NOT NULL DEFAULT '',
    postmortem_ref   text           NOT NULL DEFAULT '',
    status           text           NOT NULL DEFAULT 'OPEN',
    PRIMARY KEY (tenant_id, incident_id),
    CONSTRAINT operational_incident_key_unique UNIQUE (tenant_id, incident_key),
    CONSTRAINT operational_incident_severity_allowed CHECK (severity IN ('SEV1','SEV2','SEV3','SEV4','SEV5')),
    CONSTRAINT operational_incident_status_allowed CHECK (status IN ('OPEN','CONTAINED','RESOLVED','CLOSED')),
    CONSTRAINT operational_incident_scope_object CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT operational_incident_contained_after_declared CHECK (contained_at IS NULL OR contained_at >= declared_at),
    CONSTRAINT operational_incident_resolved_after_contained CHECK (
        resolved_at IS NULL OR (contained_at IS NOT NULL AND resolved_at >= contained_at)
    ),
    CONSTRAINT operational_incident_resolved_status_has_time CHECK (
        status NOT IN ('RESOLVED','CLOSED') OR resolved_at IS NOT NULL
    ),
    -- A closed incident always carries its postmortem reference.
    CONSTRAINT operational_incident_closed_has_postmortem CHECK (
        status <> 'CLOSED' OR length(postmortem_ref) > 0
    )
);

CREATE TABLE IF NOT EXISTS backup_run (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    run_id           uuid           NOT NULL,
    policy_key       semantic_key   NOT NULL,
    plane            text           NOT NULL,
    store_ref        semantic_key   NOT NULL,
    backup_mode      text           NOT NULL,
    point_in_time    timestamptz    NOT NULL,
    watermark        text           NOT NULL,
    location_ref     text           NOT NULL,
    key_version      cas_version    NOT NULL,
    manifest_digest  content_digest NOT NULL,
    started_at       timestamptz    NOT NULL,
    completed_at     timestamptz,
    immutable_until  timestamptz,
    verification_result text        NOT NULL DEFAULT 'PENDING',
    status           text           NOT NULL DEFAULT 'RUNNING',
    PRIMARY KEY (tenant_id, run_id),
    CONSTRAINT backup_run_policy_point_unique UNIQUE (tenant_id, policy_key, store_ref, point_in_time),
    CONSTRAINT backup_run_plane_allowed CHECK (plane IN ('DATA','CONTROL','OBJECT_STORE','SEARCH')),
    CONSTRAINT backup_run_mode_allowed CHECK (backup_mode IN ('FULL','INCREMENTAL','SNAPSHOT')),
    CONSTRAINT backup_run_status_allowed CHECK (status IN ('RUNNING','COMPLETED','FAILED','EXPIRED')),
    CONSTRAINT backup_run_verification_allowed CHECK (verification_result IN ('PENDING','PASS','FAIL','PARTIAL','UNKNOWN')),
    CONSTRAINT backup_run_completed_after_started CHECK (completed_at IS NULL OR completed_at >= started_at),
    CONSTRAINT backup_run_terminal_has_completion CHECK (status = 'RUNNING' OR completed_at IS NOT NULL),
    -- A completed backup is immutable for a stated window; that is what
    -- makes it usable as a recovery source at all.
    CONSTRAINT backup_run_completed_is_immutable CHECK (
        status <> 'COMPLETED' OR (immutable_until IS NOT NULL AND immutable_until > completed_at)
    )
);

CREATE TABLE IF NOT EXISTS recovery_run (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    run_id               uuid         NOT NULL,
    scenario_key         semantic_key NOT NULL,
    plan_ref             semantic_key NOT NULL,
    backup_run_id        uuid,
    incident_id          uuid,
    recovery_mode        text         NOT NULL,
    isolated_environment text         NOT NULL,
    target_rpo_seconds   bigint       NOT NULL,
    target_rto_seconds   bigint       NOT NULL,
    actual_rpo_seconds   bigint,
    actual_rto_seconds   bigint,
    validation_result    text         NOT NULL DEFAULT 'PENDING',
    started_at           timestamptz  NOT NULL,
    completed_at         timestamptz,
    status               text         NOT NULL DEFAULT 'RUNNING',
    PRIMARY KEY (tenant_id, run_id),
    FOREIGN KEY (tenant_id, backup_run_id) REFERENCES backup_run (tenant_id, run_id),
    FOREIGN KEY (tenant_id, incident_id) REFERENCES operational_incident (tenant_id, incident_id),
    CONSTRAINT recovery_run_mode_allowed CHECK (recovery_mode IN ('RESTORE','REBUILD','REPLAY')),
    CONSTRAINT recovery_run_status_allowed CHECK (status IN ('RUNNING','COMPLETED','FAILED','ABORTED')),
    CONSTRAINT recovery_run_validation_allowed CHECK (validation_result IN ('PENDING','PASS','FAIL','PARTIAL','UNKNOWN')),
    CONSTRAINT recovery_run_objectives_positive CHECK (target_rpo_seconds >= 0 AND target_rto_seconds >= 0),
    CONSTRAINT recovery_run_actuals_nonnegative CHECK (
        (actual_rpo_seconds IS NULL OR actual_rpo_seconds >= 0) AND (actual_rto_seconds IS NULL OR actual_rto_seconds >= 0)
    ),
    CONSTRAINT recovery_run_completed_after_started CHECK (completed_at IS NULL OR completed_at >= started_at),
    -- A RESTORE recovers from a named backup; a REBUILD or REPLAY does not
    -- and must not pretend to.
    CONSTRAINT recovery_run_restore_names_backup CHECK (recovery_mode <> 'RESTORE' OR backup_run_id IS NOT NULL),
    -- A completed run reports what it actually achieved and how it was
    -- validated; it never closes on an unvalidated claim.
    CONSTRAINT recovery_run_completed_reports_actuals CHECK (
        status <> 'COMPLETED' OR (
            completed_at IS NOT NULL
            AND actual_rpo_seconds IS NOT NULL
            AND actual_rto_seconds IS NOT NULL
            AND validation_result IN ('PASS','PARTIAL')
        )
    )
);

-- ---------------------------------------------------------------------------
-- Assurance: SLO objectives, observations, control evidence, audit packages
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS slo_definition (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    slo_id         uuid         NOT NULL,
    slo_key        semantic_key NOT NULL,
    slo_version    cas_version  NOT NULL,
    service        semantic_key NOT NULL,
    capability     semantic_key NOT NULL,
    objective_kind text         NOT NULL,
    target_ratio   numeric(9,6) NOT NULL,
    window_seconds bigint       NOT NULL,
    owner_ref      semantic_key NOT NULL,
    effective_from timestamptz  NOT NULL,
    effective_to   timestamptz,
    PRIMARY KEY (tenant_id, slo_id),
    CONSTRAINT slo_definition_version_unique UNIQUE (tenant_id, slo_key, slo_version),
    CONSTRAINT slo_definition_kind_allowed CHECK (
        objective_kind IN ('AVAILABILITY','LATENCY','FRESHNESS','RECONCILIATION','RPO','RTO')
    ),
    CONSTRAINT slo_definition_target_in_unit_interval CHECK (target_ratio > 0 AND target_ratio <= 1),
    CONSTRAINT slo_definition_window_positive CHECK (window_seconds > 0),
    CONSTRAINT slo_definition_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);

CREATE TABLE IF NOT EXISTS slo_observation (
    tenant_id             tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    observation_id        uuid         NOT NULL,
    slo_id                uuid         NOT NULL,
    slo_version           cas_version  NOT NULL,
    window_start          timestamptz  NOT NULL,
    window_end            timestamptz  NOT NULL,
    good_events           bigint       NOT NULL,
    total_events          bigint       NOT NULL,
    error_budget_remaining numeric(9,6) NOT NULL,
    source_quality        text         NOT NULL,
    observed_at           timestamptz  NOT NULL,
    PRIMARY KEY (tenant_id, observation_id),
    FOREIGN KEY (tenant_id, slo_id) REFERENCES slo_definition (tenant_id, slo_id),
    CONSTRAINT slo_observation_window_unique UNIQUE (tenant_id, slo_id, window_start),
    CONSTRAINT slo_observation_window_ordered CHECK (window_end > window_start),
    CONSTRAINT slo_observation_counts_nonnegative CHECK (good_events >= 0 AND total_events >= 0),
    CONSTRAINT slo_observation_good_within_total CHECK (good_events <= total_events),
    CONSTRAINT slo_observation_budget_bounded CHECK (error_budget_remaining >= -1 AND error_budget_remaining <= 1),
    CONSTRAINT slo_observation_quality_allowed CHECK (source_quality IN ('COMPLETE','PARTIAL','DEGRADED','UNKNOWN'))
);

CREATE TABLE IF NOT EXISTS control_evidence (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    evidence_id        uuid           NOT NULL,
    control_key        semantic_key   NOT NULL,
    control_version    cas_version    NOT NULL,
    implementation_ref semantic_key   NOT NULL,
    scope              jsonb          NOT NULL DEFAULT '{}'::jsonb,
    window_start       timestamptz    NOT NULL,
    window_end         timestamptz    NOT NULL,
    source_ref         semantic_key   NOT NULL,
    artifact_digest    content_digest NOT NULL,
    collector          semantic_key   NOT NULL,
    completeness       text           NOT NULL,
    result             text           NOT NULL,
    deficiency_ref     text           NOT NULL DEFAULT '',
    freshness_deadline timestamptz    NOT NULL,
    retention_class    text           NOT NULL,
    collected_at       timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, evidence_id),
    CONSTRAINT control_evidence_window_unique UNIQUE (tenant_id, control_key, control_version, window_start),
    CONSTRAINT control_evidence_scope_object CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT control_evidence_window_ordered CHECK (window_end > window_start),
    CONSTRAINT control_evidence_completeness_allowed CHECK (completeness IN ('COMPLETE','PARTIAL','UNKNOWN')),
    CONSTRAINT control_evidence_result_allowed CHECK (result IN ('PASS','FAIL','PARTIAL','UNKNOWN')),
    CONSTRAINT control_evidence_retention_allowed CHECK (retention_class IN ('PERMANENT','OPERATIONAL','REBUILDABLE')),
    CONSTRAINT control_evidence_freshness_after_window CHECK (freshness_deadline > window_end),
    -- A failing or partial control names the deficiency it found.
    CONSTRAINT control_evidence_failure_names_deficiency CHECK (
        result NOT IN ('FAIL','PARTIAL') OR length(deficiency_ref) > 0
    )
);

CREATE TABLE IF NOT EXISTS audit_package (
    tenant_id        tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    package_id       uuid           NOT NULL,
    purpose          semantic_key   NOT NULL,
    scope            jsonb          NOT NULL DEFAULT '{}'::jsonb,
    evidence_ids     uuid[]         NOT NULL DEFAULT ARRAY[]::uuid[],
    manifest_digest  content_digest NOT NULL,
    signature_ref    text           NOT NULL,
    completeness     text           NOT NULL,
    generated_at     timestamptz    NOT NULL,
    delivered_at     timestamptz,
    expires_at       timestamptz    NOT NULL,
    status           text           NOT NULL DEFAULT 'GENERATED',
    PRIMARY KEY (tenant_id, package_id),
    CONSTRAINT audit_package_scope_object CHECK (jsonb_typeof(scope) = 'object'),
    CONSTRAINT audit_package_completeness_allowed CHECK (completeness IN ('COMPLETE','PARTIAL','UNKNOWN')),
    CONSTRAINT audit_package_status_allowed CHECK (status IN ('GENERATED','DELIVERED','EXPIRED','WITHDRAWN')),
    CONSTRAINT audit_package_expiry_after_generated CHECK (expires_at > generated_at),
    CONSTRAINT audit_package_delivered_after_generated CHECK (delivered_at IS NULL OR delivered_at >= generated_at),
    CONSTRAINT audit_package_delivered_status_has_time CHECK (status <> 'DELIVERED' OR delivered_at IS NOT NULL),
    -- An empty package is never a complete one.
    CONSTRAINT audit_package_complete_has_evidence CHECK (
        completeness <> 'COMPLETE' OR cardinality(evidence_ids) > 0
    ),
    -- The signature is a reference into the governed store, never a value.
    CONSTRAINT audit_package_signature_is_reference CHECK (
        signature_ref ~ '^(artifact|kms|provider)://[^[:space:]]+$'
    )
);

-- ---------------------------------------------------------------------------
-- Append-only enforcement
-- ---------------------------------------------------------------------------

CREATE TRIGGER data_copy_inventory_append_only BEFORE UPDATE OR DELETE ON data_copy_inventory
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER slo_observation_append_only BEFORE UPDATE OR DELETE ON slo_observation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER control_evidence_append_only BEFORE UPDATE OR DELETE ON control_evidence
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER audit_package_append_only BEFORE UPDATE OR DELETE ON audit_package
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- ---------------------------------------------------------------------------
-- Row level security
-- ---------------------------------------------------------------------------

ALTER TABLE processing_purpose_declaration ENABLE ROW LEVEL SECURITY;
ALTER TABLE processing_purpose_declaration FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON processing_purpose_declaration USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE data_copy_inventory ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_copy_inventory FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_copy_inventory USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE data_copy ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_copy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_copy USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE record_declaration ENABLE ROW LEVEL SECURITY;
ALTER TABLE record_declaration FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON record_declaration USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE legal_hold ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal_hold FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON legal_hold USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE hold_intersection ENABLE ROW LEVEL SECURITY;
ALTER TABLE hold_intersection FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON hold_intersection USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE retention_disposition ENABLE ROW LEVEL SECURITY;
ALTER TABLE retention_disposition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON retention_disposition USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE operational_incident ENABLE ROW LEVEL SECURITY;
ALTER TABLE operational_incident FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON operational_incident USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE backup_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE backup_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON backup_run USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE recovery_run ENABLE ROW LEVEL SECURITY;
ALTER TABLE recovery_run FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON recovery_run USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE slo_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE slo_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON slo_definition USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE slo_observation ENABLE ROW LEVEL SECURITY;
ALTER TABLE slo_observation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON slo_observation USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE control_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE control_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON control_evidence USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE audit_package ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_package FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_package USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- Privileges
-- ---------------------------------------------------------------------------

REVOKE UPDATE, DELETE ON data_copy_inventory FROM PUBLIC;
REVOKE UPDATE, DELETE ON slo_observation FROM PUBLIC;
REVOKE UPDATE, DELETE ON control_evidence FROM PUBLIC;
REVOKE UPDATE, DELETE ON audit_package FROM PUBLIC;
REVOKE DELETE ON processing_purpose_declaration FROM PUBLIC;
REVOKE DELETE ON data_copy FROM PUBLIC;
REVOKE DELETE ON record_declaration FROM PUBLIC;
REVOKE DELETE ON legal_hold FROM PUBLIC;
REVOKE DELETE ON hold_intersection FROM PUBLIC;
REVOKE DELETE ON retention_disposition FROM PUBLIC;
REVOKE DELETE ON operational_incident FROM PUBLIC;
REVOKE DELETE ON backup_run FROM PUBLIC;
REVOKE DELETE ON recovery_run FROM PUBLIC;
REVOKE DELETE ON slo_definition FROM PUBLIC;

GRANT SELECT, INSERT, UPDATE ON processing_purpose_declaration TO hcmnext_app;
GRANT SELECT, INSERT ON data_copy_inventory TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON data_copy TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON record_declaration TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON legal_hold TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON hold_intersection TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON retention_disposition TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON operational_incident TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON backup_run TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON recovery_run TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON slo_definition TO hcmnext_app;
GRANT SELECT, INSERT ON slo_observation TO hcmnext_app;
GRANT SELECT, INSERT ON control_evidence TO hcmnext_app;
GRANT SELECT, INSERT ON audit_package TO hcmnext_app;

-- +goose Down
REVOKE ALL ON audit_package FROM hcmnext_app;
REVOKE ALL ON control_evidence FROM hcmnext_app;
REVOKE ALL ON slo_observation FROM hcmnext_app;
REVOKE ALL ON slo_definition FROM hcmnext_app;
REVOKE ALL ON recovery_run FROM hcmnext_app;
REVOKE ALL ON backup_run FROM hcmnext_app;
REVOKE ALL ON operational_incident FROM hcmnext_app;
REVOKE ALL ON retention_disposition FROM hcmnext_app;
REVOKE ALL ON hold_intersection FROM hcmnext_app;
REVOKE ALL ON legal_hold FROM hcmnext_app;
REVOKE ALL ON record_declaration FROM hcmnext_app;
REVOKE ALL ON data_copy FROM hcmnext_app;
REVOKE ALL ON data_copy_inventory FROM hcmnext_app;
REVOKE ALL ON processing_purpose_declaration FROM hcmnext_app;

DROP TABLE audit_package;
DROP TABLE control_evidence;
DROP TABLE slo_observation;
DROP TABLE slo_definition;
DROP TABLE recovery_run;
DROP TABLE backup_run;
DROP TABLE operational_incident;
DROP TABLE retention_disposition;
DROP TABLE hold_intersection;
DROP TABLE legal_hold;
DROP TABLE record_declaration;
DROP TABLE data_copy;
DROP TABLE data_copy_inventory;
DROP TABLE processing_purpose_declaration;
