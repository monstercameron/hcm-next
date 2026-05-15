CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
  tenant_id text PRIMARY KEY,
  name text NOT NULL,
  slug text NOT NULL UNIQUE,
  status text NOT NULL
);

CREATE TABLE environments (
  environment_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  type text NOT NULL,
  status text NOT NULL
);

CREATE TABLE actors (
  actor_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  actor_type text NOT NULL,
  linked_worker_id text,
  email text,
  display_name text NOT NULL,
  status text NOT NULL,
  roles jsonb NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE workflow_definitions (
  workflow_definition_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  name text NOT NULL,
  workflow_type text NOT NULL,
  status text NOT NULL,
  current_version_id text NOT NULL
);

CREATE TABLE workflow_versions (
  workflow_version_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  workflow_definition_id text NOT NULL REFERENCES workflow_definitions(workflow_definition_id),
  version_number integer NOT NULL,
  graph_definition jsonb NOT NULL DEFAULT '{}'::jsonb,
  input_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  output_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  validation_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  approval_rules jsonb NOT NULL DEFAULT '[]'::jsonb,
  ai_review_scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  status text NOT NULL
);

CREATE TABLE workflow_instances (
  workflow_instance_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  environment_id text NOT NULL REFERENCES environments(environment_id),
  workflow_definition_id text NOT NULL REFERENCES workflow_definitions(workflow_definition_id),
  workflow_version_id text NOT NULL REFERENCES workflow_versions(workflow_version_id),
  intent text NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  status text NOT NULL,
  state text NOT NULL,
  change_request_id text,
  requester_actor_id text NOT NULL REFERENCES actors(actor_id),
  current_interaction jsonb NOT NULL DEFAULT '{}'::jsonb,
  context jsonb NOT NULL DEFAULT '{}'::jsonb,
  started_at timestamptz NOT NULL,
  completed_at timestamptz,
  canceled_at timestamptz,
  failed_at timestamptz,
  version integer NOT NULL,
  correlation_id text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE workflow_transition_attempts (
  workflow_transition_attempt_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id text NOT NULL REFERENCES workflow_instances(workflow_instance_id),
  transition text NOT NULL,
  idempotency_key text NOT NULL,
  expected_version integer NOT NULL,
  actor_id text NOT NULL REFERENCES actors(actor_id),
  request_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_payload jsonb,
  status text NOT NULL,
  error jsonb,
  created_at timestamptz NOT NULL,
  completed_at timestamptz,
  UNIQUE (tenant_id, workflow_instance_id, idempotency_key)
);

CREATE TABLE ledger_events (
  event_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  event_type text NOT NULL,
  event_version integer NOT NULL,
  event_sequence bigint NOT NULL,
  subject_type text NOT NULL,
  subject_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  effective_at timestamptz,
  recorded_at timestamptz NOT NULL,
  actor_type text NOT NULL,
  actor_id text NOT NULL REFERENCES actors(actor_id),
  actor_role text,
  relationship_context jsonb NOT NULL DEFAULT '{}'::jsonb,
  workflow_instance_id text,
  workflow_definition_id text,
  workflow_version_id text,
  change_request_id text,
  transaction_plan_id text,
  approval_task_id text,
  correlation_id text NOT NULL,
  causation_id text,
  idempotency_key text,
  permission_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_visibility_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE change_requests (
  change_request_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  environment_id text NOT NULL REFERENCES environments(environment_id),
  change_type text NOT NULL,
  target_worker_id text NOT NULL,
  requester_actor_id text NOT NULL REFERENCES actors(actor_id),
  effective_at timestamptz NOT NULL,
  business_reason text NOT NULL,
  status text NOT NULL,
  priority text NOT NULL,
  current_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  proposed_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
  preflight_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  ai_review jsonb NOT NULL DEFAULT '{}'::jsonb,
  workflow_definition_id text NOT NULL,
  workflow_version_id text NOT NULL,
  transaction_plan_id text,
  submitted_at timestamptz,
  approved_at timestamptz,
  executed_at timestamptz,
  closed_at timestamptz,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  created_by text NOT NULL REFERENCES actors(actor_id),
  updated_by text NOT NULL REFERENCES actors(actor_id),
  version integer NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE proposed_changes (
  proposed_change_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  change_request_id text NOT NULL REFERENCES change_requests(change_request_id),
  target_object_type text NOT NULL,
  target_object_id text NOT NULL,
  field_path text NOT NULL,
  current_value jsonb,
  proposed_value jsonb NOT NULL,
  effective_at timestamptz NOT NULL,
  reason_code text NOT NULL,
  validation_status text NOT NULL,
  risk_level text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE documents (
  document_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  purpose text NOT NULL,
  filename text NOT NULL,
  content_type text NOT NULL,
  classification text NOT NULL,
  workflow_instance_id text NOT NULL REFERENCES workflow_instances(workflow_instance_id),
  owner_actor_id text NOT NULL REFERENCES actors(actor_id),
  status text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);

CREATE TABLE workflow_instance_documents (
  workflow_instance_document_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id text NOT NULL REFERENCES workflow_instances(workflow_instance_id),
  document_id text NOT NULL REFERENCES documents(document_id),
  attached_by_actor_id text NOT NULL REFERENCES actors(actor_id),
  created_at timestamptz NOT NULL
);

CREATE TABLE approval_tasks (
  approval_task_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  change_request_id text NOT NULL REFERENCES change_requests(change_request_id),
  workflow_instance_id text NOT NULL REFERENCES workflow_instances(workflow_instance_id),
  assignee_actor_id text NOT NULL REFERENCES actors(actor_id),
  assignee_role text NOT NULL,
  assignee_relationship text,
  approval_type text NOT NULL,
  status text NOT NULL,
  decision text,
  decision_reason text,
  comments text,
  due_at timestamptz,
  delegated_to_actor_id text,
  created_at timestamptz NOT NULL,
  decided_at timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE transaction_plans (
  transaction_plan_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  change_request_id text NOT NULL REFERENCES change_requests(change_request_id),
  status text NOT NULL,
  plan_version integer NOT NULL,
  steps jsonb NOT NULL DEFAULT '[]'::jsonb,
  internal_writes jsonb NOT NULL DEFAULT '[]'::jsonb,
  external_writes jsonb NOT NULL DEFAULT '[]'::jsonb,
  rollback_plan jsonb NOT NULL DEFAULT '{}'::jsonb,
  compensation_plan jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_keys jsonb NOT NULL DEFAULT '{}'::jsonb,
  simulation_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  execution_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  reconciliation_result jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  created_by text NOT NULL,
  updated_by text NOT NULL
);

CREATE TABLE employee_projection (
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  employee_id text NOT NULL,
  projection_version integer NOT NULL,
  source_event_id text,
  source_event_sequence bigint,
  document jsonb NOT NULL DEFAULT '{}'::jsonb,
  indexed_fields jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, employee_id)
);

CREATE TABLE integration_outbox (
  outbox_id text PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES tenants(tenant_id),
  change_request_id text,
  transaction_plan_id text,
  ledger_event_id text,
  destination text NOT NULL,
  operation text NOT NULL,
  request_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  response_payload jsonb,
  status text NOT NULL,
  attempt_count integer NOT NULL,
  max_attempts integer NOT NULL,
  idempotency_key text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  UNIQUE (tenant_id, idempotency_key)
);

