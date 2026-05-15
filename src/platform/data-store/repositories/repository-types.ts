export type DatabaseJson = Record<string, unknown>;

export type TenantRecord = {
  tenant_id: string;
  name: string;
  slug: string;
  status: string;
  default_timezone: string;
  default_locale: string;
  data_region: string | null;
  settings: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type EnvironmentRecord = {
  environment_id: string;
  tenant_id: string;
  name: string;
  type: string;
  status: string;
  created_at: Date;
  updated_at: Date;
};

export type ActorRecord = {
  actor_id: string;
  tenant_id: string;
  actor_type: string;
  linked_worker_id: string | null;
  email: string | null;
  display_name: string;
  status: string;
  roles: unknown[];
  groups: unknown[];
  auth_provider: string | null;
  external_subject: string | null;
  metadata: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type WorkflowDefinitionRecord = {
  workflow_definition_id: string;
  tenant_id: string;
  name: string;
  workflow_type: string;
  status: string;
  current_version_id: string | null;
  created_at: Date;
  updated_at: Date;
  metadata: DatabaseJson;
};

export type WorkflowVersionRecord = {
  workflow_version_id: string;
  tenant_id: string;
  workflow_definition_id: string;
  version_number: number;
  graph_definition: DatabaseJson;
  input_schema: DatabaseJson;
  output_schema: DatabaseJson;
  validation_rules: unknown[];
  approval_rules: unknown[];
  ai_review_scope: DatabaseJson;
  status: string;
  published_by: string | null;
  published_at: Date | null;
  created_at: Date;
  updated_at: Date;
};

export type WorkflowDefinitionWithVersionRecord = {
  definition: WorkflowDefinitionRecord;
  version: WorkflowVersionRecord;
};

export type WorkflowInstanceRecord = {
  workflow_instance_id: string;
  tenant_id: string;
  environment_id: string | null;
  workflow_definition_id: string;
  workflow_version_id: string;
  intent: string;
  subject_type: string;
  subject_id: string;
  status: string;
  state: string;
  change_request_id: string | null;
  requester_actor_id: string;
  current_interaction: DatabaseJson;
  context: DatabaseJson;
  started_at: Date;
  completed_at: Date | null;
  canceled_at: Date | null;
  failed_at: Date | null;
  version: number;
  correlation_id: string;
  metadata: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type WorkflowTransitionAttemptRecord = {
  workflow_transition_attempt_id: string;
  tenant_id: string;
  workflow_instance_id: string;
  transition: string;
  idempotency_key: string;
  expected_version: number;
  actor_id: string;
  request_payload: DatabaseJson;
  response_payload: DatabaseJson | null;
  status: string;
  error: DatabaseJson | null;
  created_at: Date;
  completed_at: Date | null;
};

export type LedgerEventRecord = {
  event_id: string;
  tenant_id: string;
  environment_id: string | null;
  event_type: string;
  event_version: number;
  event_sequence: string;
  subject_type: string;
  subject_id: string;
  occurred_at: Date;
  effective_at: Date | null;
  recorded_at: Date;
  actor_type: string;
  actor_id: string | null;
  actor_role: string | null;
  relationship_context: DatabaseJson;
  workflow_instance_id: string | null;
  workflow_definition_id: string | null;
  workflow_version_id: string | null;
  change_request_id: string | null;
  transaction_plan_id: string | null;
  approval_task_id: string | null;
  block_name: string | null;
  block_version: string | null;
  correlation_id: string;
  causation_id: string | null;
  idempotency_key: string | null;
  permission_snapshot: DatabaseJson;
  ai_visibility_snapshot: DatabaseJson;
  payload: DatabaseJson;
  previous_hash: string | null;
  event_hash: string | null;
};

export type ChangeRequestRecord = {
  change_request_id: string;
  tenant_id: string;
  environment_id: string | null;
  change_type: string;
  target_worker_id: string;
  requester_actor_id: string;
  effective_at: Date | null;
  business_reason: string | null;
  status: string;
  priority: string;
  current_snapshot: DatabaseJson;
  proposed_snapshot: DatabaseJson;
  preflight_result: DatabaseJson;
  ai_review: DatabaseJson;
  workflow_definition_id: string | null;
  workflow_version_id: string | null;
  transaction_plan_id: string | null;
  submitted_at: Date | null;
  approved_at: Date | null;
  executed_at: Date | null;
  closed_at: Date | null;
  created_at: Date;
  updated_at: Date;
  created_by: string | null;
  updated_by: string | null;
  version: number;
  metadata: DatabaseJson;
};

export type ProposedChangeRecord = {
  proposed_change_id: string;
  tenant_id: string;
  change_request_id: string;
  target_object_type: string;
  target_object_id: string;
  field_path: string;
  current_value: unknown | null;
  proposed_value: unknown;
  effective_at: Date | null;
  reason_code: string | null;
  validation_status: string;
  risk_level: string;
  metadata: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type DocumentRecord = {
  document_id: string;
  tenant_id: string;
  environment_id: string | null;
  workflow_instance_id: string | null;
  created_by_actor_id: string;
  purpose: string;
  filename: string;
  content_type: string;
  classification: string;
  status: string;
  storage_key: string | null;
  metadata: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type WorkflowInstanceDocumentRecord = {
  workflow_instance_document_id: string;
  tenant_id: string;
  workflow_instance_id: string;
  document_id: string;
  change_request_id: string | null;
  purpose: string;
  attached_by_actor_id: string;
  created_at: Date;
};

export type ApprovalTaskRecord = {
  approval_task_id: string;
  tenant_id: string;
  change_request_id: string;
  workflow_instance_id: string | null;
  assignee_actor_id: string | null;
  assignee_role: string | null;
  assignee_relationship: string | null;
  approval_type: string;
  status: string;
  decision: string | null;
  decision_reason: string | null;
  comments: string | null;
  due_at: Date | null;
  delegated_to_actor_id: string | null;
  created_at: Date;
  decided_at: Date | null;
  metadata: DatabaseJson;
};

export type TransactionPlanRecord = {
  transaction_plan_id: string;
  tenant_id: string;
  change_request_id: string;
  status: string;
  plan_version: number;
  steps: unknown[];
  internal_writes: unknown[];
  external_writes: unknown[];
  rollback_plan: DatabaseJson;
  compensation_plan: DatabaseJson;
  idempotency_keys: DatabaseJson;
  simulation_result: DatabaseJson;
  execution_result: DatabaseJson;
  reconciliation_result: DatabaseJson;
  created_at: Date;
  updated_at: Date;
  created_by: string | null;
  updated_by: string | null;
};

export type EmployeeProjectionRecord = {
  tenant_id: string;
  employee_id: string;
  projection_version: number;
  as_of_effective_at: Date | null;
  source_event_id: string | null;
  source_event_sequence: string | null;
  document: DatabaseJson;
  indexed_fields: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type ChangeRequestProjectionRecord = {
  tenant_id: string;
  change_request_id: string;
  projection_version: number;
  source_event_id: string | null;
  source_event_sequence: string | null;
  status: string;
  target_worker_id: string;
  change_type: string;
  effective_at: Date | null;
  summary: DatabaseJson;
  document: DatabaseJson;
  created_at: Date;
  updated_at: Date;
};

export type IntegrationOutboxRecord = {
  outbox_id: string;
  tenant_id: string;
  integration_connection_id: string | null;
  change_request_id: string | null;
  transaction_plan_id: string | null;
  ledger_event_id: string | null;
  destination: string;
  operation: string;
  request_payload: DatabaseJson;
  response_payload: DatabaseJson | null;
  status: string;
  attempt_count: number;
  max_attempts: number;
  next_attempt_at: Date | null;
  last_attempt_at: Date | null;
  error_code: string | null;
  error_message: string | null;
  idempotency_key: string;
  created_at: Date;
  updated_at: Date;
};
