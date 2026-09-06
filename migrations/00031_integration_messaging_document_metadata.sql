-- Owner: connectivity/content lane. Phase: P1B.
-- DB-014: integration, messaging, document and artifact metadata.
--
-- Scope
-- -----
-- This migration materializes the connectivity, communication and content
-- metadata declared by planning/data/models/connectivity-access-content.md
-- that no earlier migration already owns. It deliberately does NOT create:
--
--   * integration_schema_snapshot / _evidence  -- migration 00025 owns them
--   * artifact / artifact_reference_event      -- migration 00010 owns them
--     artifact_retrieval_refusal                  (companion object store)
--   * artifact_quarantine / _state             -- migration 00030 owns them
--   * external_observation, observation_checkpoint -- migration 00007
--   * config_object*, intent control, workflow scheduling, ledger
--     checkpoint and tenant bootstrap tables    -- migrations 00024-00029
--
-- Three invariants this migration enforces in the schema rather than only in
-- Go, because DB-014's RED clause names them:
--
--  1. No raw secret. connector_connection stores a credential *reference*
--     whose scheme must be one of the governed secret stores, and its
--     configuration object may not carry a secret-bearing key at all
--     (connector_connection_configuration_has_no_secret). delivery_endpoint
--     stores an address digest, never the address; signature stores a
--     signature value *reference*, never the bytes.
--  2. Nothing loses tenant / correlation / idempotency / version /
--     classification / freshness / deadline. Every table is tenant scoped,
--     every foreign key is composite on tenant_id so no reference can cross
--     a tenant boundary, message_intent.correlation_key and
--     connector_operation.idempotency_key are NOT NULL and unique in their
--     scope, mapping_profile and document_version carry explicit versions,
--     external_operation_observation carries a freshness deadline and
--     connector_operation carries a deadline.
--  3. Provider acceptance is not business completion. A
--     connector_operation may only reach completion_state = 'COMPLETE'
--     when it names the external_operation_observation that confirmed the
--     external state (connector_operation_completion_requires_observation),
--     and a recipient_message may only be SATISFIED when it names the
--     delivery_receipt that satisfied it
--     (recipient_message_satisfaction_requires_receipt). A
--     PROVIDER_ACCEPTED delivery_attempt on its own can never do either.
--
-- Retention. The append-only evidence tables (integration_receipt,
-- integration_receipt_item, mapping_execution, connector_operation_attempt,
-- external_operation_observation, reconciliation_result, delivery_attempt,
-- delivery_receipt, document_version, signature and
-- document_artifact_reference) carry the forbid_mutation trigger 00001
-- defines, so they reject UPDATE and DELETE outright. The remaining tables
-- are live serving state.
--
-- Content-addressed bytes stay in the governed object store (DB-014
-- REFACTOR): every artifact-shaped column here is a content_digest or a
-- store reference, never a bytea.

-- +goose Up

-- ---------------------------------------------------------------------------
-- Integration and external-system metadata
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS external_system (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    system_id            uuid         NOT NULL,
    system_key           semantic_key NOT NULL,
    vendor               text         NOT NULL,
    product              text         NOT NULL,
    environment          text         NOT NULL,
    residency_region     text         NOT NULL,
    data_classification  text         NOT NULL,
    owner_principal_ref  text         NOT NULL,
    lifecycle            text         NOT NULL DEFAULT 'ACTIVE',
    health               text         NOT NULL DEFAULT 'UNKNOWN',
    created_at           timestamptz  NOT NULL DEFAULT now(),
    updated_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, system_id),
    CONSTRAINT external_system_key_unique UNIQUE (tenant_id, system_key),
    CONSTRAINT external_system_environment_allowed CHECK (environment IN ('SANDBOX','TEST','STAGING','PRODUCTION')),
    CONSTRAINT external_system_lifecycle_allowed CHECK (lifecycle IN ('ACTIVE','DEPRECATED','RETIRED')),
    CONSTRAINT external_system_health_allowed CHECK (health IN ('UNKNOWN','HEALTHY','DEGRADED','DOWN')),
    CONSTRAINT external_system_classification_present CHECK (length(data_classification) > 0),
    CONSTRAINT external_system_owner_present CHECK (length(owner_principal_ref) > 0)
);

CREATE TABLE IF NOT EXISTS connector_definition (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    connector_id          uuid           NOT NULL,
    connector_key         semantic_key   NOT NULL,
    connector_version     cas_version    NOT NULL,
    vendor                text           NOT NULL,
    supported_objects     jsonb          NOT NULL DEFAULT '{}'::jsonb,
    capabilities          jsonb          NOT NULL DEFAULT '{}'::jsonb,
    auth_mode             text           NOT NULL,
    write_mode            text           NOT NULL,
    idempotency_semantics text           NOT NULL,
    observation_semantics text           NOT NULL,
    descriptor_digest     content_digest NOT NULL,
    status                text           NOT NULL DEFAULT 'DRAFT',
    created_at            timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, connector_id),
    CONSTRAINT connector_definition_version_unique UNIQUE (tenant_id, connector_key, connector_version),
    CONSTRAINT connector_definition_objects_object CHECK (jsonb_typeof(supported_objects) = 'object'),
    CONSTRAINT connector_definition_capabilities_object CHECK (jsonb_typeof(capabilities) = 'object'),
    CONSTRAINT connector_definition_auth_mode_allowed CHECK (auth_mode IN ('OAUTH2','API_KEY','MTLS','SAML','BASIC','NONE')),
    CONSTRAINT connector_definition_write_mode_allowed CHECK (write_mode IN ('NONE','IDEMPOTENT','AT_LEAST_ONCE','TRANSACTIONAL')),
    CONSTRAINT connector_definition_status_allowed CHECK (status IN ('DRAFT','CERTIFIED','DEPRECATED','WITHDRAWN'))
);

-- connector_connection is the tenant's live binding of a connector to a
-- system. credential_ref names a governed secret store entry; the raw
-- secret never enters this row and the configuration object may not carry
-- a secret-bearing key (DB-014 RED: "stores raw secret").
CREATE TABLE IF NOT EXISTS connector_connection (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    connection_id      uuid         NOT NULL,
    system_id          uuid         NOT NULL,
    connector_id       uuid         NOT NULL,
    org_scope_id       uuid,
    environment        text         NOT NULL,
    endpoint           text         NOT NULL,
    credential_ref     text         NOT NULL,
    residency_region   text         NOT NULL,
    configuration      jsonb        NOT NULL DEFAULT '{}'::jsonb,
    generation         cas_version  NOT NULL DEFAULT 1,
    lifecycle          text         NOT NULL DEFAULT 'DRAFT',
    health             text         NOT NULL DEFAULT 'UNKNOWN',
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, connection_id),
    FOREIGN KEY (tenant_id, system_id) REFERENCES external_system (tenant_id, system_id),
    FOREIGN KEY (tenant_id, connector_id) REFERENCES connector_definition (tenant_id, connector_id),
    CONSTRAINT connector_connection_environment_allowed CHECK (environment IN ('SANDBOX','TEST','STAGING','PRODUCTION')),
    CONSTRAINT connector_connection_lifecycle_allowed CHECK (
        lifecycle IN ('DRAFT','VALIDATING','READY','ACTIVE','DEGRADED','SUSPENDED','REVOKED','QUARANTINED')
    ),
    CONSTRAINT connector_connection_health_allowed CHECK (health IN ('UNKNOWN','HEALTHY','DEGRADED','DOWN')),
    CONSTRAINT connector_connection_configuration_object CHECK (jsonb_typeof(configuration) = 'object'),
    -- A credential is always a reference into a governed secret store.
    CONSTRAINT connector_connection_credential_is_reference CHECK (
        credential_ref ~ '^(vault|kms|secretref|keyring)://[^[:space:]]+$'
    ),
    -- ... and the configuration blob may not smuggle one in beside it.
    CONSTRAINT connector_connection_configuration_has_no_secret CHECK (
        NOT (configuration ?| ARRAY['secret','password','token','api_key','apiKey','private_key','privateKey','client_secret','clientSecret'])
    )
);
CREATE INDEX IF NOT EXISTS connector_connection_system ON connector_connection (tenant_id, system_id);

CREATE TABLE IF NOT EXISTS mapping_profile (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    mapping_id           uuid           NOT NULL,
    mapping_key          semantic_key   NOT NULL,
    mapping_version      cas_version    NOT NULL,
    connection_id        uuid,
    source_schema_key    semantic_key   NOT NULL,
    destination_schema_key semantic_key NOT NULL,
    transform_language   text           NOT NULL,
    transform_version    cas_version    NOT NULL,
    field_rules          jsonb          NOT NULL DEFAULT '{}'::jsonb,
    lossiness_policy     text           NOT NULL,
    content_digest       content_digest NOT NULL,
    publication_state    text           NOT NULL DEFAULT 'DRAFT',
    effective_from       timestamptz    NOT NULL,
    effective_to         timestamptz,
    created_at           timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, mapping_id),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES connector_connection (tenant_id, connection_id),
    CONSTRAINT mapping_profile_version_unique UNIQUE (tenant_id, mapping_key, mapping_version),
    CONSTRAINT mapping_profile_rules_object CHECK (jsonb_typeof(field_rules) = 'object'),
    CONSTRAINT mapping_profile_lossiness_allowed CHECK (lossiness_policy IN ('LOSSLESS','LOSSY_DECLARED','LOSSY_REJECTED')),
    CONSTRAINT mapping_profile_state_allowed CHECK (publication_state IN ('DRAFT','PUBLISHED','SUPERSEDED','ROLLED_BACK')),
    CONSTRAINT mapping_profile_half_open CHECK (effective_to IS NULL OR effective_from < effective_to)
);

-- integration_receipt is the immutable record of one authenticated inbound
-- payload. dedupe_key is unique per connection so a replayed provider
-- delivery cannot become a second business fact.
CREATE TABLE IF NOT EXISTS integration_receipt (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    receipt_id           uuid           NOT NULL,
    connection_id        uuid           NOT NULL,
    provider_event_id    text           NOT NULL,
    dedupe_key           semantic_key   NOT NULL,
    correlation_key      semantic_key   NOT NULL,
    trust_profile_version cas_version   NOT NULL,
    auth_result          text           NOT NULL,
    signature_result     text           NOT NULL,
    replay_disposition   text           NOT NULL,
    raw_artifact_digest  content_digest NOT NULL,
    schema_key           semantic_key   NOT NULL,
    parser_version       cas_version    NOT NULL,
    classification       text           NOT NULL,
    received_at          timestamptz    NOT NULL,
    trusted_at           timestamptz    NOT NULL,
    disposition          text           NOT NULL,
    PRIMARY KEY (tenant_id, receipt_id),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES connector_connection (tenant_id, connection_id),
    CONSTRAINT integration_receipt_dedupe_unique UNIQUE (tenant_id, connection_id, dedupe_key),
    CONSTRAINT integration_receipt_auth_allowed CHECK (auth_result IN ('AUTHENTICATED','UNSIGNED','FAILED')),
    CONSTRAINT integration_receipt_signature_allowed CHECK (signature_result IN ('VALID','INVALID','ABSENT')),
    CONSTRAINT integration_receipt_replay_allowed CHECK (replay_disposition IN ('FRESH','REPLAY_REJECTED','REPLAY_ACCEPTED')),
    CONSTRAINT integration_receipt_disposition_allowed CHECK (disposition IN ('ACCEPTED','QUARANTINED','REJECTED')),
    -- An unsigned or invalid payload is never accepted outright.
    CONSTRAINT integration_receipt_unauthenticated_not_accepted CHECK (
        disposition <> 'ACCEPTED' OR (auth_result = 'AUTHENTICATED' AND signature_result = 'VALID')
    ),
    CONSTRAINT integration_receipt_trusted_not_before_received CHECK (trusted_at >= received_at)
);
CREATE INDEX IF NOT EXISTS integration_receipt_correlation ON integration_receipt (tenant_id, correlation_key);

CREATE TABLE IF NOT EXISTS integration_receipt_item (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    item_id         uuid           NOT NULL,
    receipt_id      uuid           NOT NULL,
    item_index      integer        NOT NULL,
    item_path       text           NOT NULL,
    payload_digest  content_digest NOT NULL,
    schema_result   text           NOT NULL,
    correlation_key semantic_key   NOT NULL,
    mapping_id      uuid,
    disposition     text           NOT NULL,
    error_reason    text           NOT NULL DEFAULT '',
    recorded_at     timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, item_id),
    FOREIGN KEY (tenant_id, receipt_id) REFERENCES integration_receipt (tenant_id, receipt_id),
    FOREIGN KEY (tenant_id, mapping_id) REFERENCES mapping_profile (tenant_id, mapping_id),
    CONSTRAINT integration_receipt_item_index_unique UNIQUE (tenant_id, receipt_id, item_index),
    CONSTRAINT integration_receipt_item_index_nonnegative CHECK (item_index >= 0),
    CONSTRAINT integration_receipt_item_schema_allowed CHECK (schema_result IN ('VALID','INVALID','UNKNOWN')),
    CONSTRAINT integration_receipt_item_disposition_allowed CHECK (disposition IN ('MAPPED','QUARANTINED','REJECTED','DEFERRED')),
    CONSTRAINT integration_receipt_item_rejected_has_reason CHECK (disposition <> 'REJECTED' OR length(error_reason) > 0)
);

CREATE TABLE IF NOT EXISTS mapping_execution (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    execution_id      uuid           NOT NULL,
    mapping_id        uuid           NOT NULL,
    mapping_version   cas_version    NOT NULL,
    item_id           uuid           NOT NULL,
    input_digest      content_digest NOT NULL,
    output_digest     content_digest NOT NULL,
    field_results     jsonb          NOT NULL DEFAULT '{}'::jsonb,
    presence_diagnostics jsonb       NOT NULL DEFAULT '{}'::jsonb,
    lossiness         text           NOT NULL,
    status            text           NOT NULL,
    executed_at       timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, execution_id),
    FOREIGN KEY (tenant_id, mapping_id) REFERENCES mapping_profile (tenant_id, mapping_id),
    FOREIGN KEY (tenant_id, item_id) REFERENCES integration_receipt_item (tenant_id, item_id),
    CONSTRAINT mapping_execution_field_results_object CHECK (jsonb_typeof(field_results) = 'object'),
    CONSTRAINT mapping_execution_presence_object CHECK (jsonb_typeof(presence_diagnostics) = 'object'),
    CONSTRAINT mapping_execution_lossiness_allowed CHECK (lossiness IN ('LOSSLESS','LOSSY','UNKNOWN')),
    CONSTRAINT mapping_execution_status_allowed CHECK (status IN ('APPLIED','QUARANTINED','FAILED')),
    CONSTRAINT mapping_execution_item_unique UNIQUE (tenant_id, item_id, mapping_id, mapping_version)
);

-- connector_operation is one node of the outbound effect graph.
-- causal_predecessor_id is the edge; the operation may only claim business
-- completion once it names the observation that proved it.
CREATE TABLE IF NOT EXISTS connector_operation (
    tenant_id               tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    operation_id            uuid           NOT NULL,
    connection_id           uuid           NOT NULL,
    causal_predecessor_id   uuid,
    sequence_no             bigint         NOT NULL,
    effect_ref              semantic_key   NOT NULL,
    workflow_ref            semantic_key   NOT NULL,
    semantic_operation      semantic_key   NOT NULL,
    resource_key            semantic_key   NOT NULL,
    mapping_id              uuid,
    expected_external_version text,
    canonical_payload_digest content_digest NOT NULL,
    mapped_payload_digest   content_digest NOT NULL,
    idempotency_key         semantic_key   NOT NULL,
    fence_token             bigint         NOT NULL DEFAULT 1,
    authority_digest        content_digest NOT NULL,
    classification          text           NOT NULL,
    deadline_at             timestamptz    NOT NULL,
    state                   text           NOT NULL DEFAULT 'PLANNED',
    completion_state        text           NOT NULL DEFAULT 'PENDING',
    observation_id          uuid,
    created_at              timestamptz    NOT NULL DEFAULT now(),
    updated_at              timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES connector_connection (tenant_id, connection_id),
    FOREIGN KEY (tenant_id, causal_predecessor_id) REFERENCES connector_operation (tenant_id, operation_id),
    FOREIGN KEY (tenant_id, mapping_id) REFERENCES mapping_profile (tenant_id, mapping_id),
    CONSTRAINT connector_operation_idempotency_unique UNIQUE (tenant_id, connection_id, idempotency_key),
    CONSTRAINT connector_operation_not_self_caused CHECK (causal_predecessor_id IS NULL OR causal_predecessor_id <> operation_id),
    CONSTRAINT connector_operation_state_allowed CHECK (
        state IN ('PLANNED','SUBMITTED','PROVIDER_ACCEPTED','OBSERVED','RECONCILED','FAILED','AMBIGUOUS')
    ),
    CONSTRAINT connector_operation_completion_allowed CHECK (completion_state IN ('PENDING','COMPLETE','ABANDONED')),
    -- DB-014 RED: provider acceptance is not business completion.
    CONSTRAINT connector_operation_completion_requires_observation CHECK (
        completion_state <> 'COMPLETE' OR observation_id IS NOT NULL
    ),
    CONSTRAINT connector_operation_fence_positive CHECK (fence_token >= 1),
    CONSTRAINT connector_operation_sequence_positive CHECK (sequence_no >= 1)
);
CREATE INDEX IF NOT EXISTS connector_operation_connection ON connector_operation (tenant_id, connection_id, sequence_no);

CREATE TABLE IF NOT EXISTS connector_operation_attempt (
    tenant_id           tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    attempt_id          uuid           NOT NULL,
    operation_id        uuid           NOT NULL,
    attempt_number      integer        NOT NULL,
    request_digest      content_digest NOT NULL,
    response_digest     content_digest,
    provider_request_id text           NOT NULL DEFAULT '',
    fence_token         bigint         NOT NULL,
    attempted_at        timestamptz    NOT NULL,
    received_at         timestamptz,
    provider_result     text           NOT NULL,
    retry_disposition   text           NOT NULL,
    PRIMARY KEY (tenant_id, attempt_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    CONSTRAINT connector_operation_attempt_number_unique UNIQUE (tenant_id, operation_id, attempt_number),
    CONSTRAINT connector_operation_attempt_number_positive CHECK (attempt_number >= 1),
    CONSTRAINT connector_operation_attempt_result_allowed CHECK (
        provider_result IN ('SUCCESS','FAILURE','PARTIAL','UNKNOWN','AMBIGUOUS','PENDING')
    ),
    CONSTRAINT connector_operation_attempt_retry_allowed CHECK (
        retry_disposition IN ('NONE','RETRY','DEAD_LETTER','OBSERVATION_REQUIRED','REPAIR_REQUIRED')
    ),
    -- An ambiguous or unknown provider answer always routes to observation
    -- or repair; it is never silently retried as if nothing happened.
    CONSTRAINT connector_operation_attempt_ambiguity_routed CHECK (
        provider_result NOT IN ('AMBIGUOUS','UNKNOWN') OR retry_disposition IN ('OBSERVATION_REQUIRED','REPAIR_REQUIRED','DEAD_LETTER')
    ),
    CONSTRAINT connector_operation_attempt_received_after_attempted CHECK (received_at IS NULL OR received_at >= attempted_at)
);

CREATE TABLE IF NOT EXISTS external_operation_observation (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    observation_id     uuid           NOT NULL,
    operation_id       uuid           NOT NULL,
    resource_key       semantic_key   NOT NULL,
    observed_state     jsonb          NOT NULL DEFAULT '{}'::jsonb,
    observed_digest    content_digest NOT NULL,
    provider_version   text           NOT NULL DEFAULT '',
    watermark          text           NOT NULL DEFAULT '',
    completeness       text           NOT NULL,
    authority          text           NOT NULL,
    observed_at        timestamptz    NOT NULL,
    received_at        timestamptz    NOT NULL,
    freshness_deadline timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, observation_id),
    FOREIGN KEY (tenant_id, operation_id) REFERENCES connector_operation (tenant_id, operation_id),
    CONSTRAINT external_operation_observation_completeness_allowed CHECK (
        completeness IN ('CURRENT','REVISION','UNKNOWN','INCOMPLETE','STALE')
    ),
    CONSTRAINT external_operation_observation_authority_allowed CHECK (authority IN ('AUTHORITATIVE','ADVISORY','UNTRUSTED')),
    CONSTRAINT external_operation_observation_received_after_observed CHECK (received_at >= observed_at),
    CONSTRAINT external_operation_observation_freshness_after_observed CHECK (freshness_deadline > observed_at)
);
CREATE INDEX IF NOT EXISTS external_operation_observation_operation
    ON external_operation_observation (tenant_id, operation_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS reconciliation_job (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    job_id         uuid         NOT NULL,
    connection_id  uuid         NOT NULL,
    object_key     semantic_key NOT NULL,
    direction      text         NOT NULL,
    mode           text         NOT NULL,
    cursor_token   text         NOT NULL DEFAULT '',
    watermark      text         NOT NULL DEFAULT '',
    counts         jsonb        NOT NULL DEFAULT '{}'::jsonb,
    started_at     timestamptz  NOT NULL,
    completed_at   timestamptz,
    status         text         NOT NULL DEFAULT 'RUNNING',
    PRIMARY KEY (tenant_id, job_id),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES connector_connection (tenant_id, connection_id),
    CONSTRAINT reconciliation_job_direction_allowed CHECK (direction IN ('INBOUND','OUTBOUND','BIDIRECTIONAL')),
    CONSTRAINT reconciliation_job_mode_allowed CHECK (mode IN ('FULL','DELTA','SPOT')),
    CONSTRAINT reconciliation_job_status_allowed CHECK (status IN ('RUNNING','COMPLETED','FAILED','ABORTED')),
    CONSTRAINT reconciliation_job_counts_object CHECK (jsonb_typeof(counts) = 'object'),
    CONSTRAINT reconciliation_job_completed_after_started CHECK (completed_at IS NULL OR completed_at >= started_at),
    CONSTRAINT reconciliation_job_terminal_has_completion CHECK (status = 'RUNNING' OR completed_at IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS reconciliation_result (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    result_id         uuid           NOT NULL,
    job_id            uuid           NOT NULL,
    resource_key      semantic_key   NOT NULL,
    expected_digest   content_digest NOT NULL,
    observed_digest   content_digest,
    comparison_result text           NOT NULL,
    discrepancy_count integer        NOT NULL DEFAULT 0,
    severity          text           NOT NULL DEFAULT 'NONE',
    repair_plan_ref   text           NOT NULL DEFAULT '',
    recorded_at       timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, result_id),
    FOREIGN KEY (tenant_id, job_id) REFERENCES reconciliation_job (tenant_id, job_id),
    CONSTRAINT reconciliation_result_resource_unique UNIQUE (tenant_id, job_id, resource_key),
    CONSTRAINT reconciliation_result_comparison_allowed CHECK (comparison_result IN ('PASS','FAIL','PARTIAL','UNKNOWN')),
    CONSTRAINT reconciliation_result_severity_allowed CHECK (severity IN ('NONE','LOW','MEDIUM','HIGH','CRITICAL')),
    CONSTRAINT reconciliation_result_count_nonnegative CHECK (discrepancy_count >= 0),
    CONSTRAINT reconciliation_result_pass_has_no_discrepancy CHECK (comparison_result <> 'PASS' OR discrepancy_count = 0),
    CONSTRAINT reconciliation_result_failure_has_discrepancy CHECK (comparison_result <> 'FAIL' OR discrepancy_count > 0)
);

-- ---------------------------------------------------------------------------
-- Communication metadata
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS message_intent (
    tenant_id            tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    message_intent_id    uuid         NOT NULL,
    purpose              text         NOT NULL,
    org_scope_id         uuid,
    audience_expression  jsonb        NOT NULL DEFAULT '{}'::jsonb,
    template_key         semantic_key NOT NULL,
    template_version     cas_version  NOT NULL,
    classification       text         NOT NULL,
    urgency              text         NOT NULL,
    delivery_requirement text         NOT NULL,
    response_requirement text         NOT NULL DEFAULT 'NONE',
    workflow_ref         semantic_key NOT NULL,
    correlation_key      semantic_key NOT NULL,
    expires_at           timestamptz,
    created_at           timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, message_intent_id),
    CONSTRAINT message_intent_purpose_allowed CHECK (purpose IN (
        'APPROVAL','TASK','REMINDER','NOTICE','LEGAL_NOTICE','INCIDENT','EMPLOYEE_MESSAGE',
        'WORKFLOW_UPDATE','SURVEY_INVITATION','SURVEY_REMINDER','ANNOUNCEMENT','RECOGNITION',
        'ACKNOWLEDGEMENT_REQUEST','SYSTEM_ALERT','LEGAL_CORRECTION'
    )),
    CONSTRAINT message_intent_audience_object CHECK (jsonb_typeof(audience_expression) = 'object'),
    CONSTRAINT message_intent_urgency_allowed CHECK (urgency IN ('LOW','NORMAL','HIGH','CRITICAL')),
    CONSTRAINT message_intent_delivery_allowed CHECK (
        delivery_requirement IN ('BEST_EFFORT','DELIVERED','READ','ACKNOWLEDGED','SIGNED')
    ),
    CONSTRAINT message_intent_response_allowed CHECK (response_requirement IN ('NONE','REPLY','DECISION','SIGNATURE')),
    CONSTRAINT message_intent_expiry_after_created CHECK (expires_at IS NULL OR expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS message_intent_correlation ON message_intent (tenant_id, correlation_key);

-- delivery_endpoint keeps the address digest, never the address (DB-014 RED:
-- no raw secret or contact datum in an ordinary relational row).
CREATE TABLE IF NOT EXISTS delivery_endpoint (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    endpoint_id        uuid           NOT NULL,
    principal_ref      semantic_key   NOT NULL,
    channel            text           NOT NULL,
    address_digest     content_digest NOT NULL,
    address_hint       text           NOT NULL DEFAULT '',
    provider_account   text           NOT NULL DEFAULT '',
    ownership          text           NOT NULL,
    verification_state text           NOT NULL,
    purpose_scope      jsonb          NOT NULL DEFAULT '{}'::jsonb,
    locale             text           NOT NULL DEFAULT 'en-US',
    effective_from     timestamptz    NOT NULL,
    effective_to       timestamptz,
    status             text           NOT NULL DEFAULT 'ACTIVE',
    PRIMARY KEY (tenant_id, endpoint_id),
    CONSTRAINT delivery_endpoint_channel_allowed CHECK (channel IN ('EMAIL','SMS','PUSH','INBOX','VOICE','POSTAL','WEBHOOK')),
    CONSTRAINT delivery_endpoint_ownership_allowed CHECK (ownership IN ('BUSINESS','PERSONAL')),
    CONSTRAINT delivery_endpoint_verification_allowed CHECK (verification_state IN ('UNVERIFIED','PENDING','VERIFIED','EXPIRED')),
    CONSTRAINT delivery_endpoint_status_allowed CHECK (status IN ('ACTIVE','SUPPRESSED','REVOKED')),
    CONSTRAINT delivery_endpoint_purpose_object CHECK (jsonb_typeof(purpose_scope) = 'object'),
    CONSTRAINT delivery_endpoint_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    -- A hint is a display fragment ("j***@example.com"), never the address.
    CONSTRAINT delivery_endpoint_hint_is_short CHECK (length(address_hint) <= 32),
    CONSTRAINT delivery_endpoint_digest_unique UNIQUE (tenant_id, principal_ref, channel, address_digest)
);

CREATE TABLE IF NOT EXISTS recipient_message (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    recipient_message_id uuid           NOT NULL,
    message_intent_id    uuid           NOT NULL,
    recipient_ref        semantic_key   NOT NULL,
    endpoint_id          uuid           NOT NULL,
    rendered_digest      content_digest NOT NULL,
    classification       text           NOT NULL,
    correlation_key      semantic_key   NOT NULL,
    recipient_state      text           NOT NULL DEFAULT 'UNSEEN',
    satisfaction_state   text           NOT NULL DEFAULT 'PENDING',
    satisfying_receipt_id uuid,
    created_at           timestamptz    NOT NULL DEFAULT now(),
    updated_at           timestamptz    NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, recipient_message_id),
    FOREIGN KEY (tenant_id, message_intent_id) REFERENCES message_intent (tenant_id, message_intent_id),
    FOREIGN KEY (tenant_id, endpoint_id) REFERENCES delivery_endpoint (tenant_id, endpoint_id),
    CONSTRAINT recipient_message_unique UNIQUE (tenant_id, message_intent_id, recipient_ref),
    CONSTRAINT recipient_message_state_allowed CHECK (
        recipient_state IN ('UNSEEN','SEEN','READ','ACKNOWLEDGED','RESPONDED')
    ),
    CONSTRAINT recipient_message_satisfaction_allowed CHECK (satisfaction_state IN ('PENDING','SATISFIED','INVALIDATED')),
    -- DB-014 RED: provider acceptance is not business completion. Only a
    -- recorded receipt can satisfy a delivery requirement.
    CONSTRAINT recipient_message_satisfaction_requires_receipt CHECK (
        satisfaction_state <> 'SATISFIED' OR satisfying_receipt_id IS NOT NULL
    )
);

CREATE TABLE IF NOT EXISTS delivery_attempt (
    tenant_id            tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    attempt_id           uuid           NOT NULL,
    recipient_message_id uuid           NOT NULL,
    endpoint_id          uuid           NOT NULL,
    provider             text           NOT NULL,
    idempotency_key      semantic_key   NOT NULL,
    payload_digest       content_digest NOT NULL,
    state                text           NOT NULL,
    provider_message_id  text           NOT NULL DEFAULT '',
    submitted_at         timestamptz    NOT NULL,
    deadline_at          timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, attempt_id),
    FOREIGN KEY (tenant_id, recipient_message_id) REFERENCES recipient_message (tenant_id, recipient_message_id),
    FOREIGN KEY (tenant_id, endpoint_id) REFERENCES delivery_endpoint (tenant_id, endpoint_id),
    CONSTRAINT delivery_attempt_idempotency_unique UNIQUE (tenant_id, recipient_message_id, idempotency_key),
    CONSTRAINT delivery_attempt_state_allowed CHECK (state IN (
        'QUEUED','SUBMITTED','PROVIDER_ACCEPTED','DELIVERED','BOUNCED','REJECTED','EXPIRED','FAILED','AMBIGUOUS'
    )),
    CONSTRAINT delivery_attempt_deadline_after_submitted CHECK (deadline_at > submitted_at)
);

CREATE TABLE IF NOT EXISTS delivery_receipt (
    tenant_id          tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    receipt_id         uuid           NOT NULL,
    attempt_id         uuid           NOT NULL,
    provider_event_id  text           NOT NULL,
    event_type         text           NOT NULL,
    event_time         timestamptz    NOT NULL,
    received_at        timestamptz    NOT NULL,
    signature_result   text           NOT NULL,
    sequence_no        bigint         NOT NULL,
    dedupe_state       text           NOT NULL,
    normalized_result  text           NOT NULL,
    raw_artifact_digest content_digest NOT NULL,
    PRIMARY KEY (tenant_id, receipt_id),
    FOREIGN KEY (tenant_id, attempt_id) REFERENCES delivery_attempt (tenant_id, attempt_id),
    CONSTRAINT delivery_receipt_event_unique UNIQUE (tenant_id, attempt_id, provider_event_id),
    CONSTRAINT delivery_receipt_signature_allowed CHECK (signature_result IN ('VALID','INVALID','ABSENT')),
    CONSTRAINT delivery_receipt_dedupe_allowed CHECK (dedupe_state IN ('FIRST','DUPLICATE','OUT_OF_ORDER')),
    CONSTRAINT delivery_receipt_normalized_allowed CHECK (
        normalized_result IN ('DELIVERED','READ','ACKNOWLEDGED','BOUNCED','REJECTED','EXPIRED','UNKNOWN')
    ),
    CONSTRAINT delivery_receipt_received_after_event CHECK (received_at >= event_time),
    CONSTRAINT delivery_receipt_sequence_positive CHECK (sequence_no >= 1)
);

-- recipient_message.satisfying_receipt_id can only be added once
-- delivery_receipt exists, so its composite foreign key lands here.
ALTER TABLE recipient_message
    ADD CONSTRAINT recipient_message_satisfying_receipt_fk
    FOREIGN KEY (tenant_id, satisfying_receipt_id) REFERENCES delivery_receipt (tenant_id, receipt_id);

CREATE TABLE IF NOT EXISTS conversation_thread (
    tenant_id      tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    thread_id      uuid         NOT NULL,
    subject_ref    semantic_key NOT NULL,
    matter_ref     text         NOT NULL DEFAULT '',
    workflow_ref   text         NOT NULL DEFAULT '',
    purpose        text         NOT NULL,
    classification text         NOT NULL,
    opened_at      timestamptz  NOT NULL,
    closed_at      timestamptz,
    status         text         NOT NULL DEFAULT 'OPEN',
    PRIMARY KEY (tenant_id, thread_id),
    CONSTRAINT conversation_thread_status_allowed CHECK (status IN ('OPEN','CLOSED','MERGED','SPLIT')),
    CONSTRAINT conversation_thread_closed_after_opened CHECK (closed_at IS NULL OR closed_at >= opened_at),
    CONSTRAINT conversation_thread_closed_when_terminal CHECK (status <> 'CLOSED' OR closed_at IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS thread_participant (
    tenant_id              tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    participant_id         uuid           NOT NULL,
    thread_id              uuid           NOT NULL,
    principal_ref          semantic_key   NOT NULL,
    participant_role       text           NOT NULL,
    membership_from        timestamptz    NOT NULL,
    membership_to          timestamptz,
    authorization_digest   content_digest NOT NULL,
    historical_visibility  text           NOT NULL,
    status                 text           NOT NULL DEFAULT 'ACTIVE',
    PRIMARY KEY (tenant_id, participant_id),
    FOREIGN KEY (tenant_id, thread_id) REFERENCES conversation_thread (tenant_id, thread_id),
    CONSTRAINT thread_participant_unique UNIQUE (tenant_id, thread_id, principal_ref, membership_from),
    CONSTRAINT thread_participant_role_allowed CHECK (participant_role IN ('OWNER','MEMBER','OBSERVER','DELEGATE')),
    CONSTRAINT thread_participant_visibility_allowed CHECK (
        historical_visibility IN ('NONE','FROM_JOIN','FULL_HISTORY')
    ),
    CONSTRAINT thread_participant_status_allowed CHECK (status IN ('ACTIVE','REMOVED')),
    CONSTRAINT thread_participant_half_open CHECK (membership_to IS NULL OR membership_from < membership_to)
);

-- ---------------------------------------------------------------------------
-- Document, template, signature and artifact-reference metadata
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS document_template (
    tenant_id         tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    template_id       uuid           NOT NULL,
    template_key      semantic_key   NOT NULL,
    template_version  cas_version    NOT NULL,
    purpose           semantic_key   NOT NULL,
    source_locale     text           NOT NULL,
    parameter_schema  jsonb          NOT NULL DEFAULT '{}'::jsonb,
    content_digest    content_digest NOT NULL,
    legal_approval    text           NOT NULL DEFAULT 'PENDING',
    accessibility_approval text      NOT NULL DEFAULT 'PENDING',
    effective_from    timestamptz    NOT NULL,
    effective_to      timestamptz,
    publication_state text           NOT NULL DEFAULT 'DRAFT',
    PRIMARY KEY (tenant_id, template_id),
    CONSTRAINT document_template_version_unique UNIQUE (tenant_id, template_key, template_version),
    CONSTRAINT document_template_schema_object CHECK (jsonb_typeof(parameter_schema) = 'object'),
    CONSTRAINT document_template_legal_allowed CHECK (legal_approval IN ('PENDING','APPROVED','REJECTED')),
    CONSTRAINT document_template_accessibility_allowed CHECK (accessibility_approval IN ('PENDING','APPROVED','REJECTED')),
    CONSTRAINT document_template_state_allowed CHECK (publication_state IN ('DRAFT','PUBLISHED','RETIRED')),
    CONSTRAINT document_template_half_open CHECK (effective_to IS NULL OR effective_from < effective_to),
    -- A template only publishes once both approvals have landed.
    CONSTRAINT document_template_published_is_approved CHECK (
        publication_state <> 'PUBLISHED' OR (legal_approval = 'APPROVED' AND accessibility_approval = 'APPROVED')
    )
);

CREATE TABLE IF NOT EXISTS document (
    tenant_id          tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    document_id        uuid         NOT NULL,
    document_type      semantic_key NOT NULL,
    owner_ref          semantic_key NOT NULL,
    subject_ref        semantic_key NOT NULL,
    current_version_id uuid,
    classification     text         NOT NULL,
    compartment        text         NOT NULL DEFAULT '',
    record_declaration_ref text     NOT NULL DEFAULT '',
    lifecycle          text         NOT NULL DEFAULT 'DRAFT',
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id),
    CONSTRAINT document_lifecycle_allowed CHECK (lifecycle IN ('DRAFT','ACTIVE','SUPERSEDED','ARCHIVED','DISPOSED')),
    CONSTRAINT document_classification_present CHECK (length(classification) > 0)
);
CREATE INDEX IF NOT EXISTS document_subject ON document (tenant_id, subject_ref);

CREATE TABLE IF NOT EXISTS document_version (
    tenant_id                tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    version_id               uuid           NOT NULL,
    document_id              uuid           NOT NULL,
    version_number           cas_version    NOT NULL,
    template_id              uuid,
    template_version         cas_version,
    render_version           cas_version    NOT NULL,
    canonicalization_version cas_version    NOT NULL,
    source_artifact_digest   content_digest NOT NULL,
    rendered_artifact_digest content_digest NOT NULL,
    locale                   text           NOT NULL,
    jurisdiction_ref         text           NOT NULL DEFAULT '',
    classification           text           NOT NULL,
    supersedes_version_id    uuid,
    created_at               timestamptz    NOT NULL,
    sealed_at                timestamptz,
    status                   text           NOT NULL DEFAULT 'DRAFT',
    PRIMARY KEY (tenant_id, version_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document (tenant_id, document_id),
    FOREIGN KEY (tenant_id, template_id) REFERENCES document_template (tenant_id, template_id),
    FOREIGN KEY (tenant_id, supersedes_version_id) REFERENCES document_version (tenant_id, version_id),
    CONSTRAINT document_version_number_unique UNIQUE (tenant_id, document_id, version_number),
    -- A signature binds the exact rendered bytes: signature's foreign key
    -- below points at this key, so a signature can never name a version
    -- while claiming a different digest.
    CONSTRAINT document_version_rendered_identity UNIQUE (tenant_id, version_id, rendered_artifact_digest),
    CONSTRAINT document_version_status_allowed CHECK (status IN ('DRAFT','SEALED','SUPERSEDED','VOID')),
    CONSTRAINT document_version_sealed_has_time CHECK (status <> 'SEALED' OR sealed_at IS NOT NULL),
    CONSTRAINT document_version_sealed_after_created CHECK (sealed_at IS NULL OR sealed_at >= created_at),
    CONSTRAINT document_version_template_pair CHECK (
        (template_id IS NULL AND template_version IS NULL) OR (template_id IS NOT NULL AND template_version IS NOT NULL)
    ),
    CONSTRAINT document_version_not_self_superseding CHECK (
        supersedes_version_id IS NULL OR supersedes_version_id <> version_id
    )
);

ALTER TABLE document
    ADD CONSTRAINT document_current_version_fk
    FOREIGN KEY (tenant_id, current_version_id) REFERENCES document_version (tenant_id, version_id);

CREATE TABLE IF NOT EXISTS signature_request (
    tenant_id           tenant_ref   NOT NULL REFERENCES tenant (tenant_id),
    request_id          uuid         NOT NULL,
    version_id          uuid         NOT NULL,
    assurance_mode      text         NOT NULL,
    signer_requirements jsonb        NOT NULL DEFAULT '{}'::jsonb,
    provider            text         NOT NULL DEFAULT '',
    deadline_at         timestamptz  NOT NULL,
    created_at          timestamptz  NOT NULL,
    status              text         NOT NULL DEFAULT 'OPEN',
    PRIMARY KEY (tenant_id, request_id),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version (tenant_id, version_id),
    CONSTRAINT signature_request_mode_allowed CHECK (
        assurance_mode IN ('NATIVE_EVIDENCE','EXTERNAL_PROVIDER','MANUAL_GATE')
    ),
    CONSTRAINT signature_request_status_allowed CHECK (status IN ('OPEN','COMPLETED','DECLINED','EXPIRED','WITHDRAWN')),
    CONSTRAINT signature_request_requirements_object CHECK (jsonb_typeof(signer_requirements) = 'object'),
    CONSTRAINT signature_request_deadline_after_created CHECK (deadline_at > created_at),
    -- An external-provider ceremony always names its provider.
    CONSTRAINT signature_request_external_names_provider CHECK (
        assurance_mode <> 'EXTERNAL_PROVIDER' OR length(provider) > 0
    )
);

CREATE TABLE IF NOT EXISTS signature (
    tenant_id             tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    signature_id          uuid           NOT NULL,
    request_id            uuid           NOT NULL,
    version_id            uuid           NOT NULL,
    signer_ref            semantic_key   NOT NULL,
    signed_digest         content_digest NOT NULL,
    signature_value_ref   text           NOT NULL,
    certificate_ref       text           NOT NULL DEFAULT '',
    identity_assurance    text           NOT NULL,
    signed_at             timestamptz    NOT NULL,
    validity_state        text           NOT NULL DEFAULT 'VALID',
    PRIMARY KEY (tenant_id, signature_id),
    FOREIGN KEY (tenant_id, request_id) REFERENCES signature_request (tenant_id, request_id),
    -- Binds the signature to the exact rendered bytes it covers.
    FOREIGN KEY (tenant_id, version_id, signed_digest)
        REFERENCES document_version (tenant_id, version_id, rendered_artifact_digest),
    CONSTRAINT signature_signer_unique UNIQUE (tenant_id, request_id, signer_ref),
    CONSTRAINT signature_assurance_allowed CHECK (identity_assurance IN ('IAL1','IAL2','IAL3')),
    CONSTRAINT signature_validity_allowed CHECK (validity_state IN ('VALID','REVOKED','DISPUTED','EXPIRED')),
    -- The signature bytes live in the governed store; the row holds a
    -- reference to them, never the value.
    CONSTRAINT signature_value_is_reference CHECK (
        signature_value_ref ~ '^(artifact|kms|provider)://[^[:space:]]+$'
    )
);

-- document_artifact_reference is the governed pointer from document metadata
-- to content-addressed bytes in the object store 00010 owns. It never holds
-- bytes (DB-014 REFACTOR).
CREATE TABLE IF NOT EXISTS document_artifact_reference (
    tenant_id       tenant_ref     NOT NULL REFERENCES tenant (tenant_id),
    reference_id    uuid           NOT NULL,
    document_id     uuid           NOT NULL,
    version_id      uuid,
    content_id      content_digest NOT NULL,
    digest_algorithm digest_algorithm NOT NULL DEFAULT 'sha256',
    purpose         text           NOT NULL,
    classification  text           NOT NULL,
    retention_class text           NOT NULL,
    hold_state      text           NOT NULL DEFAULT 'NONE',
    referenced_at   timestamptz    NOT NULL,
    PRIMARY KEY (tenant_id, reference_id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES document (tenant_id, document_id),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_version (tenant_id, version_id),
    CONSTRAINT document_artifact_reference_unique UNIQUE (tenant_id, document_id, content_id, purpose),
    CONSTRAINT document_artifact_reference_purpose_allowed CHECK (
        purpose IN ('SOURCE','RENDERED','ATTACHMENT','REDACTION','EVIDENCE','THUMBNAIL')
    ),
    CONSTRAINT document_artifact_reference_retention_allowed CHECK (
        retention_class IN ('PERMANENT','OPERATIONAL','REBUILDABLE')
    ),
    CONSTRAINT document_artifact_reference_hold_allowed CHECK (hold_state IN ('NONE','HELD','RELEASED'))
);

-- ---------------------------------------------------------------------------
-- Append-only enforcement
-- ---------------------------------------------------------------------------

CREATE TRIGGER integration_receipt_append_only BEFORE UPDATE OR DELETE ON integration_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER integration_receipt_item_append_only BEFORE UPDATE OR DELETE ON integration_receipt_item
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER mapping_execution_append_only BEFORE UPDATE OR DELETE ON mapping_execution
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER connector_operation_attempt_append_only BEFORE UPDATE OR DELETE ON connector_operation_attempt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER external_operation_observation_append_only BEFORE UPDATE OR DELETE ON external_operation_observation
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER reconciliation_result_append_only BEFORE UPDATE OR DELETE ON reconciliation_result
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER delivery_attempt_append_only BEFORE UPDATE OR DELETE ON delivery_attempt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER delivery_receipt_append_only BEFORE UPDATE OR DELETE ON delivery_receipt
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER document_version_append_only BEFORE UPDATE OR DELETE ON document_version
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER signature_append_only BEFORE UPDATE OR DELETE ON signature
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER document_artifact_reference_append_only BEFORE UPDATE OR DELETE ON document_artifact_reference
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();

-- ---------------------------------------------------------------------------
-- Row level security
-- ---------------------------------------------------------------------------

ALTER TABLE external_system ENABLE ROW LEVEL SECURITY;
ALTER TABLE external_system FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON external_system USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE connector_definition ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_definition FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_definition USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE connector_connection ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_connection FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_connection USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE mapping_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE mapping_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mapping_profile USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE integration_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_receipt USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE integration_receipt_item ENABLE ROW LEVEL SECURITY;
ALTER TABLE integration_receipt_item FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON integration_receipt_item USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE mapping_execution ENABLE ROW LEVEL SECURITY;
ALTER TABLE mapping_execution FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mapping_execution USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE connector_operation ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE connector_operation_attempt ENABLE ROW LEVEL SECURITY;
ALTER TABLE connector_operation_attempt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON connector_operation_attempt USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE external_operation_observation ENABLE ROW LEVEL SECURITY;
ALTER TABLE external_operation_observation FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON external_operation_observation USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE reconciliation_job ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_job FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reconciliation_job USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE reconciliation_result ENABLE ROW LEVEL SECURITY;
ALTER TABLE reconciliation_result FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reconciliation_result USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE message_intent ENABLE ROW LEVEL SECURITY;
ALTER TABLE message_intent FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON message_intent USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE delivery_endpoint ENABLE ROW LEVEL SECURITY;
ALTER TABLE delivery_endpoint FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON delivery_endpoint USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE recipient_message ENABLE ROW LEVEL SECURITY;
ALTER TABLE recipient_message FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON recipient_message USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE delivery_attempt ENABLE ROW LEVEL SECURITY;
ALTER TABLE delivery_attempt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON delivery_attempt USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE delivery_receipt ENABLE ROW LEVEL SECURITY;
ALTER TABLE delivery_receipt FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON delivery_receipt USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE conversation_thread ENABLE ROW LEVEL SECURITY;
ALTER TABLE conversation_thread FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON conversation_thread USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE thread_participant ENABLE ROW LEVEL SECURITY;
ALTER TABLE thread_participant FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON thread_participant USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE document_template ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_template FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON document_template USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE document ENABLE ROW LEVEL SECURITY;
ALTER TABLE document FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON document USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE document_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_version FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON document_version USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE signature_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE signature_request FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON signature_request USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE signature ENABLE ROW LEVEL SECURITY;
ALTER TABLE signature FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON signature USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
ALTER TABLE document_artifact_reference ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_artifact_reference FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON document_artifact_reference USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- ---------------------------------------------------------------------------
-- Privileges
-- ---------------------------------------------------------------------------

REVOKE UPDATE, DELETE ON integration_receipt FROM PUBLIC;
REVOKE UPDATE, DELETE ON integration_receipt_item FROM PUBLIC;
REVOKE UPDATE, DELETE ON mapping_execution FROM PUBLIC;
REVOKE UPDATE, DELETE ON connector_operation_attempt FROM PUBLIC;
REVOKE UPDATE, DELETE ON external_operation_observation FROM PUBLIC;
REVOKE UPDATE, DELETE ON reconciliation_result FROM PUBLIC;
REVOKE UPDATE, DELETE ON delivery_attempt FROM PUBLIC;
REVOKE UPDATE, DELETE ON delivery_receipt FROM PUBLIC;
REVOKE UPDATE, DELETE ON document_version FROM PUBLIC;
REVOKE UPDATE, DELETE ON signature FROM PUBLIC;
REVOKE UPDATE, DELETE ON document_artifact_reference FROM PUBLIC;
REVOKE DELETE ON external_system FROM PUBLIC;
REVOKE DELETE ON connector_definition FROM PUBLIC;
REVOKE DELETE ON connector_connection FROM PUBLIC;
REVOKE DELETE ON mapping_profile FROM PUBLIC;
REVOKE DELETE ON connector_operation FROM PUBLIC;
REVOKE DELETE ON reconciliation_job FROM PUBLIC;
REVOKE DELETE ON message_intent FROM PUBLIC;
REVOKE DELETE ON delivery_endpoint FROM PUBLIC;
REVOKE DELETE ON recipient_message FROM PUBLIC;
REVOKE DELETE ON conversation_thread FROM PUBLIC;
REVOKE DELETE ON thread_participant FROM PUBLIC;
REVOKE DELETE ON document_template FROM PUBLIC;
REVOKE DELETE ON document FROM PUBLIC;
REVOKE DELETE ON signature_request FROM PUBLIC;

GRANT SELECT, INSERT, UPDATE ON external_system TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON connector_definition TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON connector_connection TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON mapping_profile TO hcmnext_app;
GRANT SELECT, INSERT ON integration_receipt TO hcmnext_app;
GRANT SELECT, INSERT ON integration_receipt_item TO hcmnext_app;
GRANT SELECT, INSERT ON mapping_execution TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON connector_operation TO hcmnext_app;
GRANT SELECT, INSERT ON connector_operation_attempt TO hcmnext_app;
GRANT SELECT, INSERT ON external_operation_observation TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON reconciliation_job TO hcmnext_app;
GRANT SELECT, INSERT ON reconciliation_result TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON message_intent TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON delivery_endpoint TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON recipient_message TO hcmnext_app;
GRANT SELECT, INSERT ON delivery_attempt TO hcmnext_app;
GRANT SELECT, INSERT ON delivery_receipt TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON conversation_thread TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON thread_participant TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON document_template TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON document TO hcmnext_app;
GRANT SELECT, INSERT ON document_version TO hcmnext_app;
GRANT SELECT, INSERT, UPDATE ON signature_request TO hcmnext_app;
GRANT SELECT, INSERT ON signature TO hcmnext_app;
GRANT SELECT, INSERT ON document_artifact_reference TO hcmnext_app;

-- +goose Down
REVOKE ALL ON document_artifact_reference FROM hcmnext_app;
REVOKE ALL ON signature FROM hcmnext_app;
REVOKE ALL ON signature_request FROM hcmnext_app;
REVOKE ALL ON document_version FROM hcmnext_app;
REVOKE ALL ON document FROM hcmnext_app;
REVOKE ALL ON document_template FROM hcmnext_app;
REVOKE ALL ON thread_participant FROM hcmnext_app;
REVOKE ALL ON conversation_thread FROM hcmnext_app;
REVOKE ALL ON delivery_receipt FROM hcmnext_app;
REVOKE ALL ON delivery_attempt FROM hcmnext_app;
REVOKE ALL ON recipient_message FROM hcmnext_app;
REVOKE ALL ON delivery_endpoint FROM hcmnext_app;
REVOKE ALL ON message_intent FROM hcmnext_app;
REVOKE ALL ON reconciliation_result FROM hcmnext_app;
REVOKE ALL ON reconciliation_job FROM hcmnext_app;
REVOKE ALL ON external_operation_observation FROM hcmnext_app;
REVOKE ALL ON connector_operation_attempt FROM hcmnext_app;
REVOKE ALL ON connector_operation FROM hcmnext_app;
REVOKE ALL ON mapping_execution FROM hcmnext_app;
REVOKE ALL ON integration_receipt_item FROM hcmnext_app;
REVOKE ALL ON integration_receipt FROM hcmnext_app;
REVOKE ALL ON mapping_profile FROM hcmnext_app;
REVOKE ALL ON connector_connection FROM hcmnext_app;
REVOKE ALL ON connector_definition FROM hcmnext_app;
REVOKE ALL ON external_system FROM hcmnext_app;

DROP TABLE document_artifact_reference;
DROP TABLE signature;
DROP TABLE signature_request;
ALTER TABLE document DROP CONSTRAINT document_current_version_fk;
DROP TABLE document_version;
DROP TABLE document;
DROP TABLE document_template;
DROP TABLE thread_participant;
DROP TABLE conversation_thread;
ALTER TABLE recipient_message DROP CONSTRAINT recipient_message_satisfying_receipt_fk;
DROP TABLE delivery_receipt;
DROP TABLE delivery_attempt;
DROP TABLE recipient_message;
DROP TABLE delivery_endpoint;
DROP TABLE message_intent;
DROP TABLE reconciliation_result;
DROP TABLE reconciliation_job;
DROP TABLE external_operation_observation;
DROP TABLE connector_operation_attempt;
DROP TABLE connector_operation;
DROP TABLE mapping_execution;
DROP TABLE integration_receipt_item;
DROP TABLE integration_receipt;
DROP TABLE mapping_profile;
DROP TABLE connector_connection;
DROP TABLE connector_definition;
DROP TABLE external_system;
