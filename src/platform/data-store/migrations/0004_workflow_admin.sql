CREATE TABLE IF NOT EXISTS workflow_families (
  workflow_family_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid NOT NULL REFERENCES environments(environment_id),
  intent text NOT NULL,
  name text NOT NULL,
  description text,
  hcm_domain text NOT NULL,
  owner_actor_id uuid REFERENCES actors(actor_id),
  status text NOT NULL DEFAULT 'active',
  active_workflow_version_record_id uuid,
  version integer NOT NULL DEFAULT 1,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_families_status_check
    CHECK (status IN ('active', 'archived')),
  CONSTRAINT workflow_families_tenant_environment_intent_unique
    UNIQUE (tenant_id, environment_id, intent),
  CONSTRAINT workflow_families_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS workflow_families_registry_idx
  ON workflow_families (tenant_id, environment_id, status, intent);

CREATE TABLE IF NOT EXISTS workflow_drafts (
  workflow_draft_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_family_id uuid NOT NULL REFERENCES workflow_families(workflow_family_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid NOT NULL REFERENCES environments(environment_id),
  draft_version integer NOT NULL,
  config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  config_checksum text NOT NULL,
  status text NOT NULL DEFAULT 'draft',
  created_by_actor_id uuid REFERENCES actors(actor_id),
  updated_by_actor_id uuid REFERENCES actors(actor_id),
  source_template_id uuid,
  source_workflow_version_record_id uuid,
  source_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  override_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  version integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_drafts_status_check
    CHECK (status IN ('draft', 'ready_for_review', 'rejected', 'published')),
  CONSTRAINT workflow_drafts_config_object_check
    CHECK (jsonb_typeof(config_json) = 'object'),
  CONSTRAINT workflow_drafts_source_metadata_object_check
    CHECK (jsonb_typeof(source_metadata) = 'object'),
  CONSTRAINT workflow_drafts_override_metadata_object_check
    CHECK (jsonb_typeof(override_metadata) = 'object'),
  CONSTRAINT workflow_drafts_family_version_unique
    UNIQUE (workflow_family_id, draft_version)
);

CREATE INDEX IF NOT EXISTS workflow_drafts_family_status_idx
  ON workflow_drafts (tenant_id, workflow_family_id, status, draft_version DESC);

CREATE TABLE IF NOT EXISTS workflow_version_records (
  workflow_version_record_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_family_id uuid NOT NULL REFERENCES workflow_families(workflow_family_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid NOT NULL REFERENCES environments(environment_id),
  published_version integer NOT NULL,
  config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  config_checksum text NOT NULL,
  status text NOT NULL DEFAULT 'published',
  is_active boolean NOT NULL DEFAULT false,
  published_by_actor_id uuid REFERENCES actors(actor_id),
  published_at timestamptz NOT NULL DEFAULT now(),
  source_workflow_draft_id uuid REFERENCES workflow_drafts(workflow_draft_id),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_version_records_status_check
    CHECK (status IN ('published', 'inactive')),
  CONSTRAINT workflow_version_records_config_object_check
    CHECK (jsonb_typeof(config_json) = 'object'),
  CONSTRAINT workflow_version_records_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT workflow_version_records_family_version_unique
    UNIQUE (workflow_family_id, published_version)
);

CREATE UNIQUE INDEX IF NOT EXISTS workflow_version_records_one_active_idx
  ON workflow_version_records (workflow_family_id)
  WHERE is_active;

CREATE INDEX IF NOT EXISTS workflow_version_records_family_idx
  ON workflow_version_records (tenant_id, workflow_family_id, published_version DESC);

ALTER TABLE workflow_families
  ADD CONSTRAINT workflow_families_active_version_fk
  FOREIGN KEY (active_workflow_version_record_id)
  REFERENCES workflow_version_records(workflow_version_record_id);

CREATE TABLE IF NOT EXISTS workflow_publish_history (
  workflow_publish_history_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workflow_family_id uuid NOT NULL REFERENCES workflow_families(workflow_family_id) ON DELETE CASCADE,
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid NOT NULL REFERENCES environments(environment_id),
  action text NOT NULL,
  source_workflow_draft_id uuid REFERENCES workflow_drafts(workflow_draft_id),
  published_workflow_version_record_id uuid NOT NULL REFERENCES workflow_version_records(workflow_version_record_id),
  previous_active_workflow_version_record_id uuid REFERENCES workflow_version_records(workflow_version_record_id),
  rollback_target_workflow_version_record_id uuid REFERENCES workflow_version_records(workflow_version_record_id),
  actor_id uuid REFERENCES actors(actor_id),
  occurred_at timestamptz NOT NULL DEFAULT now(),
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,

  CONSTRAINT workflow_publish_history_action_check
    CHECK (action IN ('published', 'rolled_back')),
  CONSTRAINT workflow_publish_history_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS workflow_publish_history_family_idx
  ON workflow_publish_history (tenant_id, workflow_family_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS workflow_templates (
  workflow_template_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  intent text NOT NULL,
  hcm_domain text NOT NULL,
  description text,
  template_version integer NOT NULL,
  config_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  config_checksum text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_templates_status_check
    CHECK (status IN ('active', 'archived')),
  CONSTRAINT workflow_templates_config_object_check
    CHECK (jsonb_typeof(config_json) = 'object'),
  CONSTRAINT workflow_templates_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT workflow_templates_intent_version_unique
    UNIQUE (intent, template_version)
);

CREATE INDEX IF NOT EXISTS workflow_templates_status_idx
  ON workflow_templates (status, intent);

CREATE TABLE IF NOT EXISTS workflow_integration_bindings (
  workflow_integration_binding_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  environment_id uuid NOT NULL REFERENCES environments(environment_id),
  abstract_connection_id text NOT NULL,
  connector_id text NOT NULL,
  connector_environment text NOT NULL,
  secret_ref text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  allowed_operations text[] NOT NULL DEFAULT ARRAY[]::text[],
  timeout_ms integer NOT NULL,
  retry_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
  reconciliation jsonb NOT NULL DEFAULT '{}'::jsonb,
  idempotency_scope text NOT NULL,
  allow_sandbox_in_production boolean NOT NULL DEFAULT false,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT workflow_integration_bindings_environment_check
    CHECK (connector_environment IN ('sandbox', 'staging', 'production')),
  CONSTRAINT workflow_integration_bindings_timeout_check
    CHECK (timeout_ms > 0),
  CONSTRAINT workflow_integration_bindings_idempotency_scope_check
    CHECK (idempotency_scope IN ('workflow_instance', 'transaction_plan', 'node', 'external_operation')),
  CONSTRAINT workflow_integration_bindings_retry_policy_object_check
    CHECK (jsonb_typeof(retry_policy) = 'object'),
  CONSTRAINT workflow_integration_bindings_reconciliation_object_check
    CHECK (jsonb_typeof(reconciliation) = 'object'),
  CONSTRAINT workflow_integration_bindings_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object'),
  CONSTRAINT workflow_integration_bindings_connection_unique
    UNIQUE (tenant_id, environment_id, abstract_connection_id)
);

CREATE INDEX IF NOT EXISTS workflow_integration_bindings_tenant_environment_idx
  ON workflow_integration_bindings (tenant_id, environment_id, abstract_connection_id);
