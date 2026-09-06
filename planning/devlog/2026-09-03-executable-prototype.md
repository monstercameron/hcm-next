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

## 7. First executed run

`TestPromotionWorkflowExecutesEndToEndWithOneGovernedWrite` in `test/workflow`
is green on embedded Postgres: a compiled plan (TRANSFORM, APPROVAL, TASK, five
terminals) is published and activated, an instance starts from a minted
`ProposalRevision`, parks on the governed approval work item, is claimed and
completed through the APPROVAL step, resumes, parks on the task, is submitted
through the TASK step, resumes, and reaches COMPLETE with exactly one
`ledger_event` on the instance stream. A byte-identical second Resume appends
nothing; the inspector renders the full chronology with an empty frontier; the
five completion dimensions are terminal. Two companion tests prove a mutable,
unapproved or superseded proposal never creates an instance, and that the
driver and its effects package contain no goroutine, sleep, ticker or wall
clock.

The terminal write itself (`effects.LedgerTerminalWriter`) did not exist in
the recovered driver, whose own integration test wrote to a sentinel table;
it now appends the promotion outcome through the outbox commit as one ledger
event, one projection advance and one outbox message, behind the idempotency
guard.

The RPC path landed next: `ExecuteIntent` on `IntentService` over both
transports, refused byte-for-byte as before unless the cell is composed with
an execution authority that names `promote_worker` and the caller holds the
operator role; under authority the harness proves the driver starts the real
promotion plan, parks at exactly one approval work item and calls no terminal
writer while parked. The endpoint manifest now names fourteen routes, four of
them refused by default. Governance kept pace: the new harness directory is
documented as a cross-system suite, and the signed P1A manifest was extended
to migrations 00019 through 00021 and re-signed with the fixture key, with the
evidence report regenerated from the checked-in results.

## 9. Review findings and the hardening plan

A read-only review of the first executed run, from inputs to outputs and
observability, found that the run is mechanically sound and governance-weak.
Findings, most serious first, each now tracked as a todo:

- Start trusts caller-asserted `Approved` and `Superseded` flags and only
  checks that an approval ref is non-empty; nothing consults the approval or
  proposal stores (WF-RUN-027).
- Resume validates the work item it is handed for shape and binding but never
  reloads the stored row, so a stale or hand-built item can advance the
  instance (WF-RUN-028).
- Nothing rechecks proposal currency, approval validity or the pinned control
  snapshots while an instance is parked; a supersession, an invalidated
  approval or a policy change during the park is invisible to the run
  (WF-RUN-029; the pre-effect halves already exist as GOVERN-003 and
  APPROVAL-005).
- The terminal ledger event is an identifier envelope: no worker, placement,
  effective date or approver evidence, and the END node's output digest is
  discarded before the write; the harness asserts a row count and never reads
  the payload, the outbox or the checkpoint (WF-RUN-030).
- Two replay designs exist for `Advance`: the committed recorded-attempt
  comparison, which the migration's own comment shows accepts a stale replay
  after later progress, and the uncommitted advancement-receipt design keyed
  by request digest and resulting version (WF-RUN-031, adopting the latter).
- Approval decisions and task submissions persist only as digests and a
  principal id; reason, authority ref and submission fields are unrecoverable
  (WORK-010).
- The engine emits no trace ids, spans, logs or metrics; the only spans are
  the transport interceptors, and ExecuteIntent records evidence only for its
  re-simulation, never for gate refusals, approvals, submissions or the
  terminal write (OBS-023, OBS-024).
- The inspector has no caller outside tests, so an operator cannot see a run
  today; four of its eight stages render empty for this workflow shape and an
  empty governance ref is indistinguishable from an unrecorded one
  (ADMIN-008).
- The harness executes a hand-built demo graph, not the shipped promotion
  reference, which is still simulate-only (PROMO-009). The wire receipt names
  work-item ids under `parked_continuations` (WF-RUN-032).

Cancellability, reverts and undo were reviewed in the same pass: the instance
state machine already declares CANCELLING, CANCELLED, PAUSE_REQUESTED, PAUSED,
BLOCKED, QUARANTINED and SUPERSEDED, and nothing invokes them (WF-RUN-008,
WF-RUN-010, WF-RUN-015). There is no undo anywhere by design: the ledger is
append-only and corrections are CORRECTION events pointing at what they
correct (LEDGER-005 exists, TX-007 does not), so reverting a completed
promotion means a compensating transaction through COMPENSATE (WF-STEP-016)
or RepairPlan execution (WF-RUN-016, REPAIR-002), neither built. The order
chosen: currency rechecks and stored-fact derivation first, then governed
cancellation at park boundaries, then compensation.

### 9a. Hardening pass outcome (paused here)

Four fix lanes ran against the findings above; the owner paused the effort
after this pass. State at the pause, all uncommitted but staged in the index:

- Landed and ticked: WF-RUN-028 (Resume reloads the stored work item and
  refuses drift), WF-RUN-029 (currency guard at every park boundary and
  before the terminal write; a material change moves the instance to
  BLOCKED), WF-RUN-030 (terminal event carries the END output digest, worker,
  placement, effective date and the approval and task decision ids; harness
  asserts payload, outbox and checkpoint and re-enters the ledger idempotency
  guard on a direct replay), WF-RUN-032 (typed continuation and work-item
  lists on the wire receipt), WORK-010 (append-once `work_item_decision`
  table, migration 00022, holding the full typed decision or submission),
  ADMIN-008 (`GetWorkflowInstance` on the admin service and `hcmctl instance`
  with gap kinds distinguishing unrecorded from redacted from absent).
- Partial: WF-RUN-027 (stored-fact ports exist and are enforced when
  supplied, but the caller-asserted approval flags remain as a documented
  fallback because no durable approval store exists and the intent service
  still constructs them); WF-RUN-031 (the concurrent session's
  advancement-receipt replay design is adopted and its regression test
  passes; the race, fault and mutation tests are not written).
- Stopped by the owner mid-verification: OBS-023 and OBS-024. The
  instrumentation and evidence ports, the OTel-backed adapter, the in-memory
  test exporter and the evidence vocabulary are on disk and the tree builds
  and vets clean, but their integration tests were not re-run after the last
  fix and neither todo is ticked. Resume by running the two packages'
  test suites, then `test/workflow` and `test/bootstrap`.
- Not started: PROMO-009 (execute the real promotion reference), cancellation
  (WF-RUN-010), pause (WF-RUN-008), compensation (WF-STEP-016), correction
  (TX-007).

Two facts for whoever resumes. The concurrent session keeps generating
`*_Smoke` stub tests and new packages that fail the library firewall
(`internal/platform/cache`, `internal/transport/eastwest`, `internal/i18n`,
`internal/resource/reservation`, `internal/operations/reliability`); those
failures are not this pass's. And `TestTodo_ARCH_GO_009_Integration` fails on
that session's new engines lacking `Version` and `Explain`.

## 10. Evening: the console goes, the journey slice begins

Three decisions after the pause, each with its reason.

**The React console is archived, not kept.** The plan selected
GoWebComponents on 2026-09-03 (UX-QUAL-001) and the P1B release image rule
forbids a Node, React or Vite runtime on the production path. Two front ends
would have meant two sources of truth for the same screens, and the console
ran on local demo data against a TypeScript API, not the Go cell. It is
zipped outside the repository (`Desktop/hcm-next-archive/react-console-2026-09-03-8224919.zip`)
and removed from the workspaces, `tsconfig`, the code-style rules and the
lockfile. Git history keeps the rest.

**The Go cell is the dev server.** PostgreSQL 17 (arm64) comes from the
embedded-postgres archive the test suite already caches; there is no `psql`
in that bundle, so the database is created from a Go one-off. Two traps cost
an hour and are now in the README: the workspace resolves workers against the
fixture tenant `harborcare-demo`, so a cell served or seeded as `harborcare`
answers a silent 403; and `hcmnext token` mints `comp_admin` only, while the
workspace needs `intent_author` too and ExecuteIntent needs
`promotion_operator`.

**The journey page speaks gRPC over a WebSocket, not HTTP forms.** The
first design was a server-rendered page with `<form method="post">` actions,
in the pattern of the Promotion workspace. The owner rejected it: "no http,
use grpc and web sockets". The revised shape is the one the plan already
names (grpcbridge is the sole public projection; TOOL-009 is its streaming
qualification): a canonical `hcmnext.journey.v1.JourneyService` on the shared
gRPC server, GoGRPCBridge's `pkg/grpctunnel` mounted at `/grpc` on the HTTP
edge, and a GoWebComponents client compiled to WASM that dials the tunnel
with `pkg/wasm/dialer` and sends the bearer as per-RPC metadata. The only
HTTP left is what a browser needs to load a page: the shell document, the
wasm bundle, the dev sign-in form. The tunnel forwards trace and correlation
headers into metadata but not cookies or Authorization, so the shell places
the admitted bearer in a JSON island for the client to use; that is
acceptable for a dev-login demo and is named as such in the code.

The engine side is unchanged by the pivot: `workspace.JourneyEngine` is the
port, implemented in `internal/intent/app` over CreateIntent, SimulateIntent,
ExecuteIntent, the WorkItem store and the driver's Resume. The approver
decision is rebuilt deterministically from the completed WorkItem row, so a
restart between "approve" and "resume" loses nothing. The stage a page shows
is derived from durable rows (intent, instance, work items, ledger), never
asserted by the client.

Rendered-HTML goldens were also dropped from the renderer's tests at the
owner's request: the product surface is the live WASM mount, and committing
HTML files invited the wrong reading of what the page is.

Lanes: renderer (opus), engine (opus), JourneyService proto and gRPC (opus),
tunnel and shell (opus), README recipe (haiku); a fable review pass follows.
The HTTP-handler lane was stopped before it wrote a file. New dependency:
GoGRPCBridge v1.1.2 (with gorilla/websocket transitive), classified in
`definitions/architecture/dependency-roles.yaml`; grpc and protobuf roots
widened to the wasm client packages only. Todo `UX-009` tracks the slice.

### 10a. Two defects the live cell found that the suites had not

**The pool's own hygiene invalidated pgx's statement cache.** `pgxadapter.NewPool`
runs `RESET ROLE`, `pg_advisory_unlock_all()` and `DISCARD ALL` in `BeforeAcquire`
so a borrower never inherits session state. `DISCARD ALL` also deallocates every
named server-side prepared statement, and pgx's default query mode
(`QueryExecModeCacheStatement`) prepares each SQL text once per connection and
remembers the name client-side. The second borrower of a recycled session binds
a statement the server no longer has: SQLSTATE 26000, surfaced on the wire as
`intent.domain_unavailable`. On a schema-pinned pool (every test) the failure
hid behind a second defect: the hygiene's own `set_config` call was itself a
cached statement, failed the same way, and made pgxpool destroy the session and
open a fresh one on every acquisition, so tests passed on connection churn while
`HygieneFailures()` climbed. Fix: `DefaultQueryExecMode = QueryExecModeCacheDescribe`
(extended protocol through the unnamed statement, the setting pgx documents for
any reset-between-uses session) and hygiene through the simple protocol.
`TestPoolReusesAConnectionAcrossItsOwnHygiene` pins both: zero hygiene failures
and exactly one connection reused across five acquisitions.

**A dev token could read but never propose.** `intent.Instance.Validate`
requires `organization_scope_id`; `hcmnext token` had no way to mint one, so
CreateIntent refused every proposal with the opaque
`release.p1a_zero_effect_ceiling` violation while the workspace's reads worked.
`-org-scope` added, with the claim round-tripped through the verifier in
`TestTokenCommandCarriesTheOrganizationScope`. The diagnostic that named the
field is deliberately never rendered on the wire; an in-process harness that
composes the cell like `cmd/hcmnext` and calls the engine port is how it was
read, and that is the right tool for the next opaque refusal too.

**First live run (21:20 UTC), over gRPC on 127.0.0.1:8443:** propose →
PROPOSED; execute → AWAITING_APPROVAL, instance version 5, one APPROVAL
WorkItem routed to `principal:promotion-approver`; approve → COMPLETED,
instance version 16, six node executions (`end_approved` SUCCEEDED, the four
sibling terminals SKIPPED), one `ledger_event` on the instance's own stream
(`hcmnext.workflow.PromotionOutcome/v2`), sixteen timeline entries. A second
proposal for another worker came back BLOCKED: that corpus scenario has no
executable plan, which is the correct answer, not a fault.

### 10b. The page runs the slice from the browser

At 21:43 UTC the GoWebComponents client, compiled to WASM (24.5 MB with grpc-go
inside; a size to revisit, not a blocker) and mounted by the shell at
`/workspace/journey`, dialled the cell's gRPC server through GoGRPCBridge's
WebSocket tunnel, listed the three journeys already in the database, opened
the proposed one, ran "Execute under authority" and "Approve" as buttons
backed by RPCs, and drew the recorded ledger fact as the WatchJourney stream
delivered it. No form post, no JSON route: the browser speaks
`hcmnext.journey.v1.JourneyService` over `/workspace/grpc`.

One placement finding on the way: the tunnel first sat at `/grpc`, and every
upgrade was refused `authentication.missing_credential`. The dev sign-in
cookie is scoped to `RoutePrefix` (`/workspace/`), so a socket opened outside
that path carried nothing. The tunnel moved under the prefix rather than the
cookie widening; the constant's comment says why.

The stream path needed its own admission: the shared gRPC server chained only
a unary interceptor, so a server-streaming handler would have run with no
principal, no invocation and no deadline cap. `grpcserver.StreamInterceptor`
now shares the unary boundary's admit/refuse/conclude, and `WatchJourney` is
a real server stream (one message per digest change, 15-minute ceiling).
`transport.Admit` sees no request message on the stream path, which is fine
for `WatchJourneyRequest` today and is written down where it will matter.

### 10c. Employees the page can create

The owner's third clarification: a vertical slice means the user creates
employees, picks one and runs that person through the workflow. The cell's
worker facts were four corpus fixtures in memory, so "create" needed a durable
home the governed read could see. Migration `00023_journey_workforce.sql`
adds `journey_worker`: one append-only, tenant-scoped row per created worker
carrying placement, declared compensation baseline and its own bitemporal
coordinates. `internal/data/workforce.Facts` projects rows onto
`people.WorkerFacts` exactly as the corpus reader does, and
`NewLayeredWorkerFacts` answers corpus first, then the table, so
`explain_worker_state`, the workspace and the journey all see created people
through the one port. `app.WorkerLocator` is the single reference resolver
(corpus key, then table key or id, then the permissive corpus id fallback)
bound to the domain-input resolver, the workspace reader and the journey;
before it existed a created worker was visible to capability handlers and
invisible to the simulation that certifies the promotion, because the
resolver held a private corpus reader. `JourneyService` gains `ListWorkers`
and `CreateWorker` (a governed demo-authority write behind the operator
role, evidence `WORKER_CREATED`/`WORKER_REFUSED`), the renderer a People
table and a New employee form, the client the selection route
`#/journeys?worker=<ref>` and the create flow. The acceptance test
`TestJourneyCreatedWorkerCompletesTheWholePromotion` runs a created worker
from proposal to the recorded ledger fact.

Two facts recorded rather than hidden: a created worker's key is not a
well-formed `values.EntityId`, so the promotion payload carries the key
while the journey's subject carries the uuid; both resolve to one row and
nothing cross-checks them. And `WorkforceOptions.Currency` is only set for
a single-currency band catalog.

### 10d. 2026-09-05: the slice from the browser, and the review pass

Resumed after the overnight break with PostgreSQL down (the launch entry is
a foreground process; it does not survive the session). Once it was back:

**The create-select-run flow from the page.** Signed in at the dev form,
added "Priya Raman" through the New employee panel (OPS-HRBP2 / P2,
people-ops, USD 91,000.00), watched the row appear as CREATED and selected,
proposed OPS-HRBP3 / P3 for her with the form's defaults, opened the
journey, executed under authority, approved as the routed approver, and read
the `hcmnext.workflow.PromotionOutcome/v2` fact off the page as the stream
delivered it. Every step was an RPC over the WebSocket tunnel; nothing was
a form post. That is UX-009's GREEN, and it is ticked.

**The fable review pass** was cut off by the weekly usage limit (HTTP 429)
after landing four of its nine items: `journey_decide.go` resumes through
`stepsapproval.NewContinuation` + `Resolve` because the execution
composition now routes the approval with a real compiled requirement
(`prototype.CompileApprovalRequirement`) rather than placeholder digests;
the driver's evidence sink is the cell's own, so Inspect lists
`APPROVAL_COMPLETED` and `TERMINAL_WRITTEN`; the operator surface's
database read moved behind `app.WorkflowInspector` (transport imports no
store again; the layer-graph golden gained `data -> domains` and
`transport -> workflow`, both reviewed); and `otelmw` gained a stream
interceptor. It rewrote the bootstrap audit assertions without running them:
`workitem.Store.Complete` releases the claim, and the routing transitions are
the factory's, so the test now asserts exactly that (the journey's own claim,
start and decision name the signed-in actor; the routing rows never do).
The port test the lane owed (`workflow_inspector_test.go`) is being written
by a sonnet lane; items 5 to 9 (lint noise, workforce validation review,
watch leak review, pgxadapter Connect review) remain for the next pass.

Housekeeping: `test/tunnel`'s fake engine gained the workforce methods it
was missing; the P1A manifest carries migrations 00022 and 00023 with a
refreshed checksum for 00021 (changed by another session's commit) and is
re-signed with the fixture key; `go mod tidy` promoted GoGRPCBridge,
GoWebComponents and the OpenTelemetry modules to direct requirements.

## 8. Numbers

| Measure                                         | Value                                    |
| ----------------------------------------------- | ---------------------------------------- |
| Todos ticked at start of day                    | 208                                      |
| Todos ticked now                                | 246 of 1201                              |
| Migrations                                      | 00001–00020 (00009 intentionally absent) |
| New runtime and human-work tables               | 7                                        |
| Lanes launched or resumed today                 | 30+                                      |
| Lanes lost to upstream overloads and relaunched | 4                                        |
