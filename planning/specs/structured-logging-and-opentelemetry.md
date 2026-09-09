# Structured Logging and OpenTelemetry

## Scope

This specification owns operational logs, traces, metrics, propagation and
telemetry export for every Human Capital Management Suite process. It applies to HTTP/gRPC requests,
capability invocation, BusinessIntent creation, workflows, transactions,
projectors, schedulers, jobs, integrations, messaging, agents and operator
actions.

Telemetry diagnoses software. It is not a business ledger, audit log, billing
source, authorization fact or proof that an external effect completed.

## Package boundary

Authored code uses owned contracts under `internal/operations/telemetry`.
`log/slog` supplies structured logging mechanics. OpenTelemetry APIs, SDKs and
OTLP exporters remain behind adapters in operations, transport and worker
composition roots. Domain entities, Protobuf contracts, ledger events and owner
ports do not expose `slog` or OpenTelemetry types.

```text
business/domain code
        |
        v
owned Telemetry / Logger contracts
        |
        +-- slog handler pipeline
        +-- OpenTelemetry trace/metric/log adapters
        +-- privacy/cardinality policy
        +-- bounded exporters/Collector
```

## Structured log envelope

Every emitted record has a stable schema version and these required fields:

```text
timestamp
observed_timestamp
severity_number
severity_text
event_name
event_version
message_template_id
service.name
service.version
service.instance.id
deployment.environment
cell.id
region
process.role
build.digest
trace_id?
span_id?
trace_flags?
correlation_id
request_id?
tenant_scope_token?
intent_id?
workflow_instance_id?
node_execution_id?
transaction_id?
operation_id?
attempt_id?
actor_kind?
error.type?
error.code?
error.retryable?
duration_ms?
outcome
telemetry_policy_version
```

Identifiers are present only where policy permits. Tenant scope uses a bounded,
non-reversible operational token rather than a customer name. Person, worker,
candidate, compensation, bank, medical, case, document, prompt and arbitrary
request/response values are prohibited by default. Error strings, SQL, URLs,
headers and stack traces pass through the same classifier; they are not trusted
merely because a library produced them.

`event_name` is a stable dotted identifier such as
`workflow.node.completed`. Free text is diagnostic presentation and cannot be
used as a query, alert or SLI contract. Dynamic values are structured fields,
never interpolated into the message template.

## Severity and outcome

```text
DEBUG  bounded development or time-limited diagnostic detail
INFO   normal material operational transition
WARN   degraded, retried, partial or unexpected recoverable state
ERROR  operation failed or correctness cannot be established
```

Fatal process termination is expressed as an `ERROR` record followed by the
owned shutdown path. Libraries do not terminate the process. A panic boundary
records a classified error and stack reference, marks the active span as error,
flushes within a bound and returns the transport/worker-owned failure behavior.

`outcome` is typed: `SUCCESS`, `FAILURE`, `PARTIAL`, `UNKNOWN`, `DENIED`,
`CANCELLED` or `DEGRADED`. A retry attempt and its logical operation have
different records and identifiers.

## Trace topology

Span names and attributes are low-cardinality and versioned. Required span
families include:

```text
HTTP/gRPC server
Capability.Invoke
Intent.Create / Intent.Advance
Workflow.Start / Workflow.Node
Transaction.Prepare / Transaction.Commit
DB operation
Outbox.Publish / Queue.Deliver
Connector.Dispatch / Provider.Call
Observation / Reconciliation / Repair
Projector.Apply / Job.Partition
```

Synchronous calls use parent/child context. Durable or fan-out boundaries use
persisted causal metadata and span links: workflow continuation, timer wake,
signal receipt, outbox delivery, queue redelivery, batch partition, connector
attempt and repair execution must not pretend to be one permanently open span.
Retries create attempt spans linked to one logical-operation identity.

Span status is set only for the span's operation. A business outcome such as
`ConsistencyState=DEGRADED` is an attribute/reference, not automatically a
failed HTTP span. Provider acceptance never becomes transaction completion.

## Propagation

Inbound W3C trace context is parsed under strict length/count/format limits.
Malformed context starts a new trace and emits a bounded security signal; it
does not fail the business request unless an independent security policy says
so. Untrusted callers cannot select tenant, actor, purpose, authorization,
business correlation or evidence identifiers through trace headers.

Baggage is denied by default. An allowlist defines key, size, destination,
classification and hop lifetime. No identity, authorization, tenant name,
personal data or business payload may arrive through baggage. Egress propagates
only reviewed context to approved destinations.

Business `correlation_id` and `causation_id` are stable owned identifiers. They
may correlate several traces across a long-running workflow. `trace_id` is
operational and must never replace them in stored business state.

## Metrics and exemplars

Metric names, units, descriptions, aggregation and label sets are immutable
versioned definitions. Labels are bounded enumerations or controlled resource
attributes. Per-worker, per-request, per-intent and arbitrary error strings are
forbidden labels. Controlled exemplars may link a metric observation to a
retained trace after privacy and sampling evaluation.

Histograms declare buckets appropriate to the operation. Counters define exact
valid/total semantics. Observable gauges define staleness and missing-source
behavior. Missing telemetry yields `UNKNOWN`, never zero or healthy.

## Sampling

Head and tail sampling are policy-driven and versioned. Sampling decisions
consider operation risk and outcome. Declared security denials, cross-tenant
attempts, financial mutations, irreversible effects, ambiguous results,
correctness failures and telemetry-pipeline failures retain the required trace
evidence. Success sampling remains bounded by cost and representativeness.

Sampling does not affect business persistence, ledger evidence or response
semantics. Parent sampling flags from an untrusted caller cannot force expensive
retention or disable required risk-tail retention.

## Export, failure and shutdown

Application exporters use bounded memory, bounded retry and non-blocking normal
business paths. Privacy-gateway failure fails telemetry export closed but does
not roll back an already valid business commit. Drops, queue saturation,
redaction rejection, exporter failure and Collector/backend lag are measured by
an independent health path and make affected assurance `UNKNOWN` or `DEGRADED`.

Shutdown order is:

```text
stop admission
drain accepted work to safe points
stop telemetry producers
flush within configured deadline
record dropped/unfinished counts through the independent health path
close exporters
exit
```

Crashes cannot promise lossless telemetry. Correctness evidence therefore lives
in authoritative stores rather than relying on a final log record.

## Diagnostic controls

Runtime log-level changes require a governed, scoped, expiring configuration
revision with actor, purpose and evidence. Sensitive content remains prohibited
at every level. Per-tenant or per-correlation diagnostic elevation has bounded
cardinality, duration and volume and cannot alter sampling requirements,
authorization or business behavior.

Telemetry query access is separately authorized, purpose-bound and logged.
Retention, residency, deletion, legal hold, restore and tenant-exit policies are
declared per signal/backend. Cross-tenant dashboards, searches and exemplars are
prohibited without a separately authorized aggregate contract.

## Testing contract

The test harness supplies deterministic clocks and IDs plus in-memory log,
trace and metric exporters. Tests assert exact records/spans/links/attributes,
absence of prohibited data, propagation across process boundaries, bounded
cardinality, sampling outcomes, exporter failure behavior and shutdown drops.

Required negative fixtures include salary, bank, medical, case, document,
prompt, authorization header, SQL value, malicious trace headers, oversized
baggage, invalid UTF-8, cyclic errors, panics, exporter stalls, clock skew,
queue redelivery and multi-tenant concurrency.

CI rejects unknown event names, attributes or metric labels; dynamic message
templates; missing schema versions; duplicate semantic definitions; domain
imports of backend types; and instrumentation that changes a business return,
commit, ordering or retry decision.

## Phase depth

Phase 1 implements the owned envelope, `slog` handler, context propagation,
critical HTTP/gRPC/workflow/transaction/database/integration spans, core metrics,
privacy policy, bounded OTLP export and deterministic test harness. Gate A adds
production Collectors, backends, risk-aware sampling, diagnostic controls and
pipeline SLOs. Later phases extend coverage without changing the semantic
contracts.
