# HTTP and gRPC Endpoint Contract

## Purpose

This specification defines how Human Capital Management Suite publishes typed service endpoints
without creating a second HTTP business model. Protobuf/gRPC is canonical.
grpcbridge projects approved methods into HTTP, and both transports invoke the
same capability gateway, trusted-context construction, governance, application
service and evidence path.

```text
gRPC client --------------------------┐
                                      ├─> trusted interceptors
HTTP/browser -> grpcbridge projection ┘       -> CapabilityGateway
                                                    -> BusinessIntent / owner
```

An endpoint is incomplete until its gRPC and HTTP forms, authorization,
idempotency, consistency, errors, evidence, compatibility and tests are present
in the generated endpoint manifest.

## Non-goals

- HTTP handlers do not own HCM rules, persistence or provider calls.
- There is no generic JSON patch endpoint for Worker, Employment, Compensation,
  Workflow or any other authoritative aggregate.
- A REST-shaped route does not imply CRUD semantics when the business operation
  is a proposal, decision, correction, cancellation, repair or other intent.
- gRPC reflection and HTTP discovery never reveal tenant-specific unavailable
  capabilities to an unauthorized caller.

## Endpoint manifest

Every public method has one versioned `EndpointDefinition` containing:

```text
endpoint_id
service_full_name / method_name
capability_definition_ref
intent_behavior: CREATES | CONSUMES | EMITS | OBSERVES | NON_MATERIAL
accepted_intent_definition_refs[]
request_type / response_type / error_details[]
grpc_exposure
http_method / path_template / body_binding
authn_assurance / authz_action / purpose_policy
tenant and organization scope derivation
classification and field-policy refs
idempotency class and key source
expected revision / ETag policy
deadline / retry / hedging policy
pagination / field-mask / ordering policy
rate and resource budget
evidence and audit policy
compatibility status / owner / phase
```

The manifest is generated from reviewed Protobuf, capability and BusinessIntent
metadata. Handwritten route tables cannot publish production methods.

## Trusted request boundary

Caller-controlled messages may contain desired business inputs, resource names,
expected revisions and a client request identifier. They cannot select trusted:

```text
PrincipalContext
TenantContext
OrganizationScope
Purpose authorization
Session assurance
Legal/Policy context
Source authority
Processing/placement context
```

Interceptors construct these values from authenticated transport and server-side
resolution. A similarly named body, query, header or metadata field is rejected
or ignored according to the endpoint definition; it never overrides trusted
context.

## Common wire and HTTP behavior

- Resource IDs are opaque canonical IDs. Display names are not route keys.
- HTTP paths are versioned under `/v1`; semantic versions remain in definition
  and capability references rather than proliferating ad hoc URL versions.
- HTTP `Idempotency-Key` maps to the canonical request id. If both header and
  message value are present they must match exactly.
- Revision-bearing resources return `ETag`; write/decision methods bind the exact
  expected revision through the request and, where exposed, `If-Match`.
- List endpoints use bounded page size, stable ordering and an opaque signed
  cursor bound to principal, tenant, scope, filters, snapshot/watermark and
  expiry. Offset pagination is not used for mutable authoritative collections.
- Field masks select only authorized response fields. A mask never grants access,
  reveals existence or bypasses classification/redaction.
- Unknown JSON fields, duplicate keys, ambiguous numbers, invalid UTF-8, unknown
  enum values and material unknown Protobuf fields follow the canonical strict
  validation/compatibility policy.
- Every request has a server-capped deadline and resource budget. Retry/hedging
  follows the method's effect/idempotency classification.
- Success, typed business rejection and transport failure remain distinct.

## Canonical error projection

The owned error detail is canonical; transport codes are projections.

| Owned condition                                                    | gRPC                         | HTTP                          |
| ------------------------------------------------------------------ | ---------------------------- | ----------------------------- |
| malformed or structurally invalid request                          | `INVALID_ARGUMENT`           | `400`                         |
| missing/invalid authentication                                     | `UNAUTHENTICATED`            | `401`                         |
| known resource but prohibited action, when disclosure is permitted | `PERMISSION_DENIED`          | `403`                         |
| hidden or absent resource                                          | `NOT_FOUND`                  | `404`                         |
| idempotency payload mismatch or current-state conflict             | `ALREADY_EXISTS` / `ABORTED` | `409`                         |
| stale revision or unmet business precondition                      | `FAILED_PRECONDITION`        | `412`                         |
| quota or admission exhaustion                                      | `RESOURCE_EXHAUSTED`         | `429`                         |
| caller deadline exceeded                                           | `DEADLINE_EXCEEDED`          | `504`                         |
| dependency unavailable before a committed result is known          | `UNAVAILABLE`                | `503`                         |
| accepted long-running work                                         | typed Operation response     | `202` plus operation resource |

Error bodies include stable code, safe field violations, retry classification,
current revision only when authorized, correlation/evidence references and no
raw stack, SQL, policy source, secret or restricted value.

## Initial endpoint inventory

### Registry and discovery

| gRPC method                             | HTTP projection                                  | Behavior                                    |
| --------------------------------------- | ------------------------------------------------ | ------------------------------------------- |
| `RegistryService.ListIntentDefinitions` | `GET /v1/intent-definitions`                     | authorized, phase/tenant-filtered discovery |
| `RegistryService.GetIntentDefinition`   | `GET /v1/intent-definitions/{intent_definition}` | one authorized immutable definition         |
| `RegistryService.ListCapabilities`      | `GET /v1/capabilities`                           | authorized semantic action discovery        |
| `RegistryService.GetCapability`         | `GET /v1/capabilities/{capability}`              | one immutable capability descriptor         |

### BusinessIntent lifecycle

| gRPC method                        | HTTP projection                        | Behavior                                                 |
| ---------------------------------- | -------------------------------------- | -------------------------------------------------------- |
| `IntentService.CreateIntent`       | `POST /v1/intents`                     | create one typed draft/root or child intent idempotently |
| `IntentService.GetIntent`          | `GET /v1/intents/{intent}`             | current multidimensional lifecycle projection            |
| `IntentService.ListIntents`        | `GET /v1/intents`                      | authorized stable snapshot pagination                    |
| `IntentService.SimulateIntent`     | `POST /v1/intents/{intent}:simulate`   | immutable no-effect simulation                           |
| `IntentService.SubmitIntent`       | `POST /v1/intents/{intent}:submit`     | bind and submit exact proposal revision                  |
| `IntentService.CancelIntent`       | `POST /v1/intents/{intent}:cancel`     | request governed cancellation at declared boundary       |
| `IntentService.SupersedeIntent`    | `POST /v1/intents/{intent}:supersede`  | create/link successor rather than mutate history         |
| `IntentService.ExplainIntent`      | `GET /v1/intents/{intent}/explanation` | purpose-scoped decision/evidence explanation             |
| `IntentService.ListIntentTimeline` | `GET /v1/intents/{intent}/timeline`    | stable, redacted business chronology                     |

### Gate B Promotion slice

| gRPC method                              | HTTP projection                             | Behavior                                                                                         |
| ---------------------------------------- | ------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `PromotionService.ProposeIntoManagement` | `POST /v1/promotions:proposeIntoManagement` | typed `PromoteWorker` proposal entry point; resolves server truth and creates one BusinessIntent |

This endpoint is a typed semantic façade over the same capability and intent
kernel. It is not an alternate promotion implementation and does not accept
caller-supplied current salary, manager, budget, authority or position vacancy.

### Human work and approvals

| gRPC method                    | HTTP projection                                  | Behavior                                              |
| ------------------------------ | ------------------------------------------------ | ----------------------------------------------------- |
| `WorkService.ListWorkItems`    | `GET /v1/work-items`                             | authorized queue snapshot                             |
| `WorkService.GetWorkItem`      | `GET /v1/work-items/{work_item}`                 | redacted task detail/current version                  |
| `WorkService.ClaimWorkItem`    | `POST /v1/work-items/{work_item}:claim`          | atomic version-bound claim/lease                      |
| `WorkService.ReleaseWorkItem`  | `POST /v1/work-items/{work_item}:release`        | release current authorized claim                      |
| `WorkService.CompleteWorkItem` | `POST /v1/work-items/{work_item}:complete`       | typed completion bound to task/proposal version       |
| `WorkService.DecideApproval`   | `POST /v1/work-items/{work_item}:decideApproval` | approve/reject/abstain bound to exact proposal digest |

### Workflow operations

| gRPC method                          | HTTP projection                                    | Behavior                                                           |
| ------------------------------------ | -------------------------------------------------- | ------------------------------------------------------------------ |
| `WorkflowService.GetWorkflow`        | `GET /v1/workflows/{workflow}`                     | authorized runtime/business/completion dimensions                  |
| `WorkflowService.ListNodeExecutions` | `GET /v1/workflows/{workflow}/nodes`               | stable execution inspection                                        |
| `WorkflowService.PauseWorkflow`      | `POST /v1/workflows/{workflow}:pause`              | governed operational intent/capability, no direct row edit         |
| `WorkflowService.ResumeWorkflow`     | `POST /v1/workflows/{workflow}:resume`             | governed resume after revalidation                                 |
| `WorkflowService.CancelWorkflow`     | `POST /v1/workflows/{workflow}:cancel`             | cancellation boundary/irreversible-effect aware                    |
| `WorkflowService.RetryNode`          | `POST /v1/workflows/{workflow}/nodes/{node}:retry` | retry exact failed attempt under idempotency and current authority |

### Evidence and long-running operations

| gRPC method                            | HTTP projection                            | Behavior                                          |
| -------------------------------------- | ------------------------------------------ | ------------------------------------------------- |
| `EvidenceService.GetExecutionReceipt`  | `GET /v1/execution-receipts/{receipt}`     | authorized immutable receipt                      |
| `EvidenceService.ExportIntentEvidence` | `POST /v1/intents/{intent}:exportEvidence` | starts a governed resumable export operation      |
| `OperationsService.GetOperation`       | `GET /v1/operations/{operation}`           | current long-running operation state/result/error |
| `OperationsService.CancelOperation`    | `POST /v1/operations/{operation}:cancel`   | cancellation request under operation policy       |

### Non-material service endpoints

| gRPC/HTTP                                              | Behavior                                                                         |
| ------------------------------------------------------ | -------------------------------------------------------------------------------- |
| standard `grpc.health.v1.Health/Check`; `GET /healthz` | process liveness only; no dependency or tenant detail                            |
| standard `grpc.health.v1.Health/Check`; `GET /readyz`  | admission readiness for the addressed role; bounded safe reasons internally only |

## Required test layers for every endpoint

Each endpoint todo supplies, as applicable:

1. Protobuf golden request/response/error vectors.
2. Direct application/capability unit test.
3. In-memory gRPC integration test with real interceptors.
4. grpcbridge HTTP parity test over the same vector.
5. Authentication, authorization, tenant, purpose and field-security tests.
6. Idempotency, stale revision, duplicate, concurrency and replay tests.
7. Deadline, cancellation, overload and dependency-fault tests.
8. Unknown-field, malformed input and fuzz tests.
9. Evidence/telemetry assertions with prohibited-data checks.
10. Compatibility test against the previous published descriptor/HTTP manifest.

Tests assert exact returned and persisted states, row/event/outbox/work/effect
counts, safe error details and evidence identifiers. HTTP status equality alone
is not sufficient transport parity.

## Expansion rule

The initial inventory proves the endpoint pattern. Subsequent domain methods are
generated from accepted CapabilityDefinition and BusinessIntent contracts. An
intent-derived endpoint coverage gate must fail for any `CONTRACTED` feature that
has no approved endpoint disposition:

```text
TYPED_PUBLIC_METHOD
GENERIC_INTENT_LIFECYCLE_ONLY
INTERNAL_CAPABILITY_ONLY
EVENT_OR_SCHEDULE_ONLY
NO_ENDPOINT_WITH_JUSTIFICATION
```

No endpoint is inferred merely from a database entity or model object.
