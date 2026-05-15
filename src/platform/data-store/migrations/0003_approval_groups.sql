CREATE TABLE IF NOT EXISTS approval_groups (
  approval_group_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(tenant_id),
  workflow_instance_id uuid NOT NULL REFERENCES workflow_instances(workflow_instance_id) ON DELETE CASCADE,
  change_request_id uuid REFERENCES change_requests(change_request_id) ON DELETE CASCADE,
  gate_node_id text NOT NULL,
  mode text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  pass_rule jsonb NOT NULL DEFAULT '{}'::jsonb,
  failure_policy text NOT NULL,
  current_sequence_index integer,
  opened_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz,
  failed_at timestamptz,
  canceled_at timestamptz,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT approval_groups_mode_check
    CHECK (mode IN ('sequential', 'parallel')),
  CONSTRAINT approval_groups_status_check
    CHECK (status IN ('active', 'passed', 'failed', 'repair', 'canceled')),
  CONSTRAINT approval_groups_sequence_check
    CHECK (current_sequence_index IS NULL OR current_sequence_index >= 0),
  CONSTRAINT approval_groups_pass_rule_object_check
    CHECK (jsonb_typeof(pass_rule) = 'object'),
  CONSTRAINT approval_groups_metadata_object_check
    CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS approval_groups_workflow_idx
  ON approval_groups (tenant_id, workflow_instance_id, created_at DESC);

CREATE INDEX IF NOT EXISTS approval_groups_active_workflow_idx
  ON approval_groups (tenant_id, workflow_instance_id, gate_node_id)
  WHERE status = 'active';

ALTER TABLE approval_tasks
  ADD COLUMN IF NOT EXISTS approval_group_id uuid REFERENCES approval_groups(approval_group_id),
  ADD COLUMN IF NOT EXISTS gate_node_id text,
  ADD COLUMN IF NOT EXISTS sequence_index integer,
  ADD COLUMN IF NOT EXISTS weight numeric(12,4),
  ADD COLUMN IF NOT EXISTS is_veto_holder boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS resolved_from jsonb,
  ADD COLUMN IF NOT EXISTS task_version integer NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS approval_tasks_group_status_idx
  ON approval_tasks (tenant_id, approval_group_id, status, sequence_index, created_at)
  WHERE approval_group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS approval_tasks_workflow_gate_idx
  ON approval_tasks (tenant_id, workflow_instance_id, gate_node_id, status, created_at)
  WHERE gate_node_id IS NOT NULL;
