// Package app is the application/composition layer of the BusinessIntent
// kernel: it turns generated Protobuf transport requests into kernel calls and
// back, and it composes one runnable P1A cell.
//
// Semantic owner: intent-and-capability. Phase: P1A. Todos: NEXT-004, NEXT-005.
//
// What lives here and what does not:
//
//   - [IntentService] implements transport.IntentHandler and
//     transport.RegistryHandler. It owns the translation
//     (protomap), the ordering (draft, preflight, propose, compile) and the
//     P1A refusals. It owns no domain rule and no SQL.
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
// P1A grants no write authority. SubmitIntent, CancelIntent and
// SupersedeIntent are governed writes that do not exist yet, so they return
// one typed FAILED_PRECONDITION refusal rather than a partial implementation.
// SimulateIntent persists nothing at all: it reads, computes and answers, and
// its answer carries a zero-effect receipt.
//
// The single write path is CreateIntent, and what it writes is chronology, not
// workforce state: one ledger event carrying the typed envelope, one
// projection advance and one outbox message, in one transaction.
package app
