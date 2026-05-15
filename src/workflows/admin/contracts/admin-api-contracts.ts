export type AdminResponseMetadata = {
  correlationId: string;
  generatedAt: string;
};

export type AdminWorkflowSummaryResponse = AdminResponseMetadata & {
  workflows: WorkflowRegistryListItem[];
};

export type WorkflowRegistryListItem = {
  workflowDefinitionId: string;
  intent?: string;
  name: string;
  workflowType: string;
  status: string;
  currentVersionId?: string;
  versions: WorkflowVersionSummaryItem[];
};

export type WorkflowVersionSummaryItem = {
  workflowVersionId: string;
  workflowDefinitionId: string;
  versionNumber: number;
  status: string;
  intent?: unknown;
  configHash?: string;
  validationStatus?: string;
};

export type WorkflowDraftEditorResponse = AdminResponseMetadata & {
  workflowDefinitionId: string;
  workflowVersionId: string;
  versionNumber: number;
  workflowConfig: Record<string, unknown>;
};

export type WorkflowValidationResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  validation: {
    valid: boolean;
    errors: WorkflowAdminIssue[];
    warnings: WorkflowAdminIssue[];
    configHash: string;
  };
};

export type WorkflowAdminIssue = {
  severity: string;
  code: string;
  path: string;
  message: string;
};

export type MermaidPreviewResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  mermaid: string;
  metadata: {
    nodeCount: number;
    edgeCount: number;
    terminalCount: number;
    unreachableNodeIds: string[];
  };
};

export type BlockCatalogResponse = AdminResponseMetadata & {
  blocks: BlockCatalogItem[];
};

export type BlockCatalogItem = {
  name: string;
  version: string;
  domain: string;
  runtime: "go";
  sideEffectClass: string;
  inputContract: Record<string, unknown>;
  outputContract: Record<string, unknown>;
  capabilities: string[];
};

export type InputMappingPreviewResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  nodeId: string;
  mappings: InputMappingPreviewItem[];
};

export type InputMappingPreviewItem = {
  targetField: string;
  source: string;
  path?: string;
  required: boolean;
  resolvedValue?: unknown;
  status: "resolved" | "missing" | "redacted";
};

export type InteractionPreviewResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  state: string;
  interaction: Record<string, unknown>;
  visibleFieldPaths: string[];
  redactedFieldPaths: string[];
};

export type PermissionPreviewResponse = AdminResponseMetadata & {
  actorId: string;
  workflowIntent: string;
  workflowState: string;
  allowedActions: string[];
  deniedActions: Array<{
    action: string;
    reasonCode: string;
  }>;
  visibleFieldPaths: string[];
  hiddenFieldPaths: string[];
  aiVisibleFieldPaths: string[];
};

export type SimulatorResultResponse = AdminResponseMetadata & {
  workflowVersionId?: string;
  finalState: string;
  finalStatus: string;
  trace: SimulatorTraceItem[];
  proposedLedgerEvents: Record<string, unknown>[];
  transactionPlan?: Record<string, unknown>;
};

export type SimulatorTraceItem = {
  nodeId: string;
  nodeType: string;
  status: string;
  routeDecision?: string;
  validationErrors: WorkflowAdminIssue[];
  warnings: WorkflowAdminIssue[];
};

export type DiffResultResponse = AdminResponseMetadata & {
  fromWorkflowVersionId?: string;
  toWorkflowVersionId: string;
  items: DiffResultItem[];
  impactSummary: {
    riskLevel: "low" | "medium" | "high";
    highRiskChangeCount: number;
    newExternalCallCount: number;
    removedApprovalCount: number;
  };
};

export type DiffResultItem = {
  category: string;
  path: string;
  changeType: "added" | "removed" | "changed";
  riskLevel: "low" | "medium" | "high";
  summary: string;
};

export type ApprovalGateAdminResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  gates: ApprovalGateAdminItem[];
};

export type ApprovalGateAdminItem = {
  gateId: string;
  mode: string;
  passRule: Record<string, unknown>;
  resolverCount: number;
  validationErrors: WorkflowAdminIssue[];
};

export type IntegrationBindingResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  bindings: IntegrationBindingItem[];
};

export type IntegrationBindingItem = {
  connectionId: string;
  connectorId: string;
  environment: string;
  status: string;
  allowedOperations: string[];
};

export type PublishGuardrailResponse = AdminResponseMetadata & {
  workflowVersionId: string;
  publishable: boolean;
  blockers: WorkflowAdminIssue[];
  warnings: WorkflowAdminIssue[];
};

export type PublishResultResponse = AdminResponseMetadata & {
  published: number;
  rejected: number;
  workflowDefinitionId: string;
  workflowVersionId: string;
};

export type RollbackResultResponse = AdminResponseMetadata & {
  rolledBack: number;
  workflowDefinitionId: string;
  previousWorkflowVersionId: string;
  activeWorkflowVersionId: string;
};

export type RuntimeDebuggerResponse = AdminResponseMetadata & {
  workflowInstanceId: string;
  workflowIntent: string;
  pinnedWorkflowVersion: PinnedWorkflowVersionDebugInfo;
  currentState: string;
  currentGraphNode?: GraphNodeDebugInfo;
  workflowStatus: string;
  workflowVersion: number;
  lastTransition?: TransitionDebugInfo;
  pendingApprovalTasks: ApprovalTaskDebugInfo[];
  approvalGate?: ApprovalGateDebugInfo;
  currentRepair?: RepairRequirementDebugInfo;
  nodeExecutionHistory: NodeExecutionDebugInfo[];
  ledgerEventsByNode: Record<string, LedgerEventDebugInfo[]>;
  externalCallsByNode: Record<string, ExternalCallDebugInfo[]>;
  failure?: WorkflowFailureDebugInfo;
  compensationEligibility: CompensationEligibilityDebugInfo;
  stuckDetections: StuckWorkflowDetection[];
  repairOptions: RepairOptionDebugInfo[];
};

export type PinnedWorkflowVersionDebugInfo = {
  workflowDefinitionId: string;
  workflowVersionId: string;
  versionNumber?: number;
  status?: string;
  configHash?: string;
};

export type GraphNodeDebugInfo = {
  nodeId: string;
  type: string;
  title: string;
  state?: string;
  routeKeys: string[];
};

export type TransitionDebugInfo = {
  transition: string;
  actorId: string;
  idempotencyKey: string;
  expectedVersion: number;
  status: string;
  result?: Record<string, unknown>;
  error?: Record<string, unknown>;
};

export type ApprovalTaskDebugInfo = {
  approvalTaskId: string;
  assigneeActorId: string;
  assigneeRole: string;
  status: string;
  gateNodeId?: string;
  taskVersion?: number;
};

export type ApprovalGateDebugInfo = {
  approvalGroupId: string;
  gateNodeId: string;
  mode: string;
  status: string;
  passRule: Record<string, unknown>;
  failurePolicy: string;
  taskCount: number;
};

export type RepairRequirementDebugInfo = {
  state: string;
  status: string;
  interaction: Record<string, unknown>;
  requiredFields: string[];
};

export type NodeExecutionDebugInfo = {
  nodeId: string;
  nodeType?: string;
  title?: string;
  eventTypes: string[];
  firstEventSequence: number;
  lastEventSequence: number;
  routeKeys: string[];
  routeDecision?: {
    routeKeys: string[];
    matchedCondition: string;
  };
  blockInputSummary?: unknown;
  blockOutputSummary?: unknown;
};

export type WorkflowFailureDebugInfo = {
  failedNodeId: string;
  errorCode: string;
  safeMessage: string;
};

export type CompensationEligibilityDebugInfo = {
  eligible: boolean;
  reason: string;
};

export type LedgerEventDebugInfo = {
  eventId: string;
  eventType: string;
  eventSequence: number;
  occurredAt: string;
  actorId: string;
  payload: unknown;
};

export type ExternalCallDebugInfo = {
  connectionId: string;
  operation: string;
  status: string;
  idempotencyKey?: string;
  attemptCount?: number;
  requestPayload?: unknown;
  responsePayload?: unknown;
};

export type StuckWorkflowDetection = {
  code: string;
  severity: "info" | "warning" | "error";
  message: string;
  details: Record<string, unknown>;
};

export type RepairOptionDebugInfo = {
  action: string;
  label: string;
  enabled: boolean;
  reason?: string;
};

export type RepairActionResponse = AdminResponseMetadata & {
  workflowInstanceId: string;
  action: string;
  accepted: boolean;
  idempotentReplay?: boolean;
  workflowVersion: number;
  ledgerEventIds: string[];
  repairOptions: RepairOptionDebugInfo[];
};
