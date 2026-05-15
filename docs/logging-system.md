# Logging System

## 1. Design

All layers emit structured JSON to stdout (info/debug) or stderr (warn/error). No log files. No log framework beyond what each runtime already provides. Infrastructure (Docker, systemd, cloud) routes and aggregates the stream.

Principle:

> One log shape. One stream per process. Correlation IDs tie them together.

Three layers emit logs:

- **API** — Node/TypeScript control plane (`"service": "api"`)
- **Executor** — Go execution plane (`"service": "executor"`)
- **Frontend** — React console; browser errors only, dev-mode only

---

## 2. Unified Log Shape

Every log entry from every layer shares this envelope:

```json
{
  "timestamp": "2026-05-15T10:23:41.123Z",
  "level": "debug" | "info" | "warn" | "error",
  "service": "api" | "executor",
  "message": "...",

  "requestId": "req_abc123",
  "correlationId": "corr_xyz",
  "actorId": "actor_42",
  "tenantId": "t_1",
  "environmentId": "env_prod",

  "workflowInstanceId": "wi_99",
  "changeRequestId": "cr_77",

  "durationMs": 45,

  "details": {},

  "error": {
    "code": "WORKFLOW_NOT_FOUND",
    "message": "..."
  }
}
```

Fields are omitted when not applicable. `requestId`, `correlationId`, `actorId`, and `tenantId` are present on every log entry from the API layer — they are bound to the logger at request start and carried automatically.

---

## 3. Layer 1 — Node API

### 3.1 Logger Infrastructure

The logger is already implemented at `src/platform/foundation/logger/logger.ts`.

`createStructuredLogger(baseContext: LogContext)` creates a `StructuredLogger` pre-stamped with context fields. `LogContext` accepts the full `RequestContext` plus `workflowInstanceId` and `changeRequestId` from `WorkflowContext`.

The app logger is created once at startup with no context:

```typescript
const appLogger = createStructuredLogger({ service: "api" });
```

A request-scoped child logger is created per request by spreading the request context into a new logger. This is not a formal child API — it's a second `createStructuredLogger` call with the merged context:

```typescript
const requestLogger = createStructuredLogger({
  service: "api",
  requestId: ctx.requestId,
  correlationId: ctx.correlationId,
  actorId: ctx.actor.id,
  tenantId: ctx.tenantId,
  environmentId: ctx.environmentId,
});
```

The request logger is passed to all handlers and services for that request. Handlers do not construct their own loggers.

### 3.2 Log Level

The logger respects the `LOG_LEVEL` environment variable. Valid values: `debug`, `info`, `warn`, `error`. Default: `info`.

Entries below the configured level are dropped before writing.

`.env.example` must document this variable.

### 3.3 What the API Logs

**HTTP request lifecycle**

Logged by a thin wrapper in `server.ts` around each route handler. Not middleware — the route wrapper already exists for error handling; logging is added there.

```
→ request received
  level:   info
  message: "request received"
  fields:  method, path, requestId, correlationId, actorId

← request completed
  level:   info (2xx–3xx), warn (4xx), error (5xx)
  message: "request completed"
  fields:  method, path, status, durationMs
```

**Result boundary errors**

When a handler returns `Result.err`, the error is logged before the HTTP response is sent. Internal details (the `AppError` code and any detail fields) go to the log. Only the safe user message goes to the client.

```
  level:   warn (expected failures), error (unexpected/system failures)
  message: "request failed"
  fields:  errorCode, errorDetails (internal only — never user-facing message)
```

**Workflow transitions**

Logged by the workflow runtime at transition boundaries.

```
workflow transition started
  fields:  workflowInstanceId, changeRequestId, transitionType, currentNodeId, expectedVersion

workflow transition completed
  fields:  workflowInstanceId, changeRequestId, nextNodeId, durationMs

workflow transition failed
  level:   warn
  fields:  workflowInstanceId, changeRequestId, reason, errorCode
```

---

## 4. Layer 2 — Go Executor

### 4.1 Process-Level Logger

The executor already uses `log/slog` with a JSON handler writing to stdout:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
```

This is correct. No change needed at the process level.

`"service": "executor"` is added as a default attribute on the logger at startup:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "executor")
```

### 4.2 Correlation Threading

The Node executor client (`src/api/executor-client.ts`) must forward correlation headers on every HTTP POST to the Go executor:

```
X-Request-ID: <requestId>
X-Correlation-ID: <correlationId>
```

The Go HTTP server reads these headers and binds them to a request-scoped slog logger:

```go
requestLogger := logger.With(
    "requestId",     r.Header.Get("X-Request-ID"),
    "correlationId", r.Header.Get("X-Correlation-ID"),
)
```

This logger is passed to the block handler so all executor-level logs carry the same IDs as the originating API request.

### 4.3 Block Execution Logs

Blocks return `[]ExecutionLog` in their `BlockResult`. These are included in the `ExecutionResponse` and returned to Node. Node re-emits each entry to its own logger tagged with `"service": "executor"`, stamping the current `requestId` and `correlationId` onto each entry.

This means all block execution logs flow through a single unified stdout stream on the Node side, fully correlated.

```typescript
for (const log of executorResponse.logs) {
  requestLogger.log(log.level, log.message, {
    service: "executor",
    ...log.fields,
  });
}
```

### 4.4 What the Executor Logs

The Go executor logs:

```
block execution started
  fields:  block.name, block.version, workflowInstanceId, changeRequestId, requestId, correlationId

block execution completed
  fields:  block.name, status, durationMs

block execution failed
  level:   error
  fields:  block.name, errorCode, errorMessage
```

Block-internal logs are emitted via `BlockResult.Logs` and handled by Node (see 4.3). Block handlers do not write directly to slog.

---

## 5. Layer 3 — Frontend

The React console does not send logs to the server. This is an internal tool — frontend errors surface through server-side API logs when a bad call is made.

In development, a React `ErrorBoundary` renders the error in the UI. No production client-side logging pipeline is needed at this stage.

The `no-console: "error"` ESLint rule remains enforced across all console source files.

---

## 6. Correlation Flow

A single user action produces a correlated log trail across both processes:

```
[api]      request received          requestId=req_1  correlationId=corr_A
[api]      workflow transition started                 workflowInstanceId=wi_99
[api]      executor call started     block=compensation/preflight
[executor] block execution started   requestId=req_1  correlationId=corr_A
[executor] Compensation preflight completed  (re-emitted by Node, service=executor)
[executor] block execution completed durationMs=12
[api]      workflow transition completed               durationMs=58
[api]      request completed         status=200        durationMs=61
```

All entries share `requestId` and `correlationId`. Querying either field in a log aggregator returns the full trace for that request.

---

## 7. What Is Not Logged

- Raw request or response bodies — field-level values may contain restricted HR data
- SQL query text — parameterized values may contain PII
- Secrets, tokens, or credentials — ever
- User-facing error messages on internal log lines — these are for the client, not the log

The `AppError` type separates `safeMessage` (client-facing) from `details` (internal). Only `details` and `code` appear in logs.
