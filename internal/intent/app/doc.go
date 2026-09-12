// Package app is the application/composition layer of the BusinessIntent
// kernel: it turns generated Protobuf transport requests into kernel calls and
// back, and it composes one whole P1A cell, including the published
// gRPC/Connect listeners ([Cell.GRPCServer], [Cell.EdgeHandler]).
//
// Semantic owner: intent-and-capability. Phase: P1A. Todos: NEXT-004, NEXT-005.
//
// What lives here and what does not:
//
//   - [IntentService] implements transport.IntentHandler and
//     transport.RegistryHandler. It owns the translation
//     (protomap), the ordering (draft, preflight, propose, compile), the
//     lifecycle-transition endpoints (EP-INTENT-003) and the remaining P1A
//     refusals (ExecuteIntent, absent an [ExecutionAuthority] gate). It owns
//     no domain rule and no SQL.
//   - Every domain answer is reached through the governed capability gateway
//     (internal/capability), so a P1A invocation cannot happen without
//     evidence and cannot happen at all for a write-effect capability.
//   - Persistence is a port. [Store] is the only way this package reaches a
//     database; the PostgreSQL implementation lives in the sibling package
//     internal/intent/app/pgstore, which the composition roots (cmd/hcmnext,
//     test/) wire in. This package imports no concrete adapter
//     (definitions/architecture/package-dependency-policy.yaml).
//   - Authorization is decided once, by internal/trust/authz under the
//     BOOTSTRAP policy, and projected onto each domain package's narrower
//     decision shape ([peopleDecision], [dataopsDecision],
//     [intelligenceDecision]). There is no second evaluator here: a policy
//     this package decided for itself would be a policy nobody published.
//   - The external system of record is reached only through
//     internal/connectivity, whose Connector interface has no method that
//     could write. Every page a comparison reads is recorded as immutable
//     observation evidence before the comparison sees a value.
//
// # The eight contracts
//
// All eight P1A intents (planning/next-steps.md, "P1A") are creatable and
// simulatable through [IntentService.SimulateIntent] on both transports:
// explain_worker_state, promote_worker (simulate only), simulate_compensation
// and evaluate_pay_band_position answer from the design-partner corpus;
// detect_drift, create_repair_plan and simulate_repair compare that corpus
// against the incumbent connector; explain_transaction reconstructs one
// recorded intent's chronology from the authoritative ledger. Every answer
// carries a zero-effect receipt minted by the domain that produced it.
//
// The HTTP edge additionally serves the API-001 discovery document at
// [DiscoveryPath], to an authenticated caller only. RegistryService declares
// no discovery method, so there is deliberately no RPC counterpart.
//
// # P1A ceiling
//
// P1A grants no domain-mutating write authority: ExecuteIntent refuses with a
// typed FAILED_PRECONDITION unless this cell is composed with an
// [ExecutionAuthority] that admits the intent's own type and the caller's
// role (still nobody, in this release). SimulateIntent persists nothing at
// all: it reads, computes and answers, and its answer carries a zero-effect
// receipt.
//
// SubmitIntent, CancelIntent and SupersedeIntent (EP-INTENT-003) are governed
// writes over the intent's own five-dimensional lifecycle state, not over
// workforce state: SubmitIntent binds the exact re-simulated proposal
// revision and starts once; CancelIntent reports CANCELLED,
// CANCELLATION_PENDING, TOO_LATE or REPAIR_REQUIRED entirely through the
// returned instance's dimensions (there is no wire enum for it); and
// SupersedeIntent creates an authorized successor and moves the original's
// RequestState to SUPERSEDED without otherwise mutating it. All three compose
// [intent.Submit], [intent.CancelInstance] and [intent.SupersedeOriginal]
// through [lifecycle.Machine]; this package never assigns a lifecycle tuple
// directly, and require a cell composed with an idempotency [Options.Idempotency]
// coordinator ([NewCell] supplies one by default) to answer at all.
//
// CreateIntent's own write is chronology, not workforce state: one ledger
// event carrying the typed envelope, one projection advance and one outbox
// message, in one transaction.
package app
