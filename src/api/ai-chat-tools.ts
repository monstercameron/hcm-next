import {
  ERROR_CODES,
  err,
  ok,
  systemError,
  type AppError,
  type Result,
} from "@hcm-next/foundation";
import type {
  AiChatToolDefinition,
  AiGeneratedPageDefinition,
  AiUiGenerationActor,
  AiUiGenerationRequest,
  AiUiGenerationSubject,
} from "@hcm-next/ai-client";
import type { EmployeeProjectionRecord } from "@hcm-next/data-store";
import type { AppDependencies } from "./dependencies.js";
import type { ApiRequestContext } from "./request-context.js";
import { sha1IdempotencyKey } from "./ai-idempotency.js";
import {
  computeWorkflowConfigHash,
  getWorkflowConfigByIntent,
  type WorkflowConfig,
} from "../workflows/registry/index.js";
import {
  configuredWorkflowIntents,
  findFilesystemWorkflowConfigByIntent,
} from "../workflows/shared/workflow-config.js";
import {
  getAvailableActions,
  getEmployeeProjection,
  getTasks,
  getTimeline,
  getWorkflowInstance,
  listWorkflowInstances,
  startWorkflowIntent,
  transitionWorkflow,
} from "../workflows/runtime/service.js";

/**
 * Catalog of the tools the chat agent may invoke. Defined in one place so the
 * route handler can register them with the AI client and the dispatcher can
 * look them up by name.
 */
export const AI_CHAT_TOOL_DEFINITIONS: readonly AiChatToolDefinition[] = [
  {
    name: "list_workflows",
    description:
      "List the workflow intents the acting user can start. Each entry includes the intent id, a human title, a description, the subject type, and the names of fields the workflow declares for submission.",
    parametersJsonSchema: {
      type: "object",
      properties: {},
      additionalProperties: false,
    },
  },
  {
    name: "search_employees",
    description:
      "Find employees by display name. With no query, returns the first 5 employees in the workspace so the assistant can suggest candidates.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        query: {
          type: "string",
          description:
            "Case-insensitive substring of the employee's display name. Omit to list the first 5 employees.",
        },
      },
      additionalProperties: false,
    },
  },
  {
    name: "generate_ui_page",
    description:
      "Render a workflow page in the main area of the screen. The page always includes an employee picker at the top so the user can choose or change the subject visually. Call this as soon as the user expresses intent to start a workflow — you do NOT need to know the subject employee first.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        intent: {
          type: "string",
          description: "Workflow intent id, e.g. 'employee.termination'.",
        },
        subjectId: {
          type: "string",
          description:
            "Optional employee id the workflow applies to. Omit to let the user pick from the on-page picker.",
        },
        currentState: {
          type: "string",
          description:
            "Optional workflow state id if the assistant is targeting a specific stage.",
        },
      },
      required: ["intent"],
      additionalProperties: false,
    },
  },
  {
    name: "get_employee",
    description:
      "Fetch a single employee profile by id. Returns display name, job title, department, manager, and employment status with permission-restricted fields removed.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        employeeId: {
          type: "string",
          description: "Employee id (e.g. emp_001).",
        },
      },
      required: ["employeeId"],
      additionalProperties: false,
    },
  },
  {
    name: "get_workflow_config",
    description:
      "Summarise the structure of a configured workflow: title, subject type, declared submit inputs, states with their available transitions, and the approval assignee. Use this when the user asks how a workflow works or what it needs.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        intent: {
          type: "string",
          description: "Workflow intent id, e.g. 'employee.termination'.",
        },
      },
      required: ["intent"],
      additionalProperties: false,
    },
  },
  {
    name: "start_workflow",
    description:
      "Start a new workflow instance on behalf of the user. Only call this after the user has confirmed they want to proceed and supplied the required inputs.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        intent: {
          type: "string",
          description: "Workflow intent id, e.g. 'employee.contact_info_update'.",
        },
        subjectId: {
          type: "string",
          description: "Employee id the workflow applies to.",
        },
        input: {
          type: "object",
          description: "Object whose keys match the workflow's declared submit inputs.",
          additionalProperties: true,
        },
      },
      required: ["intent", "subjectId", "input"],
      additionalProperties: false,
    },
  },
  {
    name: "list_workflow_instances",
    description:
      "List workflow instances visible to the user. Filter by `assignedToMe: true` to see only approval tasks assigned to the user, or by `state`/`intent` to narrow the listing.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        assignedToMe: {
          type: "boolean",
          description:
            "When true, list only workflows where the user has a pending approval task.",
        },
        state: {
          type: "string",
          description: "Optional workflow state filter, e.g. 'pending_approval'.",
        },
        intent: {
          type: "string",
          description: "Optional intent filter, e.g. 'employee.termination'.",
        },
      },
      additionalProperties: false,
    },
  },
  {
    name: "get_workflow_instance",
    description:
      "Fetch the current snapshot of a workflow instance — state, status, input, and the active interaction.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        workflowInstanceId: {
          type: "string",
          description: "Workflow instance id (e.g. wfi_...).",
        },
      },
      required: ["workflowInstanceId"],
      additionalProperties: false,
    },
  },
  {
    name: "get_workflow_timeline",
    description:
      "Fetch the chronological timeline of business events for a workflow instance. Limited to the 20 most recent entries.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        workflowInstanceId: {
          type: "string",
          description: "Workflow instance id (e.g. wfi_...).",
        },
      },
      required: ["workflowInstanceId"],
      additionalProperties: false,
    },
  },
  {
    name: "get_available_actions",
    description:
      "Fetch the transitions the current user can take on a workflow instance right now.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        workflowInstanceId: {
          type: "string",
          description: "Workflow instance id.",
        },
      },
      required: ["workflowInstanceId"],
      additionalProperties: false,
    },
  },
  {
    name: "transition_workflow",
    description:
      "Move a workflow forward — approve, reject, execute, request more info, or cancel. Only call this after the user has confirmed the action. If the response carries `error.code: 'version_conflict'`, call `get_workflow_instance` for the latest version and retry.",
    parametersJsonSchema: {
      type: "object",
      properties: {
        workflowInstanceId: {
          type: "string",
          description: "Workflow instance id.",
        },
        transition: {
          type: "string",
          description:
            "Transition name as listed by `get_available_actions`, e.g. 'approve', 'reject', 'execute'.",
        },
        body: {
          type: "object",
          description:
            "Optional payload for the transition (reason, approval comments, etc.).",
          additionalProperties: true,
        },
      },
      required: ["workflowInstanceId", "transition"],
      additionalProperties: false,
    },
  },
  {
    name: "get_my_tasks",
    description:
      "List approval tasks pending for the current user across all workflows.",
    parametersJsonSchema: {
      type: "object",
      properties: {},
      additionalProperties: false,
    },
  },
] as const;

const EMPLOYEE_SEARCH_DEFAULT_LIMIT = 5;
const EMPLOYEE_SEARCH_QUERY_LIMIT = 10;
const SUBJECT_PICKER_OPTION_LIMIT = 10;

/**
 * Successful side effects from a single tool invocation. Tool results are
 * always serialised to JSON for the agent; `renderPage` is captured separately
 * so the route handler can attach it to its response without pushing the full
 * PageDefinition back into the model's context window.
 */
export type ToolExecutionSideEffects = {
  toolResultJson: unknown;
  renderPage?: AiGeneratedPageDefinition;
  workflowConfigHash?: string;
};

export type ExecuteToolCallInput = {
  name: string;
  arguments: Record<string, unknown>;
  dependencies: AppDependencies;
  requestContext: ApiRequestContext;
};

/**
 * Dispatches a single tool call invoked by the chat agent. Tool errors that
 * the agent could recover from (bad arguments, permission denial, missing
 * workflow) are surfaced through the `toolResultJson` channel as plain
 * serialisable error objects so the agent can read them and adjust. Only
 * unrecoverable failures (no aiClient configured, etc.) flow through Result.err.
 */
export async function executeToolCall(
  input: ExecuteToolCallInput,
): Promise<Result<ToolExecutionSideEffects, AppError>> {
  if (input.name === "list_workflows") {
    return ok({ toolResultJson: listWorkflowsToolResult() });
  }
  if (input.name === "search_employees") {
    return ok({
      toolResultJson: searchEmployeesToolResult(input),
    });
  }
  if (input.name === "generate_ui_page") {
    return generateUiPageToolResult(input);
  }
  if (input.name === "get_employee") {
    return ok({ toolResultJson: getEmployeeToolResult(input) });
  }
  if (input.name === "get_workflow_config") {
    return ok({ toolResultJson: getWorkflowConfigToolResult(input) });
  }
  if (input.name === "start_workflow") {
    return ok({ toolResultJson: await startWorkflowToolResult(input) });
  }
  if (input.name === "list_workflow_instances") {
    return ok({ toolResultJson: listWorkflowInstancesToolResult(input) });
  }
  if (input.name === "get_workflow_instance") {
    return ok({ toolResultJson: getWorkflowInstanceToolResult(input) });
  }
  if (input.name === "get_workflow_timeline") {
    return ok({ toolResultJson: getWorkflowTimelineToolResult(input) });
  }
  if (input.name === "get_available_actions") {
    return ok({ toolResultJson: getAvailableActionsToolResult(input) });
  }
  if (input.name === "transition_workflow") {
    return ok({ toolResultJson: await transitionWorkflowToolResult(input) });
  }
  if (input.name === "get_my_tasks") {
    return ok({ toolResultJson: getMyTasksToolResult(input) });
  }
  return ok({
    toolResultJson: serializableToolError(
      "unknown_tool",
      `No tool registered with name "${input.name}".`,
    ),
  });
}

type ListWorkflowsToolResultEntry = {
  intent: string;
  title?: string;
  description?: string;
  subjectType: string;
  declaredInputFields: string[];
};

function listWorkflowsToolResult(): {
  workflows: ListWorkflowsToolResultEntry[];
} {
  const entries: ListWorkflowsToolResultEntry[] = [];
  for (const intent of configuredWorkflowIntents()) {
    const result = findFilesystemWorkflowConfigByIntent(intent);
    if (!result.ok) {
      continue;
    }
    const config = result.value;
    entries.push({
      intent: config.intent,
      ...(config.metadata?.title !== undefined ? { title: config.metadata.title } : {}),
      ...(config.metadata?.description !== undefined
        ? { description: config.metadata.description }
        : {}),
      subjectType: config.subjectType,
      declaredInputFields: collectDeclaredInputFields(config),
    });
  }
  return { workflows: entries };
}

function collectDeclaredInputFields(config: WorkflowConfig): string[] {
  const fields = new Set<string>();
  const proposedSnapshot = config.submit.proposedSnapshot;
  if (typeof proposedSnapshot === "object" && proposedSnapshot !== null) {
    for (const key of Object.keys(proposedSnapshot)) {
      fields.add(key);
    }
  }
  const currentSnapshot = config.submit.currentSnapshot;
  if (typeof currentSnapshot === "object" && currentSnapshot !== null) {
    for (const key of Object.keys(currentSnapshot)) {
      fields.add(key);
    }
  }
  return [...fields];
}

type SearchEmployeesToolResultEntry = {
  id: string;
  displayName: string;
  jobTitle?: string;
  department?: string;
  employmentStatus: string;
};

function searchEmployeesToolResult(
  input: ExecuteToolCallInput,
): { employees: SearchEmployeesToolResultEntry[] } | Record<string, unknown> {
  const projectionResult =
    input.dependencies.repositories.employeeProjections.findByTenant(
      input.requestContext.tenantId,
    );
  if (!projectionResult.ok) {
    return serializableToolError(
      "employee_search_failed",
      "Could not load employees for this workspace.",
    );
  }

  const rawQuery = input.arguments["query"];
  const queryText =
    typeof rawQuery === "string" && rawQuery.trim().length > 0
      ? rawQuery.trim().toLowerCase()
      : undefined;

  const all = projectionResult.value;
  const filtered =
    queryText === undefined
      ? all.slice(0, EMPLOYEE_SEARCH_DEFAULT_LIMIT)
      : all
          .filter((record) =>
            record.document.person.displayName.toLowerCase().includes(queryText),
          )
          .slice(0, EMPLOYEE_SEARCH_QUERY_LIMIT);

  return {
    employees: filtered.map((record) => projectionToToolEntry(record)),
  };
}

function projectionToToolEntry(
  record: EmployeeProjectionRecord,
): SearchEmployeesToolResultEntry {
  const document = record.document;
  const entry: SearchEmployeesToolResultEntry = {
    id: record.employeeId,
    displayName: document.person.displayName,
    employmentStatus: document.employment.status,
  };
  if (typeof document.job.title === "string" && document.job.title.length > 0) {
    entry.jobTitle = document.job.title;
  }
  if (
    typeof document.organization.department === "string" &&
    document.organization.department.length > 0
  ) {
    entry.department = document.organization.department;
  }
  return entry;
}

async function generateUiPageToolResult(
  input: ExecuteToolCallInput,
): Promise<Result<ToolExecutionSideEffects, AppError>> {
  const intentArg = input.arguments["intent"];
  const subjectIdArg = input.arguments["subjectId"];
  const currentStateArg = input.arguments["currentState"];

  if (typeof intentArg !== "string" || intentArg.trim().length === 0) {
    return ok({
      toolResultJson: serializableToolError(
        "intent_required",
        "Argument 'intent' is required.",
      ),
    });
  }

  const subjectId =
    typeof subjectIdArg === "string" && subjectIdArg.trim().length > 0
      ? subjectIdArg
      : undefined;

  if (input.dependencies.aiClient === undefined) {
    return err(systemError({ reason: "ai_client_not_configured" }));
  }

  const workflowConfigResult = getWorkflowConfigByIntent(
    input.dependencies.repositories,
    intentArg,
  );
  if (!workflowConfigResult.ok) {
    return ok({
      toolResultJson: serializableToolError(
        "workflow_not_found",
        `No workflow is registered for intent "${intentArg}".`,
      ),
    });
  }
  const workflowConfig = workflowConfigResult.value;

  const permissionResult = checkCanStartIntent({
    workflowConfig,
    actor: input.requestContext.actor,
    subjectId,
  });
  if (!permissionResult.ok) {
    return ok({
      toolResultJson: serializableToolError(
        "permission_denied",
        `The acting user is not permitted to start "${intentArg}".`,
      ),
    });
  }

  // Load the picker candidate pool. We always include the first N employees so
  // the subject picker has visible options even when no subjectId was passed.
  // The same pool also seeds the manager-name lookup so subject summaries
  // render a human name instead of a bare manager employee id.
  const tenantProjections = loadTenantProjections(input);
  const managerLookup = buildManagerLookup(tenantProjections);

  let subjectSummary: AiUiGenerationSubject | undefined;
  if (subjectId !== undefined) {
    const subjectProjectionResult =
      input.dependencies.repositories.employeeProjections.findByEmployeeId(
        input.requestContext.tenantId,
        subjectId,
      );
    if (!subjectProjectionResult.ok) {
      return ok({
        toolResultJson: serializableToolError(
          "employee_not_found",
          `No employee with id "${subjectId}" was found.`,
        ),
      });
    }
    subjectSummary = subjectFromProjection(
      subjectProjectionResult.value,
      managerLookup,
    );
  }

  const employeeOptions = tenantProjections
    .slice(0, SUBJECT_PICKER_OPTION_LIMIT)
    .map((record) => subjectFromProjection(record, managerLookup));

  const workflowConfigHash = computeWorkflowConfigHash(workflowConfig);
  const userPrompt =
    typeof currentStateArg === "string" && currentStateArg.length > 0
      ? `Render the ${intentArg} workflow at state "${currentStateArg}".`
      : `Render the ${intentArg} workflow.`;
  const idempotencyKey = idempotencyKeyForChatToolRequest({
    workflowIntent: intentArg,
    actorId: input.requestContext.actor.actorId,
    subjectId,
    currentState: typeof currentStateArg === "string" ? currentStateArg : undefined,
  });

  const aiRequest: AiUiGenerationRequest = {
    workflow: workflowConfig,
    workflowConfigHash,
    ...(typeof currentStateArg === "string" && currentStateArg.length > 0
      ? { currentState: currentStateArg }
      : {}),
    ...(subjectSummary !== undefined ? { subject: subjectSummary } : {}),
    employeeOptions,
    actor: actorSummary(input.requestContext.actor),
    userPrompt,
    correlationId: input.requestContext.correlationId,
    idempotencyKey,
  };

  const generationResult = await input.dependencies.aiClient.generatePageDefinition(
    aiRequest,
    input.dependencies.logger,
  );
  if (!generationResult.ok) {
    return ok({
      toolResultJson: serializableToolError(
        "ui_generation_failed",
        generationResult.error.safeMessage,
      ),
    });
  }

  const page = generationResult.value.page;
  const widgetCount = page.regions.reduce(
    (total, region) => total + region.widgets.length,
    0,
  );

  return ok({
    toolResultJson: {
      ok: true,
      pageId: page.id,
      regions: page.regions.length,
      widgets: widgetCount,
    },
    renderPage: page,
    workflowConfigHash,
  });
}

function loadTenantProjections(
  input: ExecuteToolCallInput,
): readonly EmployeeProjectionRecord[] {
  const projectionResult =
    input.dependencies.repositories.employeeProjections.findByTenant(
      input.requestContext.tenantId,
    );
  if (!projectionResult.ok) {
    return [];
  }
  return projectionResult.value;
}

function buildManagerLookup(
  projections: readonly EmployeeProjectionRecord[],
): ReadonlyMap<string, string> {
  const lookup = new Map<string, string>();
  for (const record of projections) {
    lookup.set(record.employeeId, record.document.person.displayName);
  }
  return lookup;
}

function checkCanStartIntent(input: {
  workflowConfig: WorkflowConfig;
  actor: ApiRequestContext["actor"];
  subjectId: string | undefined;
}): Result<true, AppError> {
  const { workflowConfig, actor, subjectId } = input;

  if (
    workflowConfig.selfServiceStart &&
    subjectId !== undefined &&
    actor.linkedWorkerId !== undefined &&
    actor.linkedWorkerId === subjectId
  ) {
    return ok(true);
  }

  const startActors = Array.isArray(workflowConfig.startActors)
    ? workflowConfig.startActors.map(String)
    : [];

  for (const actorToken of startActors) {
    if (actorMatchesRoleToken(actor.roles, actorToken)) {
      return ok(true);
    }
  }

  return err(systemError({ reason: "permission_denied" }));
}

function actorMatchesRoleToken(roles: readonly string[], actorToken: string): boolean {
  if (actorToken.includes("_or_")) {
    return actorToken.split("_or_").some((role) => roles.includes(role));
  }
  return roles.includes(actorToken);
}

function subjectFromProjection(
  projection: EmployeeProjectionRecord,
  managerLookup?: ReadonlyMap<string, string>,
): AiUiGenerationSubject {
  const document = projection.document;
  const jobTitle = document.job.title;
  const department = document.organization.department;
  const managerEmployeeId = document.manager.employeeId;
  const manager =
    managerEmployeeId !== null
      ? (managerLookup?.get(managerEmployeeId) ?? undefined)
      : undefined;
  return {
    id: projection.employeeId,
    displayName: document.person.displayName,
    ...(jobTitle !== undefined && jobTitle.length > 0 ? { jobTitle } : {}),
    ...(department !== undefined && department.length > 0 ? { department } : {}),
    ...(manager !== undefined && manager.length > 0 ? { manager } : {}),
  };
}

function actorSummary(actor: ApiRequestContext["actor"]): AiUiGenerationActor {
  return {
    id: actor.actorId,
    displayName: actor.displayName,
    roles: actor.roles,
  };
}

function idempotencyKeyForChatToolRequest(input: {
  workflowIntent: string;
  actorId: string;
  subjectId: string | undefined;
  currentState: string | undefined;
}): string {
  return sha1IdempotencyKey("ai-chat-tool", [
    input.workflowIntent,
    input.actorId,
    input.subjectId ?? "",
    input.currentState ?? "",
  ]);
}

function serializableToolError(
  code: string,
  message: string,
): { ok: false; error: { code: string; message: string } } {
  return { ok: false, error: { code, message } };
}

const WORKFLOW_TIMELINE_MAX_ENTRIES = 20;

/**
 * Maps a foundation AppError into the ok:false envelope the agent reads.
 * Service-layer errors carry stable codes (NOT_FOUND, PERMISSION_DENIED,
 * VERSION_CONFLICT, ...) that the agent can match against for recovery.
 */
function toolErrorFromAppError(
  error: AppError,
  fallback: { code: string; message: string },
): { ok: false; error: { code: string; message: string } } {
  if (
    error.code === ERROR_CODES.NOT_FOUND ||
    error.code === ERROR_CODES.WORKFLOW_NOT_FOUND
  ) {
    return serializableToolError("not_found", error.safeMessage);
  }
  if (error.code === ERROR_CODES.PERMISSION_DENIED) {
    return serializableToolError("permission_denied", error.safeMessage);
  }
  if (error.code === ERROR_CODES.VERSION_CONFLICT) {
    return serializableToolError("version_conflict", error.safeMessage);
  }
  if (error.code === ERROR_CODES.VALIDATION_FAILED) {
    return serializableToolError("validation_failed", error.safeMessage);
  }
  if (error.code === ERROR_CODES.INVALID_WORKFLOW_TRANSITION) {
    return serializableToolError("invalid_transition", error.safeMessage);
  }
  return serializableToolError(fallback.code, fallback.message);
}

function stringArgument(
  args: Record<string, unknown>,
  field: string,
): string | undefined {
  const value = args[field];
  if (typeof value !== "string" || value.trim().length === 0) {
    return undefined;
  }
  return value;
}

function objectArgument(
  args: Record<string, unknown>,
  field: string,
): Record<string, unknown> | undefined {
  const value = args[field];
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }
  return value as Record<string, unknown>;
}

function booleanArgument(
  args: Record<string, unknown>,
  field: string,
): boolean | undefined {
  const value = args[field];
  return typeof value === "boolean" ? value : undefined;
}

function lookupSubjectDisplayName(
  input: ExecuteToolCallInput,
  subjectId: string | undefined,
): string | undefined {
  if (subjectId === undefined) {
    return undefined;
  }
  const projectionResult =
    input.dependencies.repositories.employeeProjections.findByEmployeeId(
      input.requestContext.tenantId,
      subjectId,
    );
  if (!projectionResult.ok) {
    return undefined;
  }
  return projectionResult.value.document.person.displayName;
}

type GetEmployeeToolResult =
  | {
      ok: true;
      employee: Record<string, unknown>;
    }
  | { ok: false; error: { code: string; message: string } };

function getEmployeeToolResult(input: ExecuteToolCallInput): GetEmployeeToolResult {
  const employeeId = stringArgument(input.arguments, "employeeId");
  if (employeeId === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'employeeId' is required.",
    );
  }

  const projectionResult = getEmployeeProjection(
    input.dependencies,
    input.requestContext,
    employeeId,
  );
  if (!projectionResult.ok) {
    return toolErrorFromAppError(projectionResult.error, {
      code: "employee_not_found",
      message: `Could not load employee ${employeeId}.`,
    });
  }

  const projectionContainer = projectionResult.value["projection"];
  if (typeof projectionContainer !== "object" || projectionContainer === null) {
    return serializableToolError(
      "employee_not_found",
      `Could not load employee ${employeeId}.`,
    );
  }
  const projection = projectionContainer as EmployeeProjectionRecord;
  const document = projection.document;

  const employee: Record<string, unknown> = {
    id: projection.employeeId,
    displayName: document.person.displayName,
    employmentStatus: document.employment.status,
  };
  if (
    typeof document.job.title === "string" &&
    document.job.title.length > 0 &&
    document.job.title !== "[restricted]"
  ) {
    employee["jobTitle"] = document.job.title;
  }
  if (
    typeof document.organization.department === "string" &&
    document.organization.department.length > 0 &&
    document.organization.department !== "[restricted]"
  ) {
    employee["department"] = document.organization.department;
  }
  if (document.manager.employeeId !== null) {
    employee["manager"] = { employeeId: document.manager.employeeId };
  }

  return { ok: true, employee };
}

type GetWorkflowConfigToolResult =
  | {
      ok: true;
      intent: string;
      title?: string;
      description?: string;
      subjectType: string;
      selfServiceStart: boolean;
      startActors: string[];
      declaredInputs: Array<{ name: string; path: string; source: string }>;
      states: Array<{
        id: string;
        actions: Array<{ transition: string; label: string; actor: string }>;
      }>;
      approvers: Array<{ assigneeRole: string; approvalType: string }>;
    }
  | { ok: false; error: { code: string; message: string } };

function getWorkflowConfigToolResult(
  input: ExecuteToolCallInput,
): GetWorkflowConfigToolResult {
  const intent = stringArgument(input.arguments, "intent");
  if (intent === undefined) {
    return serializableToolError("invalid_arguments", "Argument 'intent' is required.");
  }

  const configResult = getWorkflowConfigByIntent(
    input.dependencies.repositories,
    intent,
  );
  if (!configResult.ok) {
    return toolErrorFromAppError(configResult.error, {
      code: "workflow_not_found",
      message: `No workflow is registered for intent "${intent}".`,
    });
  }

  const config = configResult.value;
  return {
    ok: true,
    intent: config.intent,
    ...(config.metadata?.title !== undefined ? { title: config.metadata.title } : {}),
    ...(config.metadata?.description !== undefined
      ? { description: config.metadata.description }
      : {}),
    subjectType: config.subjectType,
    selfServiceStart: config.selfServiceStart,
    startActors: Array.isArray(config.startActors)
      ? config.startActors.map(String)
      : [],
    declaredInputs: summariseDeclaredInputs(config),
    states: summariseWorkflowStates(config),
    approvers:
      config.approval.assigneeRole.length > 0
        ? [
            {
              assigneeRole: config.approval.assigneeRole,
              approvalType: config.approval.approvalType,
            },
          ]
        : [],
  };
}

function summariseDeclaredInputs(
  config: WorkflowConfig,
): Array<{ name: string; path: string; source: string }> {
  const summary: Array<{ name: string; path: string; source: string }> = [];
  for (const [name, value] of Object.entries(config.submit.proposedSnapshot ?? {})) {
    if (typeof value === "object" && value !== null && "$source" in value) {
      const expression = value as { $source?: unknown; path?: unknown };
      summary.push({
        name,
        source: typeof expression.$source === "string" ? expression.$source : "literal",
        path: typeof expression.path === "string" ? expression.path : "",
      });
    } else {
      summary.push({ name, source: "literal", path: "" });
    }
  }
  return summary;
}

function summariseWorkflowStates(config: WorkflowConfig): Array<{
  id: string;
  actions: Array<{ transition: string; label: string; actor: string }>;
}> {
  return Object.entries(config.states).map(([stateId, stateConfig]) => ({
    id: stateId,
    actions: stateConfig.actions.map((action) => ({
      transition: action.transition,
      label: action.label,
      actor: action.actor,
    })),
  }));
}

type StartWorkflowToolResult =
  | {
      ok: true;
      workflowInstanceId: string;
      currentState: string;
      currentInteraction: Record<string, unknown>;
      nextActions: Array<Record<string, unknown>>;
    }
  | { ok: false; error: { code: string; message: string } };

async function startWorkflowToolResult(
  input: ExecuteToolCallInput,
): Promise<StartWorkflowToolResult> {
  const intent = stringArgument(input.arguments, "intent");
  const subjectId = stringArgument(input.arguments, "subjectId");
  const startInput = objectArgument(input.arguments, "input");

  if (intent === undefined) {
    return serializableToolError("invalid_arguments", "Argument 'intent' is required.");
  }
  if (subjectId === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'subjectId' is required.",
    );
  }
  if (startInput === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'input' must be an object.",
    );
  }

  // Resolve the subject's expected subjectType from the workflow config so the
  // underlying startWorkflowIntent service sees the shape it expects.
  const configResult = getWorkflowConfigByIntent(
    input.dependencies.repositories,
    intent,
  );
  if (!configResult.ok) {
    return toolErrorFromAppError(configResult.error, {
      code: "workflow_not_found",
      message: `No workflow is registered for intent "${intent}".`,
    });
  }

  const idempotencyKey = sha1IdempotencyKey("ai-chat-start", [
    intent,
    input.requestContext.actor.actorId,
    subjectId,
    startInput,
  ]);

  const startResult = startWorkflowIntent(input.dependencies, input.requestContext, {
    intent,
    subjectId,
    subjectType: configResult.value.subjectType,
    input: startInput,
    idempotencyKey,
  });
  if (!startResult.ok) {
    return toolErrorFromAppError(startResult.error, {
      code: "start_failed",
      message: startResult.error.safeMessage,
    });
  }

  const value = startResult.value as Record<string, unknown>;
  const workflowInstanceId =
    typeof value["workflowInstanceId"] === "string"
      ? (value["workflowInstanceId"] as string)
      : "";
  const currentState =
    typeof value["state"] === "string" ? (value["state"] as string) : "";
  const currentInteraction =
    typeof value["currentInteraction"] === "object" &&
    value["currentInteraction"] !== null
      ? (value["currentInteraction"] as Record<string, unknown>)
      : {};

  const actionsResult = getAvailableActions(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
  );
  const nextActions: Array<Record<string, unknown>> =
    actionsResult.ok &&
    Array.isArray((actionsResult.value as Record<string, unknown>)["actions"])
      ? ((actionsResult.value as Record<string, unknown>)["actions"] as Array<
          Record<string, unknown>
        >)
      : [];

  return {
    ok: true,
    workflowInstanceId,
    currentState,
    currentInteraction,
    nextActions,
  };
}

type WorkflowInstanceSummary = {
  id: string;
  intent: string;
  subjectId: string;
  subjectDisplayName?: string;
  state: string;
  status: string;
  nextActionLabel?: string;
  createdAt: string;
  dueAt?: string;
};

type ListWorkflowInstancesToolResult =
  | { ok: true; instances: WorkflowInstanceSummary[] }
  | { ok: false; error: { code: string; message: string } };

function listWorkflowInstancesToolResult(
  input: ExecuteToolCallInput,
): ListWorkflowInstancesToolResult {
  const assignedToMe = booleanArgument(input.arguments, "assignedToMe") ?? false;
  const state = stringArgument(input.arguments, "state");
  const intent = stringArgument(input.arguments, "intent");

  if (assignedToMe) {
    const tasksResult = getTasks(input.dependencies, input.requestContext);
    if (!tasksResult.ok) {
      return toolErrorFromAppError(tasksResult.error, {
        code: "tasks_failed",
        message: "Could not load tasks.",
      });
    }
    const tasks = ((tasksResult.value as Record<string, unknown>)["tasks"] ??
      []) as Array<Record<string, unknown>>;

    return {
      ok: true,
      instances: tasks
        .map((task) => taskToInstanceSummary(input, task))
        .filter((entry): entry is WorkflowInstanceSummary => entry !== undefined)
        .filter((entry) => state === undefined || entry.state === state)
        .filter((entry) => intent === undefined || entry.intent === intent),
    };
  }

  const filters: { intent?: string; state?: string } = {};
  if (intent !== undefined) {
    filters.intent = intent;
  }
  if (state !== undefined) {
    filters.state = state;
  }

  const listResult = listWorkflowInstances(
    input.dependencies,
    input.requestContext,
    filters,
  );
  if (!listResult.ok) {
    return toolErrorFromAppError(listResult.error, {
      code: "list_failed",
      message: "Could not list workflows.",
    });
  }

  const instances = ((listResult.value as Record<string, unknown>)["instances"] ??
    []) as Array<Record<string, unknown>>;

  return {
    ok: true,
    instances: instances.map((instance) => workflowInstanceToSummary(input, instance)),
  };
}

function taskToInstanceSummary(
  input: ExecuteToolCallInput,
  task: Record<string, unknown>,
): WorkflowInstanceSummary | undefined {
  const workflowInstanceId =
    typeof task["workflowInstanceId"] === "string"
      ? (task["workflowInstanceId"] as string)
      : undefined;
  if (workflowInstanceId === undefined) {
    return undefined;
  }
  const instanceResult = getWorkflowInstance(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
  );
  if (!instanceResult.ok) {
    return undefined;
  }
  const summary = workflowInstanceToSummary(input, instanceResult.value);
  const dueAt = task["dueAt"];
  if (typeof dueAt === "string" && summary.dueAt === undefined) {
    summary.dueAt = dueAt;
  }
  return summary;
}

function workflowInstanceToSummary(
  input: ExecuteToolCallInput,
  instance: Record<string, unknown>,
): WorkflowInstanceSummary {
  const subjectId =
    typeof instance["subjectId"] === "string" ? (instance["subjectId"] as string) : "";
  const displayName = lookupSubjectDisplayName(input, subjectId);
  const summary: WorkflowInstanceSummary = {
    id:
      typeof instance["workflowInstanceId"] === "string"
        ? (instance["workflowInstanceId"] as string)
        : "",
    intent:
      typeof instance["intent"] === "string" ? (instance["intent"] as string) : "",
    subjectId,
    state: typeof instance["state"] === "string" ? (instance["state"] as string) : "",
    status:
      typeof instance["status"] === "string" ? (instance["status"] as string) : "",
    createdAt:
      typeof instance["startedAt"] === "string"
        ? (instance["startedAt"] as string)
        : "",
  };
  if (displayName !== undefined) {
    summary.subjectDisplayName = displayName;
  }
  return summary;
}

type GetWorkflowInstanceToolResult =
  | {
      ok: true;
      id: string;
      intent: string;
      subjectId: string;
      subjectDisplayName?: string;
      state: string;
      status: string;
      version: number;
      currentInteraction: Record<string, unknown>;
    }
  | { ok: false; error: { code: string; message: string } };

function getWorkflowInstanceToolResult(
  input: ExecuteToolCallInput,
): GetWorkflowInstanceToolResult {
  const workflowInstanceId = stringArgument(input.arguments, "workflowInstanceId");
  if (workflowInstanceId === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'workflowInstanceId' is required.",
    );
  }

  const instanceResult = getWorkflowInstance(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
  );
  if (!instanceResult.ok) {
    return toolErrorFromAppError(instanceResult.error, {
      code: "not_found",
      message: `Workflow instance ${workflowInstanceId} not found.`,
    });
  }

  const value = instanceResult.value as Record<string, unknown>;
  const subjectId =
    typeof value["subjectId"] === "string" ? (value["subjectId"] as string) : "";
  const subjectDisplayName = lookupSubjectDisplayName(input, subjectId);

  const summary: GetWorkflowInstanceToolResult = {
    ok: true,
    id:
      typeof value["workflowInstanceId"] === "string"
        ? (value["workflowInstanceId"] as string)
        : "",
    intent: typeof value["intent"] === "string" ? (value["intent"] as string) : "",
    subjectId,
    state: typeof value["state"] === "string" ? (value["state"] as string) : "",
    status: typeof value["status"] === "string" ? (value["status"] as string) : "",
    version: typeof value["version"] === "number" ? (value["version"] as number) : 0,
    currentInteraction:
      typeof value["currentInteraction"] === "object" &&
      value["currentInteraction"] !== null
        ? (value["currentInteraction"] as Record<string, unknown>)
        : {},
  };
  if (subjectDisplayName !== undefined) {
    summary.subjectDisplayName = subjectDisplayName;
  }
  return summary;
}

type TimelineEntry = {
  eventType: string;
  summary: string;
  actorId: string;
  occurredAt: string;
};

type GetWorkflowTimelineToolResult =
  | {
      ok: true;
      entries: TimelineEntry[];
      truncated?: boolean;
    }
  | { ok: false; error: { code: string; message: string } };

function getWorkflowTimelineToolResult(
  input: ExecuteToolCallInput,
): GetWorkflowTimelineToolResult {
  const workflowInstanceId = stringArgument(input.arguments, "workflowInstanceId");
  if (workflowInstanceId === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'workflowInstanceId' is required.",
    );
  }

  const timelineResult = getTimeline(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
    "business",
  );
  if (!timelineResult.ok) {
    return toolErrorFromAppError(timelineResult.error, {
      code: "timeline_failed",
      message: "Could not load timeline.",
    });
  }

  const value = timelineResult.value as Record<string, unknown>;
  const rawEvents = Array.isArray(value["events"])
    ? (value["events"] as Array<Record<string, unknown>>)
    : [];
  const truncated = rawEvents.length > WORKFLOW_TIMELINE_MAX_ENTRIES;
  const recent = truncated
    ? rawEvents.slice(-WORKFLOW_TIMELINE_MAX_ENTRIES)
    : rawEvents;

  const entries: TimelineEntry[] = recent.map((event) => ({
    eventType:
      typeof event["eventType"] === "string" ? (event["eventType"] as string) : "",
    summary:
      typeof event["summary"] === "string"
        ? (event["summary"] as string)
        : typeof event["eventType"] === "string"
          ? (event["eventType"] as string)
          : "",
    actorId: typeof event["actorId"] === "string" ? (event["actorId"] as string) : "",
    occurredAt:
      typeof event["occurredAt"] === "string" ? (event["occurredAt"] as string) : "",
  }));

  return {
    ok: true,
    entries,
    ...(truncated ? { truncated: true } : {}),
  };
}

type AvailableAction = {
  transition: string;
  label: string;
  requiresReason: boolean;
  target?: string;
};

type GetAvailableActionsToolResult =
  | { ok: true; actions: AvailableAction[] }
  | { ok: false; error: { code: string; message: string } };

function getAvailableActionsToolResult(
  input: ExecuteToolCallInput,
): GetAvailableActionsToolResult {
  const workflowInstanceId = stringArgument(input.arguments, "workflowInstanceId");
  if (workflowInstanceId === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'workflowInstanceId' is required.",
    );
  }

  const actionsResult = getAvailableActions(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
  );
  if (!actionsResult.ok) {
    return toolErrorFromAppError(actionsResult.error, {
      code: "actions_failed",
      message: "Could not load available actions.",
    });
  }

  const value = actionsResult.value as Record<string, unknown>;
  const rawActions = Array.isArray(value["actions"])
    ? (value["actions"] as Array<Record<string, unknown>>)
    : [];

  return {
    ok: true,
    actions: rawActions.map((action) => {
      const transition =
        typeof action["transition"] === "string"
          ? (action["transition"] as string)
          : "";
      const summary: AvailableAction = {
        transition,
        label: typeof action["label"] === "string" ? (action["label"] as string) : "",
        requiresReason: transition === "reject" || transition === "request_more_info",
      };
      if (typeof action["nextState"] === "string") {
        summary.target = action["nextState"] as string;
      }
      return summary;
    }),
  };
}

type TransitionWorkflowToolResult =
  | {
      ok: true;
      workflowInstanceId: string;
      state: string;
      status: string;
      currentInteraction: Record<string, unknown>;
      nextActions: Array<Record<string, unknown>>;
    }
  | { ok: false; error: { code: string; message: string } };

async function transitionWorkflowToolResult(
  input: ExecuteToolCallInput,
): Promise<TransitionWorkflowToolResult> {
  const workflowInstanceId = stringArgument(input.arguments, "workflowInstanceId");
  const transition = stringArgument(input.arguments, "transition");
  const body = objectArgument(input.arguments, "body") ?? {};

  if (workflowInstanceId === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'workflowInstanceId' is required.",
    );
  }
  if (transition === undefined) {
    return serializableToolError(
      "invalid_arguments",
      "Argument 'transition' is required.",
    );
  }

  const instanceResult = getWorkflowInstance(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
  );
  if (!instanceResult.ok) {
    return toolErrorFromAppError(instanceResult.error, {
      code: "not_found",
      message: `Workflow instance ${workflowInstanceId} not found.`,
    });
  }

  const instanceValue = instanceResult.value as Record<string, unknown>;
  const expectedVersion =
    typeof instanceValue["version"] === "number"
      ? (instanceValue["version"] as number)
      : 0;

  const idempotencyKey = sha1IdempotencyKey("ai-chat-transition", [
    workflowInstanceId,
    transition,
    input.requestContext.actor.actorId,
    body,
  ]);

  const transitionResult = await transitionWorkflow(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
    {
      transition,
      input: body,
      idempotencyKey,
      expectedVersion,
    },
  );
  if (!transitionResult.ok) {
    return toolErrorFromAppError(transitionResult.error, {
      code: "transition_failed",
      message: transitionResult.error.safeMessage,
    });
  }

  const value = transitionResult.value as Record<string, unknown>;
  const newState = typeof value["state"] === "string" ? (value["state"] as string) : "";
  const newStatus =
    typeof value["status"] === "string" ? (value["status"] as string) : "";
  const currentInteraction =
    typeof value["currentInteraction"] === "object" &&
    value["currentInteraction"] !== null
      ? (value["currentInteraction"] as Record<string, unknown>)
      : {};

  const actionsResult = getAvailableActions(
    input.dependencies,
    input.requestContext,
    workflowInstanceId,
  );
  const nextActions: Array<Record<string, unknown>> =
    actionsResult.ok &&
    Array.isArray((actionsResult.value as Record<string, unknown>)["actions"])
      ? ((actionsResult.value as Record<string, unknown>)["actions"] as Array<
          Record<string, unknown>
        >)
      : [];

  return {
    ok: true,
    workflowInstanceId,
    state: newState,
    status: newStatus,
    currentInteraction,
    nextActions,
  };
}

type TaskSummary = {
  id: string;
  workflowInstanceId: string;
  subjectId: string;
  subjectDisplayName?: string;
  intent: string;
  taskType: string;
  dueAt?: string;
};

type GetMyTasksToolResult =
  | { ok: true; tasks: TaskSummary[] }
  | { ok: false; error: { code: string; message: string } };

function getMyTasksToolResult(input: ExecuteToolCallInput): GetMyTasksToolResult {
  const tasksResult = getTasks(input.dependencies, input.requestContext);
  if (!tasksResult.ok) {
    return toolErrorFromAppError(tasksResult.error, {
      code: "tasks_failed",
      message: "Could not load tasks.",
    });
  }

  const rawTasks = ((tasksResult.value as Record<string, unknown>)["tasks"] ??
    []) as Array<Record<string, unknown>>;

  return {
    ok: true,
    tasks: rawTasks.map((task) => {
      const workflowInstanceId =
        typeof task["workflowInstanceId"] === "string"
          ? (task["workflowInstanceId"] as string)
          : "";
      // We need the subjectId from the workflow instance; the approval task
      // record does not store it directly. Look it up best-effort.
      const subjectInfo = subjectFromInstance(input, workflowInstanceId);
      const summary: TaskSummary = {
        id:
          typeof task["approvalTaskId"] === "string"
            ? (task["approvalTaskId"] as string)
            : "",
        workflowInstanceId,
        subjectId: subjectInfo?.subjectId ?? "",
        intent: subjectInfo?.intent ?? "",
        taskType:
          typeof task["approvalType"] === "string"
            ? (task["approvalType"] as string)
            : "",
      };
      if (subjectInfo?.subjectDisplayName !== undefined) {
        summary.subjectDisplayName = subjectInfo.subjectDisplayName;
      }
      if (typeof task["dueAt"] === "string") {
        summary.dueAt = task["dueAt"] as string;
      }
      return summary;
    }),
  };
}

function subjectFromInstance(
  input: ExecuteToolCallInput,
  workflowInstanceId: string,
): { subjectId: string; intent: string; subjectDisplayName?: string } | undefined {
  if (workflowInstanceId.length === 0) {
    return undefined;
  }
  const instanceResult =
    input.dependencies.repositories.workflows.findInstanceById(workflowInstanceId);
  if (!instanceResult.ok) {
    return undefined;
  }
  const subjectId = instanceResult.value.subjectId;
  const displayName = lookupSubjectDisplayName(input, subjectId);
  return {
    subjectId,
    intent: instanceResult.value.intent,
    ...(displayName !== undefined ? { subjectDisplayName: displayName } : {}),
  };
}
