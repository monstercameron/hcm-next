// Package replay is WF-RUN-013's deterministic REPLAY mode: it re-executes a
// compiled workflow definition from one instance's durable record and proves,
// byte for byte, that the recorded run is the run the current code still
// produces.
//
// # What replay is, and what it deliberately is not
//
// A replay is not a second execution of the same intent. It is a re-derivation
// of a historical one, and every difference between the two is structural
// rather than a flag a caller sets:
//
//   - Every node input comes from the record. A node's outcome, its route key
//     and its typed output digest are read out of [Record.Nodes]; a signal's
//     payload digest out of [Record.Signals]; a timer's settlement out of
//     [Record.Timers]. Nothing is recomputed, so a capability whose answer has
//     changed since the run cannot silently change the replay.
//   - Time and randomness come from the record. This package reads no wall
//     clock and draws no random value; [Recorder.Now] and [Recorder.Draw]
//     answer from the recorded material or refuse. TestReplayReadsNoWallClock
//     checks the source of this package for that property rather than trusting
//     it.
//   - Adapters are replaced by a [Recorder] that refuses. Any attempt to reach
//     a domain write, an external destination or a live approval is
//     [CodeEffectForbidden], naming the node that tried; a caller-supplied
//     [NodeAdapter] is never invoked at all, so an adapter that would have
//     reached out cannot, even by accident.
//   - The run is admitted only under the REPLAY [intent.ModeContract]. A
//     contract whose Permit would allow a domain commit or an external effect
//     is refused before any record is read, and [intent.CausalSeparation] must
//     hold: the replay names the historical intent it re-derives and is not
//     that intent.
//
// # The output
//
// [Replayer.Replay] produces a [Trace]: one [TraceEntry] per replayed node, in
// recorded order, each carrying the [frontier.Transition] digest that
// advancement produced. The trace has a content digest of its own, and when
// the record pins [Record.TraceDigest] the replay must reproduce it exactly --
// a mismatch is [CodeDivergence], not a warning.
//
// When the replay and the record disagree, the disagreement is a value, not a
// panic and not a message: [Divergence] names the first differing node, the
// field that differed and both sides of it. A record that is simply missing an
// artifact the frontier reached is the same shape of answer, additionally
// carrying [CodeArtifactUnavailable] -- WF-RUN-013's GREEN clause spells that
// code, and its FAULT clause asks for a divergence rather than a crash, so the
// call returns both.
//
// # Where the record comes from
//
// [Source] is the port. [MemorySource] is a record assembled in process --
// what most tests and any tool holding an exported run use. [StoreSource]
// assembles the same shape from the durable tables DB-012 materialized, read
// through internal/workflow/runtime's own [runtime.Store] for the instance and
// its node executions and through the workflow_continuation ledger for the
// route keys those executions took, plus internal/data/runtimestate for the
// frontier entries, timers, signals and checkpoints when they are present.
//
// Nothing in this package writes. A replay that could write would not be a
// replay.
package replay
