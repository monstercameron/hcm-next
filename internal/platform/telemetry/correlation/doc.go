// Package correlation implements planning/todos.md OBS-016: typed helpers
// that join structured logs, traces, metrics and business identifiers
// without conflating authority.
//
// A Correlation keeps two identities apart by construction. The owned
// identity (IntentID, CorrelationID, TenantToken) is stable business
// causation: it survives sampling, outlives any one trace, and is the only
// identity that may serve as an idempotency, authorization or evidence key.
// The telemetry linkage (TraceID, SpanID, Sampled) names one sampled span
// for diagnosis and is the only identity an exemplar may carry. There is no
// constructor that writes trace identity into the owned fields and no
// accessor that reads owned authority out of the trace fields, so using a
// trace ID as business authority is not a policy violation here — it is
// inexpressible.
//
// Every signal join is authorized independently through Join: the caller
// presents a JoinScope (tenant token plus purpose) per join, and the join
// succeeds only for the correlation's own tenant. Authorization verdicts
// are never cached between joins.
package correlation
