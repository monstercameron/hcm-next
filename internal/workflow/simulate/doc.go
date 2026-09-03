// Package simulate is the in-memory SIMULATE-mode interpreter for a compiled
// workflow plan (owner: workflow-runtime; phase: P1A; CONF-001's "simulate
// mode, fixed clock, zero-effect receipt" disposition).
//
// # What this is, and what it deliberately is not
//
// [Run] walks a [workflow.CompiledWorkflow] node by node in one process, in
// one goroutine, against an injected fake clock, and hands back a [Receipt]:
// the ordered node trace, the evidence each node recorded, the five-dimension
// lifecycle state the terminal declares, the zero-effect counters, the elapsed
// virtual time and a content digest that is byte-stable across runs.
//
// It is not the durable runtime. definitions/runtime/durable-runtime-decision.yaml
// (WF-RUN-000) gates every durable primitive behind the P1B re-evaluation, so
// there is no instance table, no lease, no fence token, no timer, no retry
// across a restart and no persisted human task anywhere in this package. A
// simulation that dies with the process is the honest P1A artifact; a
// simulation that pretended to survive one would be a runtime nobody decided
// to build.
//
// # Zero effect is enforced, not asserted
//
// planning/specs/workflow-runtime.md is explicit that "Simulation suppresses
// all mutations and external effects". This package refuses rather than
// suppresses: [Run] admits a plan only when every reachable node's effect
// class is PURE or READ_ONLY and every node admits [workflow.ModeSimulate],
// and it re-checks that at the safe point in front of each node before
// dispatching it. A write-class node therefore cannot execute in SIMULATE
// mode by construction, and the governed gateway underneath refuses it a
// second time (capability.Gateway.Invoke refuses every write effect class in
// P1A). The receipt carries an [evidence.ZeroEffectReceipt] that its own
// constructor refuses to mint over a non-zero count.
//
// # Where the semantics live
//
// The interpreter owns coordination and nothing else. CAPABILITY nodes go
// through capability.Gateway.Invoke to handlers bound in [Handlers], which
// call the real domain calculations (people.ExplainWorkerState,
// rewards.SimulateCompensation, rewards.EvaluatePayBandPosition) - all of them
// already zero-effect by contract. DECISION nodes go to the rules engine.
// TRANSFORM goes to a registered pure transform, which for the promotion
// reference is promotion.PreflightPromotion plus promotion.SimulatePromotion.
// OBSERVE goes to an injected [ReadPort]. APPROVAL and TASK nodes derive
// requirements through internal/humanwork and resolve candidate approvers
// without ever waiting: a work item is recorded as WOULD_AWAIT, because a
// simulation that blocked on a human would be an execution.
//
// # Determinism
//
// Nothing here reads a wall clock, a random source, a map iteration order or
// the network. Time comes from timeauth.FakeClock, advanced by a declared
// step per node; ordering comes from the compiled plan; and every collection
// in the receipt is emitted in a fixed order. [Receipt.Digest] is the proof:
// two runs of the same plan with the same inputs produce identical bytes.
package simulate
