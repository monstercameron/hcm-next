# Devlog — 2026-09-03: from a read-only P1A cell to an executable Promotion prototype

Running log of the decisions taken and the results observed while driving the
backlog with parallel agent lanes. Written by the orchestrating session; every
result below was re-verified by the orchestrator before it was recorded
(`go test -count=1` on the named packages, windows/arm64, Go 1.26.3, branch
`plan-revision-2026-09-02`). Entries are in the order the work happened.

## 1. Starting point

- HEAD `1faafb5` had the full P1A cell wired: eight read-only intents served
  over gRPC and the connect edge, discovery, authz, the served Promotion
  workspace, the compiler and the SIMULATE interpreter. 208 todos ticked.
- Standing constraints for every lane: disjoint file scopes; agents never edit
  `go.mod`, `go.sum`, `planning/`, `definitions/`, `migrations/` they do not
  own, and never run git. The orchestrator folds their DDL into numbered
  migrations, registers tables, ticks todos and commits.
- Cap on concurrency was set by the owner at four lanes at a time, with care
  not to clobber past code.

## 2. Closing the interrupted platform lanes

A session-limit interruption had killed nine lanes mid-edit. They were resumed
from their own transcripts rather than relaunched, so no file was rewritten
blind.

| Lane                                             | Result                                                                                                                                                                                                                                                                                      | Todo                                                       |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| Ephemeral-environment isolation                  | `TestEnvironmentIsolation` family plus a lock-file hardening: Windows can return `ERROR_ACCESS_DENIED` instead of `ERROR_FILE_EXISTS` from `O_CREATE\|O_EXCL`, which the old retry treated as fatal. This was the real cause of the intermittent `prepare.lock: Access is denied` failures. | TOOL-014                                                   |
| Seeder re-point                                  | Fixtures now load through the frozen aggregate stores behind an aggregate-corpus marker row, so exactly one transaction per tenant ever loads them under concurrency. Plan shrank from 21 registrations to 5.                                                                               | DB-019                                                     |
| Policy reconciliation                            | No waivers. LIB-002/003/008 and ARCH-GO-004 pass by relocation (`internal/kernel/{canonical,digest}` to `internal/engines/wire/…`) and evidence-backed allow-list widening. The lane also corrected the record: it had not performed the relocation; a concurrent session had.              | LIB-002/003/008, ARCH-GO-004                               |
| Plancheck wiring                                 | Six gate and boundary checkers wired as `plancheck` subcommands; all report OK on the real tree.                                                                                                                                                                                            | NEXT-002/003 partial (business deps open), GOV-010/011/013 |
| Federal baseline and research questions          | `federalbaseline`: 170 federal restatements across the 50 state files, zero disagreements with `us-federal.md`. `researchquestions`: every §11 blocking item tracked; a DISPUTED state can never be releasable.                                                                             | LEGAL-017/018                                              |
| Rule-pack release family and 22 obligation kinds | Signed, digested, superseding releases; 50 draft state packs extracted from the research corpus (599 obligations, 340 CONFIRMED / 259 VERIFY). No federal pack, by contract.                                                                                                                | LEGAL-010/011                                              |
| Token command, browser login, OTel               | `hcmnext token`, `-dev-browser-login`, OTel interceptors on both transports.                                                                                                                                                                                                                | OBS-002 wiring                                             |

Decisions recorded while folding these in:

- The rule-pack lane's test names ran one behind the ticket ids; renamed to
  match the todo matrix rather than editing the plan.
- Preemption rows in §6.4 and `F` cells in the §5 matrix are not a
  contradiction (the state imposes nothing beyond federal and forbids localities
  from adding to it). Clarified in the spec; extraction findings (eleven `Y`
  cells without a locatable citation, drug-testing and separation-filing
  research gaps) recorded under §11 without adding a fifteenth blocking item.
- Rework of the state research was justified by real errors (invented
  statutes, cross-state contamination, the Kentucky final-pay inversion), not
  by style.

## 3. The question that set the direction

Asked "how much farther to a prototype workflow", the answer was: the compiler,
five step kinds and SIMULATE exist; execution needs durable instance state, a
start-and-advance loop, human step kinds that park on real work items,
inspection, and the one governed write. The owner then directed: move toward
getting the prototype run executable.

### Gate ruling

WF-RUN-000 had decided BUILD (in-house Postgres outbox and projection
primitives) with a blocking re-evaluation before any scheduler, lease or timer
code. Rather than reopen the gate, a prototype ruling was appended to the
decision record: a caller-driven advancement unit of work, fenced by the
instance row's optimistic version, is explicitly exempt; leases, fencing
tokens, recovery, timers, signal subscriptions, retry and workload limits stay
blocked. Every lane since has been briefed with that sentence, and three of
them assert it with a source scan (no goroutines, `time.Now`, `time.Sleep`).

## 4. Execution-path lanes, in landing order

1. **Frontier transition (WF-RUN-024).** A pure `Advance` over the compiled
   plan returning successors, join counters, terminal dimensions and
   scheduling intents (`READY`, `WORK_ITEM_REQUIRED`,
   `SIGNAL_SUBSCRIPTION_REQUIRED`, `TIMER_REQUIRED`, `COMPLETE`) as data. No
   implicit routes: a DECISION without a matching route and without a declared
   default is a typed error. Parity test against the simulator's trace.
2. **Durable instance state and inspector (WF-RUN-001/012/019).** Migration
   00016 (`workflow_instance`, `workflow_node_execution`) with optimistic
   `instance_version`; the inspector renders the whole chronology with
   redaction; SIMULATE formalised (`SIMULATION_SIDE_EFFECT_FORBIDDEN` became the
   contract code, the old code kept as an alias).
3. **Immutable compiled versions (WF-COMP-006).** `Publish` refuses a plan
   whose digest does not match recompiling its definition; `Resolve` never
   falls back to "latest".
4. **WorkItems (WORK-001/002/003).** Migration 00017. Exclusive claim is an
   `item_version` compare-and-swap with a caller-supplied expiry, released on
   the next touch; no sweeper. A trigger makes a completed output immutable
   even against a raw UPDATE. Eight concurrent claimants, one winner.
5. **Start and fenced advancement (WF-RUN-023/025).** Migration 00018
   (`workflow_continuation`, audit-only). One transaction records the node
   result, applies the frontier transition, writes successors, version and
   frontier, and persists every continuation through a `ContinuationSink`
   port; a sink failure rolls the whole advancement back; a deterministic
   retry replays the original receipt. Found and fixed during verification: an
   intermittent `FRONTIER_STATE_INCONSISTENT` caused by comparing two reads
   taken at different points of a READ COMMITTED transaction.
6. **WAIT and SIGNAL (WF-STEP-005/006) and their compiler binding.** Pure
   resolvers with caller-supplied time; DST-ambiguous local times are refused
   as `TIMER_REVIEW_REQUIRED`, never guessed. The compiler gained `WaitSpec`
   and `SignalSpec` with every pre-existing golden digest unchanged; a real
   compiler bug surfaced (un-annotated WAIT/SIGNAL nodes defaulted to an
   internal-mutation effect class and failed compilation).
7. **Durable idempotency (TX-006).** Migration 00019. `Guard` reserves, runs
   the effect and completes inside the caller's transaction; `Reserve` is
   `INSERT … ON CONFLICT DO NOTHING RETURNING` so the race is decided by
   Postgres, not by error-code inspection; a retention shorter than the retry
   window is refused before any row exists.
8. **APPROVAL and TASK (WF-STEP-003/004).** `Open`/`Complete` and
   `Open`/`Submit` over the WorkItem store; quorum and separation of duties
   over the APPROVAL-001 requirement; the `_Browser` test is a server-rendered
   accessibility-contract check because there is no browser in the
   environment.
9. **Admin API and `hcmctl` (SVC-011/ADMIN-001).** Five read-only RPCs on the
   shared interceptor chain; the CLI was first blocked from `cmd/` by the
   layout and process-role manifests, which were widened deliberately, with a
   reviewed ceremony exception for the thin entrypoint.
10. **DB-012 cross-store conformance.** Proves CAS, dedupe, claim exclusivity,
    idempotency, exactly-once continuations and atomic three-store rollback,
    and proves the gated tables are deliberately absent.

Decisions worth keeping:

- Two lanes never share a migration number: numbers are assigned in the brief
  (00017, 00018, 00019) instead of "next free".
- Library-firewall violations are fixed by placement, never by widening:
  transport composition moved to `internal/transport/cell`, the admin adapter
  to `internal/transport/admin`, protobuf handling to `internal/intent/protomap`.
  One lane's fold-back of transport code into `internal/intent/app` was
  reversed the same hour.
- Registry semantics: `append_only: true` requires a `forbid_mutation`
  trigger; grant-level append-only tables stay `false` with a note.
- Opus lanes hit upstream 529 overloads repeatedly; four lanes were resumed or
  relaunched on Sonnet with "continue from the files on disk" briefs and
  finished cleanly.

## 5. Two defects that looked like something else

**The harness time bomb.** `test/bootstrap` started failing on
`explain_transaction` with "the capability invocation was refused" mid-
afternoon, with no relevant diff. One lane blamed a missing `payload_schema`
row. A stderr probe in the transaction-history reader showed the truth: the
harness pins the cell clock to 2026-09-03 12:00 UTC, but the ledger appender
had its own clock, so `recorded_at` was real wall time. Once the afternoon
passed 12:00 UTC every as-known-at read filtered out its own events. Fix: the
store takes a clock option and the harness passes the pinned clock through.
The test had only ever passed because it was run before noon.

**The replay regression.** After the recovery below, WF-RUN-025's retry tests
failed deterministically. The concurrent session's "repair vet" commit had
replaced the three-way replay-detection switch in `advance.go` with a bare
version comparison. Restored by the lane that wrote it. The matching WORK-002
failures had the opposite cause: the source was intact and the transcript
replay had reconstructed a pre-fix snapshot of the test file, which the
work-item lane re-applied its fixture change to. Both point the same way: a
recovered tree is verified per package, never assumed.

## 6. The wipe, and the recovery

At about 11:05 a second Claude session working in the same worktree committed
its own tree and then cleaned untracked files. That deleted the execute driver,
the APPROVAL, TASK and SIGNAL step packages, the runtime continuation store
and its tests, two work-item test files, `cmd/hcmctl` and several golden
fixtures. Nothing was in git or a stash (`git fsck` showed only ordinary stash
WIPs with two parents).

Recovery came from the subagent transcripts
(`~/.claude/projects/<project>/<session>/subagents/agent-<id>.jsonl`), which
hold every `Write`, `Edit`, `Read` and shell `cat` verbatim. A replay script
stitched windowed reads by line number, applied writes as authoritative and
edits in order, and wrote only files it could prove complete. Traps found on
the way: a chained `cat a; echo ---; cat b` lands two files in one result; a
`sed -n '1,260p'` read truncates; goldens produced by `go test -update` leave
no trace and had to be regenerated. Everything came back except one
integration test of the other session's driver, which was replaced by the
end-to-end harness. All recovered files are now staged in the index so a
repeat clean cannot remove them.

Two standing consequences: commit lane output as soon as it verifies, and
never assume HEAD is where it was left.

## 6a. Late-day observations

- The end-to-end promotion run reached the terminal write and stopped on a
  real driver defect: the continuation sink's start idempotency key was never
  populated, so every COMPLETE hit the idempotency guard with an empty scope.
  One-line fix in `driver.go`; the harness lane had already fixed two of its
  own (the start node cannot be an APPROVAL because `frontier.Seed` marks the
  start node READY, and `task.Open` does not set the proposal ref the driver's
  resume validation requires).
- The concurrent session generated 388 untracked `*_Smoke` stub test files
  across the repository (`if t == nil { t.Fatalf(...) }`, BOM-prefixed). They
  are excluded from this session's staged set and left on disk for their
  author; they should not be committed.

## 7. Where the prototype stands

- Green and ticked: 246 todos, including every building block of the
  executable path listed in §4.
- Running: the driver lane (END-node governed write through outbox plus
  idempotency guard, and the end-to-end promotion harness); the EXECUTE RPC
  and authority-gate lane (execution is refused byte-for-byte as today unless
  the cell is composed with an execution authority naming `promote_worker` and
  the caller holds an operator role); the work-item assignment repair.
- Next: run the promotion reference from approved proposal to COMPLETE with
  exactly one ledger event, then commit.

## 8. Numbers

| Measure                                         | Value                                    |
| ----------------------------------------------- | ---------------------------------------- |
| Todos ticked at start of day                    | 208                                      |
| Todos ticked now                                | 246 of 1201                              |
| Migrations                                      | 00001–00020 (00009 intentionally absent) |
| New runtime and human-work tables               | 7                                        |
| Lanes launched or resumed today                 | 30+                                      |
| Lanes lost to upstream overloads and relaunched | 4                                        |
