CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS schema_migrations (
  migration_name text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tenants (
  tenant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  status text NOT NULL DEFAULT 'active',
  default_timezone text NOT NULL DEFAULT 'UTC',
  default_locale text NOT NULL DEFAULT 'en-US',
  data_region text,
  settings jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT tenants_status_check
    CHECK (status IN ('active', 'inactive', 'suspended', 'deleted'))
);

CREATE TABLE environments (
  environment_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  type text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT environments_type_check
    CHECK (type IN ('development', 'sandbox', 'staging', 'production')),
  CONSTRAINT environments_status_check
    CHECK (status IN ('active', 'inactive', 'deleted')),
  CONSTRAINT environments_tenant_name_unique
    UNIQUE (tenant_id, name)
);

CREATE INDEX environments_tenant_idx
  ON environments (tenant_id);

CREATE TABLE actors (
  actor_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  actor_type text NOT NULL,
  linked_worker_id text,
  email text,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  roles jsonb NOT NULL DEFAULT '[]'::jsonb,
  groups jsonb NOT NULL DEFAULT '[]'::jsonb,
  auth_provider text,
  external_subject text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT actors_type_check
    CHECK (actor_type IN ('human', 'service_account', 'integration', 'workflow', 'ai_agent', 'system')),
  CONSTRAINT actors_status_check
    CHECK (status IN ('active', 'inactive', 'suspended', 'deleted')),
  CONSTRAINT actors_roles_array_check
    CHECK (jsonb_typeof(roles) = 'array'),
  CONSTRAINT actors_groups_array_check
    CHECK (jsonb_typeof(groups) = 'array')
);

CREATE INDEX actors_tenant_idx
  ON actors (tenant_id);

CREATE INDEX actors_tenant_email_idx
  ON actors (tenant_id, email);

CREATE INDEX actors_linked_worker_idx
  ON actors (tenant_id, linked_worker_id);

CREATE UNIQUE INDEX actors_external_subject_unique_idx
  ON actors (tenant_id, external_subject)
  WHERE external_subject IS NOT NULL;

CREATE TABLE workflow_definitions (
  workflow_definition_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  workflow_type text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  current_version_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT workflow_definitions_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived')),
  CONSTRAINT workflow_definitions_tenant_name_unique
    UNIQUE (tenant_id, name)
);

CREATE TABLE workflow_versions (
  workflow_version_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_definition_id uuid NOT NULL REFERENCES workflow_definitions(workflow_definition_id) ON DELETE CASCADE,
  version_number integer NOT NULL,
  graph_definition jsonb NOT NULL DEFAULT '{}'::jsonb,
  input_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  output_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  validation_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  approval_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  ai_review_scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL DEFAULT 'draft',
  published_by uuid REFERENCES actors(actor_id),
  published_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_versions_status_check
    CHECK (status IN ('draft', 'published', 'retired')),
  CONSTRAINT workflow_versions_unique
    UNIQUE (workflow_definition_id, version_number),
  CONSTRAINT workflow_versions_validation_rules_array_check
    CHECK (jsonb_typeof(validation_rules) = 'array'),
  CONSTRAINT workflow_versions_approval_rules_array_check
    CHECK (jsonb_typeof(approval_rules) = 'array')
);

ALTER TABLE workflow_definitions
  ADD CONSTRAINT workflow_definitions_current_version_fk
  FOREIGN KEY (current_version_id)
  REFERENCES workflow_versions(workflow_version_id);

CREATE INDEX workflow_versions_definition_idx
  ON workflow_versions (tenant_id, workflow_definition_id, version_number DESC);

CREATE TABLE change_requests (
  change_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid REFERENCES environments(environment_id),
  change_type text NOT NULL,
  target_worker_id text NOT NULL,
  requester_actor_id uuid NOT NULL REFERENCES actors(actor_id),
  effective_at timestamptz,
  business_reason text,
  status text NOT NULL DEFAULT 'draft',
  priority text NOT NULL DEFAULT 'normal',
  current_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  proposed_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  preflight_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_review jsonb NOT NULL DEFAULT '{}'::jsonb,
  workflow_definition_id uuid REFERENCES workflow_definitions(workflow_definition_id),
  workflow_version_id uuid REFERENCES workflow_versions(workflow_version_id),
  transaction_plan_id uuid,
  submitted_at timestamptz,
  approved_at timestamptz,
  executed_at timestamptz,
  closed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid REFERENCES actors(actor_id),
  updated_by uuid REFERENCES actors(actor_id),
  version integer NOT NULL DEFAULT 1,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT change_requests_change_type_check
    CHECK (change_type IN (
      'hire',
      'onboarding',
      'job_change',
      'manager_org_change',
      'compensation_change',
      'promotion',
      'leave',
      'employee_data_change',
      'termination',
      'reorg_batch',
      'custom'
    )),
  CONSTRAINT change_requests_status_check
    CHECK (status IN (
      'draft',
      'needs_data',
      'preflighted',
      'submitted',
      'in_approval',
      'approved',
      'rejected',
      'canceled',
      'simulated',
      'executing',
      'executed',
      'reconciling',
      'reconciled',
      'waiting_repair',
      'closed',
      'superseded',
      'failed'
    )),
  CONSTRAINT change_requests_priority_check
    CHECK (priority IN ('low', 'normal', 'high', 'urgent'))
);

CREATE INDEX change_requests_tenant_status_idx
  ON change_requests (tenant_id, status, created_at DESC);

CREATE INDEX change_requests_target_worker_idx
  ON change_requests (tenant_id, target_worker_id, created_at DESC);

CREATE INDEX change_requests_type_idx
  ON change_requests (tenant_id, change_type, created_at DESC);

CREATE TABLE workflow_instances (
  workflow_instance_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid NOT NULL REFERENCES environments(environment_id),
  workflow_definition_id uuid NOT NULL REFERENCES workflow_definitions(workflow_definition_id),
  workflow_version_id uuid NOT NULL REFERENCES workflow_versions(workflow_version_id),
  intent text NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  state text NOT NULL,
  change_request_id uuid REFERENCES change_requests(change_request_id),
  requester_actor_id uuid NOT NULL REFERENCES actors(actor_id),
  current_interaction jsonb NOT NULL DEFAULT '{}'::jsonb,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  started_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  canceled_at timestamptz,
  failed_at timestamptz,
  version integer NOT NULL DEFAULT 1,
  correlation_id text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_instances_status_check
    CHECK (status IN (
      'active',
      'waiting',
      'completed',
      'rejected',
      'canceled',
      'failed',
      'superseded',
      'waiting_repair'
    )),
  CONSTRAINT workflow_instances_state_check
    CHECK (state IN (
      'collecting_input',
      'collecting_evidence',
      'waiting_approval',
      'approved',
      'executing',
      'executed',
      'rejected',
      'canceled',
      'failed',
      'waiting_repair'
    ))
);

CREATE INDEX workflow_instances_subject_idx
  ON workflow_instances (tenant_id, subject_type, subject_id, created_at DESC);

CREATE INDEX workflow_instances_status_idx
  ON workflow_instances (tenant_id, status, updated_at DESC);

CREATE INDEX workflow_instances_change_request_idx
  ON workflow_instances (tenant_id, change_request_id);

CREATE TABLE workflow_transition_attempts (
  workflow_transition_attempt_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id uuid NOT NULL REFERENCES workflow_instances(workflow_instance_id) ON DELETE CASCADE,
  transition text NOT NULL,
  idempotency_key text NOT NULL,
  expected_version integer NOT NULL,
  actor_id uuid NOT NULL REFERENCES actors(actor_id),
  request_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_payload jsonb,
  status text NOT NULL DEFAULT 'started',
  error jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,

  CONSTRAINT workflow_transition_attempts_status_check
    CHECK (status IN ('started', 'completed', 'failed')),
  CONSTRAINT workflow_transition_attempts_request_object_check
    CHECK (jsonb_typeof(request_payload) = 'object')
);

CREATE UNIQUE INDEX workflow_transition_attempts_idempotency_unique_idx
  ON workflow_transition_attempts (tenant_id, workflow_instance_id, idempotency_key);

CREATE TABLE ledger_events (
  event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid REFERENCES environments(environment_id),
  event_type text NOT NULL,
  event_version integer NOT NULL DEFAULT 1,
  event_sequence bigint GENERATED ALWAYS AS IDENTITY,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  effective_at timestamptz,
  recorded_at timestamptz NOT NULL DEFAULT now(),
  actor_type text NOT NULL,
  actor_id uuid REFERENCES actors(actor_id),
  actor_role text,
  relationship_context jsonb NOT NULL DEFAULT '{}'::jsonb,
  workflow_instance_id uuid REFERENCES workflow_instances(workflow_instance_id),
  workflow_definition_id uuid REFERENCES workflow_definitions(workflow_definition_id),
  workflow_version_id uuid REFERENCES workflow_versions(workflow_version_id),
  change_request_id uuid REFERENCES change_requests(change_request_id),
  transaction_plan_id uuid,
  approval_task_id uuid,
  block_name text,
  block_version text,
  correlation_id text NOT NULL,
  causation_id uuid,
  idempotency_key text,
  permission_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_visibility_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  previous_hash text,
  event_hash text,

  CONSTRAINT ledger_events_actor_type_check
    CHECK (actor_type IN ('human', 'service_account', 'integration', 'workflow', 'ai_agent', 'system')),
  CONSTRAINT ledger_events_payload_object_check
    CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX ledger_events_tenant_sequence_idx
  ON ledger_events (tenant_id, event_sequence);

CREATE INDEX ledger_events_subject_idx
  ON ledger_events (tenant_id, subject_type, subject_id, event_sequence);

CREATE INDEX ledger_events_workflow_instance_idx
  ON ledger_events (tenant_id, workflow_instance_id, event_sequence);

CREATE INDEX ledger_events_change_request_idx
  ON ledger_events (tenant_id, change_request_id, event_sequence);

CREATE INDEX ledger_events_correlation_idx
  ON ledger_events (tenant_id, correlation_id);

CREATE UNIQUE INDEX ledger_events_idempotency_unique_idx
  ON ledger_events (tenant_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

CREATE FUNCTION prevent_ledger_events_mutation()
RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'ledger_events is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_events_prevent_update
  BEFORE UPDATE ON ledger_events
  FOR EACH ROW
  EXECUTE FUNCTION prevent_ledger_events_mutation();

CREATE TRIGGER ledger_events_prevent_delete
  BEFORE DELETE ON ledger_events
  FOR EACH ROW
  EXECUTE FUNCTION prevent_ledger_events_mutation();

CREATE TABLE proposed_changes (
  proposed_change_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,
  target_object_type text NOT NULL,
  target_object_id text NOT NULL,
  field_path text NOT NULL,
  current_value jsonb,
  proposed_value jsonb NOT NULL,
  effective_at timestamptz,
  reason_code text,
  validation_status text NOT NULL DEFAULT 'pending',
  risk_level text NOT NULL DEFAULT 'unknown',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT proposed_changes_validation_status_check
    CHECK (validation_status IN ('pending', 'valid', 'invalid', 'warning', 'skipped')),
  CONSTRAINT proposed_changes_risk_level_check
    CHECK (risk_level IN ('unknown', 'low', 'medium', 'high', 'critical'))
);

CREATE INDEX proposed_changes_request_idx
  ON proposed_changes (tenant_id, change_request_id);

CREATE INDEX proposed_changes_target_idx
  ON proposed_changes (tenant_id, target_object_type, target_object_id);

CREATE INDEX proposed_changes_field_path_idx
  ON proposed_changes (tenant_id, field_path);

CREATE TABLE documents (
  document_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid REFERENCES environments(environment_id),
  workflow_instance_id uuid REFERENCES workflow_instances(workflow_instance_id),
  created_by_actor_id uuid NOT NULL REFERENCES actors(actor_id),
  purpose text NOT NULL,
  filename text NOT NULL,
  content_type text NOT NULL,
  classification text NOT NULL,
  status text NOT NULL DEFAULT 'pending_upload',
  storage_key text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT documents_status_check
    CHECK (status IN ('pending_upload', 'uploaded', 'rejected', 'deleted')),
  CONSTRAINT documents_classification_check
    CHECK (classification IN ('public', 'internal', 'confidential', 'sensitive_person_identity'))
);

CREATE INDEX documents_workflow_instance_idx
  ON documents (tenant_id, workflow_instance_id, created_at DESC);

CREATE INDEX documents_created_by_idx
  ON documents (tenant_id, created_by_actor_id, created_at DESC);

CREATE TABLE workflow_instance_documents (
  workflow_instance_document_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id uuid NOT NULL REFERENCES workflow_instances(workflow_instance_id) ON DELETE CASCADE,
  document_id uuid NOT NULL REFERENCES documents(document_id),
  change_request_id uuid REFERENCES change_requests(change_request_id),
  purpose text NOT NULL,
  attached_by_actor_id uuid NOT NULL REFERENCES actors(actor_id),
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_instance_documents_unique
    UNIQUE (tenant_id, workflow_instance_id, document_id)
);

CREATE INDEX workflow_instance_documents_request_idx
  ON workflow_instance_documents (tenant_id, change_request_id);

CREATE TABLE approval_tasks (
  approval_task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,
  workflow_instance_id uuid REFERENCES workflow_instances(workflow_instance_id),
  assignee_actor_id uuid REFERENCES actors(actor_id),
  assignee_role text,
  assignee_relationship text,
  approval_type text NOT NULL DEFAULT 'standard',
  status text NOT NULL DEFAULT 'pending',
  decision text,
  decision_reason text,
  comments text,
  due_at timestamptz,
  delegated_to_actor_id uuid REFERENCES actors(actor_id),
  created_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT approval_tasks_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'delegated', 'expired', 'canceled', 'skipped')),
  CONSTRAINT approval_tasks_decision_check
    CHECK (decision IS NULL OR decision IN ('approved', 'rejected', 'delegated', 'skipped'))
);

CREATE INDEX approval_tasks_request_idx
  ON approval_tasks (tenant_id, change_request_id);

CREATE INDEX approval_tasks_assignee_idx
  ON approval_tasks (tenant_id, assignee_actor_id, status, due_at);

CREATE INDEX approval_tasks_role_status_idx
  ON approval_tasks (tenant_id, assignee_role, status, due_at);

CREATE TABLE transaction_plans (
  transaction_plan_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'draft',
  plan_version integer NOT NULL DEFAULT 1,
  steps jsonb NOT NULL DEFAULT '[]'::jsonb,
  internal_writes jsonb NOT NULL DEFAULT '[]'::jsonb,
  projection_patches jsonb NOT NULL DEFAULT '[]'::jsonb,
  external_writes jsonb NOT NULL DEFAULT '[]'::jsonb,
  rollback_plan jsonb NOT NULL DEFAULT '{}'::jsonb,
  compensation_plan jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_keys jsonb NOT NULL DEFAULT '{}'::jsonb,
  simulation_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  execution_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  reconciliation_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by uuid REFERENCES actors(actor_id),
  updated_by uuid REFERENCES actors(actor_id),

  CONSTRAINT transaction_plans_status_check
    CHECK (status IN ('draft', 'simulated', 'ready', 'executing', 'executed', 'reconciling', 'reconciled', 'failed', 'canceled')),
  CONSTRAINT transaction_plans_steps_array_check
    CHECK (jsonb_typeof(steps) = 'array'),
  CONSTRAINT transaction_plans_internal_writes_array_check
    CHECK (jsonb_typeof(internal_writes) = 'array'),
  CONSTRAINT transaction_plans_projection_patches_array_check
    CHECK (jsonb_typeof(projection_patches) = 'array'),
  CONSTRAINT transaction_plans_external_writes_array_check
    CHECK (jsonb_typeof(external_writes) = 'array')
);

ALTER TABLE change_requests
  ADD CONSTRAINT change_requests_transaction_plan_fk
  FOREIGN KEY (transaction_plan_id)
  REFERENCES transaction_plans(transaction_plan_id);

ALTER TABLE ledger_events
  ADD CONSTRAINT ledger_events_transaction_plan_fk
  FOREIGN KEY (transaction_plan_id)
  REFERENCES transaction_plans(transaction_plan_id);

ALTER TABLE ledger_events
  ADD CONSTRAINT ledger_events_approval_task_fk
  FOREIGN KEY (approval_task_id)
  REFERENCES approval_tasks(approval_task_id);

CREATE INDEX transaction_plans_request_idx
  ON transaction_plans (tenant_id, change_request_id);

CREATE INDEX transaction_plans_status_idx
  ON transaction_plans (tenant_id, status, created_at DESC);

CREATE TABLE employee_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  employee_id text NOT NULL,
  projection_version integer NOT NULL DEFAULT 1,
  as_of_effective_at timestamptz,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,
  indexed_fields jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, employee_id)
);

CREATE INDEX employee_projection_updated_idx
  ON employee_projection (tenant_id, updated_at DESC);

CREATE INDEX employee_projection_document_gin_idx
  ON employee_projection USING gin (document);

CREATE INDEX employee_projection_indexed_fields_gin_idx
  ON employee_projection USING gin (indexed_fields);

CREATE TABLE change_request_projection (
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  change_request_id uuid NOT NULL REFERENCES change_requests(change_request_id) ON DELETE CASCADE,
  projection_version integer NOT NULL DEFAULT 1,
  source_event_id uuid REFERENCES ledger_events(event_id),
  source_event_sequence bigint,
  status text NOT NULL,
  target_worker_id text NOT NULL,
  change_type text NOT NULL,
  effective_at timestamptz,
  summary jsonb NOT NULL DEFAULT '{}'::jsonb,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  PRIMARY KEY (tenant_id, change_request_id)
);

CREATE INDEX change_request_projection_status_idx
  ON change_request_projection (tenant_id, status, updated_at DESC);

CREATE INDEX change_request_projection_worker_idx
  ON change_request_projection (tenant_id, target_worker_id, updated_at DESC);

CREATE TABLE integration_connections (
  integration_connection_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  system_type text NOT NULL,
  auth_type text NOT NULL,
  secret_ref text,
  base_url text,
  status text NOT NULL DEFAULT 'draft',
  capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
  rate_limits jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_health_check_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT integration_connections_auth_type_check
    CHECK (auth_type IN ('none', 'api_key', 'oauth2', 'basic', 'saml', 'custom')),
  CONSTRAINT integration_connections_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'failed', 'deleted'))
);

CREATE INDEX integration_connections_tenant_type_idx
  ON integration_connections (tenant_id, system_type, status);

CREATE TABLE integration_outbox (
  outbox_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  integration_connection_id uuid REFERENCES integration_connections(integration_connection_id),
  change_request_id uuid REFERENCES change_requests(change_request_id),
  transaction_plan_id uuid REFERENCES transaction_plans(transaction_plan_id),
  ledger_event_id uuid REFERENCES ledger_events(event_id),
  destination text NOT NULL,
  operation text NOT NULL,
  request_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_payload jsonb,
  status text NOT NULL DEFAULT 'pending',
  attempt_count integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 5,
  next_attempt_at timestamptz,
  last_attempt_at timestamptz,
  error_code text,
  error_message text,
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT integration_outbox_status_check
    CHECK (status IN ('pending', 'processing', 'succeeded', 'failed', 'dead_letter', 'canceled')),
  CONSTRAINT integration_outbox_attempts_check
    CHECK (attempt_count >= 0 AND max_attempts > 0),
  CONSTRAINT integration_outbox_idempotency_unique
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX integration_outbox_pending_idx
  ON integration_outbox (status, next_attempt_at, created_at)
  WHERE status IN ('pending', 'failed');

CREATE INDEX integration_outbox_request_idx
  ON integration_outbox (tenant_id, change_request_id);

CREATE TABLE permission_policies (
  permission_policy_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  subject_scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  resource_scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  actions jsonb NOT NULL DEFAULT '[]'::jsonb,
  field_permissions jsonb NOT NULL DEFAULT '{}'::jsonb,
  relationship_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
  attribute_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_visibility_rules jsonb NOT NULL DEFAULT '{}'::jsonb,
  effect text NOT NULL DEFAULT 'allow',
  priority integer NOT NULL DEFAULT 100,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT permission_policies_effect_check
    CHECK (effect IN ('allow', 'deny', 'require_approval', 'mask', 'redact')),
  CONSTRAINT permission_policies_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived')),
  CONSTRAINT permission_policies_actions_array_check
    CHECK (jsonb_typeof(actions) = 'array')
);

CREATE INDEX permission_policies_tenant_status_idx
  ON permission_policies (tenant_id, status, priority);

CREATE TABLE metadata_field_definitions (
  metadata_field_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  object_type text NOT NULL,
  field_key text NOT NULL,
  label text NOT NULL,
  data_type text NOT NULL,
  required boolean NOT NULL DEFAULT false,
  sensitive boolean NOT NULL DEFAULT false,
  searchable boolean NOT NULL DEFAULT false,
  effective_dated boolean NOT NULL DEFAULT false,
  validation jsonb NOT NULL DEFAULT '{}'::jsonb,
  default_value jsonb,
  permission_tags jsonb NOT NULL DEFAULT '[]'::jsonb,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT metadata_field_definitions_data_type_check
    CHECK (data_type IN ('string', 'number', 'boolean', 'date', 'datetime', 'enum', 'object', 'array', 'reference', 'money')),
  CONSTRAINT metadata_field_definitions_status_check
    CHECK (status IN ('draft', 'active', 'inactive', 'archived')),
  CONSTRAINT metadata_field_definitions_permission_tags_array_check
    CHECK (jsonb_typeof(permission_tags) = 'array'),
  CONSTRAINT metadata_field_definitions_unique
    UNIQUE (tenant_id, object_type, field_key)
);

CREATE INDEX metadata_field_definitions_object_idx
  ON metadata_field_definitions (tenant_id, object_type, status);
