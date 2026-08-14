# HCM Next

> **Every workforce action, one governed operating model.**

HCM Next is a Go-based architecture for expressing, governing, executing,
observing, and repairing Human Capital Management business intent.

It is not architecturally centered on integrations, a collection of modules, or
an AI assistant. Its behavioral center is `BusinessIntent`; its data center is
the governed Person/Worker Graph.

```text
                          BEHAVIOR

                       BusinessIntent
                            |
                            v
Person / Worker Graph <-----+-----> Capability Graph
                            |
                            v
                         Workflow
                            |
                            v
                          Outcome
```

The initial product, **HCM Next ChangeOps**, applies this architecture to
cross-system employee changes while incumbent systems remain authoritative. That
is the entry wedge—not the boundary of the architecture.

## Project status

HCM Next is currently an architecture and early implementation project. It is
not a production HCM suite, payroll engine, or UKG/Workday replacement.

Artifact maturity must be interpreted precisely:

```text
planning/specs       mixed contracts, active registers and reference documents;
                     consult the ownership registry for each artifact's status
schema/proto         canonical contract sources; generation toolchain not yet pinned
schema/schemaflux    draft structured sources; no HCM compiler pipeline yet
src/blocks/go        existing Go conformance and execution fixtures
legacy TypeScript    historical behavior evidence; not the target architecture
```

Do not present a planned subsystem, catalog entry, diagram, or Protobuf source as
implemented product behavior. The Phase 1 execution gates define what is actually
being built.

## The core thesis

HCM is a coherent domain of business intent, facts, capabilities, decisions,
workflows, effects, and outcomes.

Examples of business intent include:

```text
Promote Jane                 Hire Sarah
Run payroll                  Request leave
File a government report    Investigate a complaint
Change benefits              Find successors
Correct a paycheck           Provision access
Analyze turnover             Send a legal notice
```

All enter the same architectural spine:

```text
                         BUSINESS INTENT
                                |
                                v
                         INTENT KERNEL
                    classify / normalize / scope
                                |
                                v
                           GOVERNANCE
          Identity | AuthZ | Legal | Privacy | Entitlement
              Risk | Purpose | Jurisdiction | Policy
                                |
                                v
                            WORKFLOW
          capabilities | rules | approvals | tasks | timers
          signals | agents | documents | calculations | waits
                                |
                                v
                       DOMAIN CAPABILITIES
       People | Workforce | Rewards | Talent | Experience | Access
       Payroll | Benefits | Recruiting | Regulatory | Intelligence
                                |
                                v
                    DETERMINISTIC EFFECT PLAN
                                |
              +-----------------+-----------------+
              v                 v                 v
       HCM Next state     External systems    Human interaction
              |                 |                 |
              +-----------------+-----------------+
                                |
                                v
                         OBSERVE OUTCOME
                                |
                                v
                           RECONCILE
                                |
                                v
                    COMPLETE | DIAGNOSE | REPAIR
```

The responsibilities remain distinct:

| Concept             | Question answered                                                                         |
| ------------------- | ----------------------------------------------------------------------------------------- |
| Person/Worker Graph | What people, employments, assignments, positions, organizations, and relationships exist? |
| BusinessIntent      | What is an authorized actor trying to accomplish or answer?                               |
| Governance          | May it happen, for this purpose, subject, scope, time, and risk?                          |
| Capability          | What typed semantic operation can perform part of it?                                     |
| Workflow            | In what sequence, under which obligations and failure rules, should it happen?            |
| Domain              | Which component owns the business meaning, invariants, and authoritative writes?          |
| Ledger              | What was proposed, decided, attempted, committed, observed, corrected, or superseded?     |
| Reconciliation      | Did observed reality reach the intended outcome?                                          |
| Repair              | How is a partial or incorrect outcome corrected without rewriting history?                |

## BusinessIntent is the behavioral root

`BusinessIntent` means something a human, agent, service, schedule, rule, or
external event wants HCM Next to accomplish or answer.

The stable kernel has seven families:

```text
BusinessIntent
  |
  +-- ChangeRequest         governed domain mutation
  +-- ProcessRequest        durable multi-step business process
  +-- CalculationRequest    deterministic calculation
  +-- FilingRequest         regulated filing or submission
  +-- Case                  service, investigation, or confidential matter
  +-- BatchOperation        bounded population operation
  `-- AnalyticalRequest     governed query, explanation, or inference
```

Semantic intent names such as `ChangeManager`, `RunPayroll`, or
`AnalyzeTurnover` do not create new runtime classes. They are immutable,
versioned `IntentDefinition`s with typed Protobuf input/output, ownership,
governance, side effects, idempotency, conflict, evidence, reliability, and
outcome contracts.

The supplied semantic vocabulary contains 530 proposed candidates. Fourteen are
currently recorded as draft definitions; the remaining 516 still require
lossless ingestion into governed source partitions. Proposed or catalogued does
not mean executable. Only definitions that advance through the governed maturity
lifecycle may be invoked:

```text
CATALOGUED -> DRAFT_CONTRACT -> CONTRACTED -> COMPILED -> PUBLISHED
                                                        |
                                           DEPRECATED -> RETIRED
```

See the [Business Intent Catalog](planning/specs/business-intent-catalog.md) and
the canonical [Protobuf contract](schema/proto/hcmnext/intents/v1/business_intent.proto).

## Native and external execution share one business model

A connector is an execution adapter, not the product model.

Early Manager Change:

```text
ChangeManager
      |
      v
governed workflow
      |
      v
people.manager.change
      |
      v
Workday / UKG connector
```

Later, for a scope where HCM Next owns People:

```text
ChangeManager
      |
      v
same governed workflow
      |
      v
people.manager.change
      |
      v
HCM Next People domain
```

The semantic capability is stable. Authority resolution changes:

```text
SourceAuthority
  authority: WORKDAY

becomes, after an explicit field/population/domain cutover:

SourceAuthority
  authority: HCM_NEXT
```

Authority is resolved by field, domain, population, organization, jurisdiction,
effective time, and customer—not by a global `system_of_record` flag.

## Composition is recursive, but bounded

One intent can create explicitly related child intents:

```text
HireWorker
  |
  +-- ResolvePersonIdentity
  +-- CreateEmployment
  +-- SetCompensation
  +-- EnrollWorkerInPayroll
  +-- ProvisionWorkerAccess
  +-- AssignOnboardingLearning
  `-- StartOnboarding
```

A child operation may be a separately governed intent when it needs independent
authorization, lifecycle, idempotency, evidence, repair, or outcome tracking. It
may remain a capability call when the parent fully owns those semantics.

This recursion is not arbitrary runtime recursion. The compiled plan requires:

- explicit parent/child relationship and version;
- bounded depth and fan-out;
- typed inputs and results;
- deterministic idempotency and ordering;
- governance at every authority boundary;
- declared cancellation, compensation, and completion propagation;
- cycle rejection unless a bounded loop primitive is explicitly supported.

Parent completion cannot erase or collapse child truth.

## Workflow coordinates; domains own meaning

The workflow kernel understands a small set of durable primitives:

```text
CAPABILITY  DECISION  RULE       APPROVAL   TASK
WAIT        SIGNAL    PARALLEL   JOIN       SUBWORKFLOW
TRANSFORM   AGENT     DOCUMENT   OBSERVE    CHECKPOINT
COMPENSATE  END
```

It does not own salary calculation, tax, worker storage, legal interpretation,
email delivery, vendor API behavior, or AI authority. It invokes typed capabilities
owned by the relevant domain or platform plane.

```text
Customer-specific Promotion workflow
        |
        +-- people.job.change
        +-- positions.reserve
        +-- rewards.compensation.simulate
        +-- budget.compensation.reserve
        +-- regulation.obligations.resolve
        +-- approvals.resolve
        +-- documents.generate
        +-- payroll.worker.sync
        +-- access.entitlements.recalculate
        +-- learning.assign
        +-- communications.send
        `-- reconciliation.verify
```

Capabilities form the vocabulary. Compiled workflows are the grammar customers
use to express how their organization operates.

## Atomic truth and honest partial completion

One parent intent does not imply distributed ACID across SaaS providers.

```text
parent BusinessIntent
        |
        v
authoritative local transaction core
        |
        v
ONE ACID COMMIT
ledger events + critical projections + intent state + outbox
        |
        +--------------+---------------+---------------+
        v              v               v               v
     payroll          access         learning       messaging
        |              |               |               |
        +--------------+---------------+---------------+
                               |
                               v
                     observe + reconcile + repair
```

Completion is multidimensional:

```text
RuntimeState          COMPLETED
BusinessState         COMPLETED
ExternalConsistency  DEGRADED
ReconciliationState  REPAIR_REQUIRED
OperationalState     INCIDENT
ObligationState       SATISFIED
```

If Jane is promoted but one downstream access grant fails, Jane remains promoted.
HCM Next records the drift, creates a bounded `RepairPlan`, redrives or corrects
the access effect, observes the result, and closes reconciliation. It does not
rewrite the promotion or claim that everything succeeded.

## Agents interpret and compose; deterministic services execute

AI assistance is not an authority bypass and generic “agentic orchestration” is
not the product moat.

```text
user intent or authorized data
              |
              v
agent discovers visible typed capabilities
              |
              v
typed BusinessIntent / workflow draft / analysis plan
              |
              v
schema + AuthZ + legal + privacy + risk + cost validation
              |
              v
simulation + human decision where required
              |
              v
deterministic capability execution
              |
              v
observation + reconciliation + outcome evaluation
```

Agent inputs may be hostile. Prompt injection controls, semantic taint,
least-privilege data retrieval, tool-use authorization, typed output validation,
budgets, kill switches, and incident handling surround every agent path.

## Product path

HCM Next approaches the HCM market through earned authority:

```text
Stage 1  ChangeOps overlay
         observe, simulate, approve, reconcile
              |
Stage 2  workflow operating layer
         controlled cross-system execution and repair
              |
Stage 3  system of transaction
         HCM Next owns transaction truth and selected writes
              |
Stage 4  selected system of record
         explicit field/domain/population authority
              |
Stage 5  Workforce Operating System
         native and external domains share one intent architecture
```

The initial competitive proposition is not “replace UKG” or “replace Workday.”
It is:

> Safely coordinate workforce change across everything you already run.

The long-term architectural proposition is:

> One semantic runtime for HCM business intent.

Native payroll, deep workforce management, benefits, recruiting, and other mature
suite domains remain separate authority-expansion investments. A box in a diagram
or an intent in the catalog is not a feature-parity claim.

See [Competitive Positioning and Authority Expansion](planning/specs/competitive-positioning-and-authority-expansion.md).

## Phase 1: Promotion and Compensation Change

Phase 1 proves one bounded vertical slice for paid design partners:

```text
manager / HR intent
      -> immutable proposal
      -> source-authority-aware reads
      -> deterministic preflight and simulation
      -> exact approval binding
      -> effective-time conflict control
      -> execution-time revalidation
      -> one governed write path
      -> external observation
      -> reconciliation or RepairPlan
      -> complete evidence
```

Phase 1 does not implement the complete intent catalog, a global payroll engine,
deep WFM, a general integration marketplace, or autonomous write-capable agents.
The authoritative delivery scope is the [Phase 1 Execution Plan](planning/execution-plan.md).

Reference conformance workflows include:

- [Manager Change](planning/reference-workflows/manager-change.md), the smallest useful single-domain mutation;
- [Promote Into Management](planning/reference-workflows/promote-into-management.md), the cross-domain composition fixture;
- the broader [Reference Workflow Suite](planning/reference-workflows/reference-suite.md).

The [HR Workflow Exploration Index](planning/workflows/README.md) extracts the
concrete legacy workflows and models their dependencies, features, steps, data,
failure paths and candidate domain properties. Exploratory workflow documents are
discovery artifacts, not implementation or contractual maturity claims.

## Architecture planes

```text
1. Experience       GWC UI | APIs | SDK | CLI | agent interface
2. Identity         humans | agents | services | federation | sessions
3. Governance       AuthZ | legal | privacy | risk | entitlement | DLP
4. Control          tenants | config | schemas | capabilities | versions
5. Workflow         durable orchestration | human work | timers | repair
6. Domain           People | Workforce | Rewards | Talent | Experience | Access
7. Connectivity     integrations | messaging | files | events | government
8. Data             ledger | artifacts | projections | outbox | serving planes
9. Intelligence     reports | analytics | semantic access | outcomes

Cross-cutting:
   Operations / Assurance
   Billing / Metering
```

The synchronous dependency spine is:

```text
Experience
    -> Identity
    -> Governance
    -> Control resolution
    -> Workflow
    -> Domain capability
    -> authoritative transaction and data truth
```

Connectivity, Intelligence, agents, Operations, and Billing attach to this spine
without becoming universal synchronous dependencies.

## Technology constitution

The target product and toolchain are Go-only:

```text
                         Go
                          |
          +---------------+---------------+
          v               v               v
   GWC / GoWebComponents  grpcbridge      SchemaFlux
      experience UI       transport       definitions/compiler
          +---------------+---------------+
                          |
                          v
                Go domain/runtime packages
                          |
                          v
          Protobuf/gRPC + governed infrastructure
```

- **GWC / GoWebComponents** owns Go-authored browser and presentation behavior.
- **grpcbridge** adapts HTTP, gRPC-Web, WebSocket, and SSE to canonical gRPC.
- **SchemaFlux** validates and compiles structured platform definitions.
- **Protobuf** is canonical for service and typed payload contracts.
- PostgreSQL, OpenTelemetry, object storage, and reviewed low-cost/open-source
  infrastructure support the core; they do not replace it.

No new TypeScript, JavaScript application framework, Node service, or parallel
REST business implementation belongs in the target architecture. Existing Node,
TypeScript, and React files are legacy evidence and must not be extended.

See the [Go-Only Technology Constitution](planning/specs/go-only-technology-constitution.md).

## Repository map

Current and target material coexist during migration:

```text
planning/
  plan.md                 architecture constitution and long-term direction
  execution-plan.md       bounded Phase 1 delivery gates
  specs/                  focused contracts
  reference-workflows/    conformance scenarios
  workflows/              exploratory per-domain workflow and data inventories

schema/
  proto/                  canonical Protobuf contracts
  schemaflux/             governed structured definitions

src/blocks/go/            current Go fixtures and execution code

src/, package.json
                          legacy implementation evidence unless a planning
                          contract explicitly classifies an artifact otherwise
```

The target Go repository shape is described in the technology constitution. Do
not infer the target architecture from the legacy Node workspace layout.

## Source-of-truth hierarchy

```text
planning/plan.md
  strategy + architecture constitution
        |
        +-- planning/execution-plan.md
        |     delivery scope and sequencing
        |
        +-- planning/specs/*.md
        |     owned subsystem contracts
        |
        +-- planning/reference-workflows/*.md
              integration/conformance behavior

schema/proto + schema/schemaflux + migrations + executable tests
  authoritative over prose where implementation exists
```

Important starting points:

- [High-Level Plan](planning/plan.md)
- [Phase 1 Execution Plan](planning/execution-plan.md)
- [Canonical Platform Plane Model](planning/specs/platform-plane-model.md)
- [Business Intent Kernel](planning/specs/business-intent-and-change-request.md)
- [Business Intent Catalog](planning/specs/business-intent-catalog.md)
- [Workflow Execution Kernel](planning/specs/workflow-runtime.md)
- [Transaction Plan and Commit Coordinator](planning/specs/transaction-plan-and-commit-coordinator.md)
- [Integration Platform](planning/specs/integration-platform.md)
- [Capability Coverage Matrix](planning/specs/platform-capability-coverage-matrix.md)
- [Risk Register](planning/specs/risk-register.md)

## Working in the repository

The existing Go fixture suite can be run with:

```powershell
go -C .\src\blocks\go test ./...
```

The repository does not yet have a pinned root Protobuf/SchemaFlux generation
command. Do not check in handwritten Go duplicates of Protobuf messages to work
around that gap. Pin the Go-native generator/toolchain and generated-file policy
before generated contracts become a build dependency.

When changing architecture or implementation:

1. Identify the owning plane, domain, capability, and authority boundary.
2. Update the focused contract rather than hiding a responsibility in workflow code.
3. Preserve intent, domain, execution, observation, and outcome truth separately.
4. Add or update a reference/conformance scenario.
5. Keep Phase 1 implementation depth explicit.
6. Use Go, GWC, grpcbridge, SchemaFlux, and canonical Protobuf contracts.
7. Do not extend the legacy TypeScript/Node runtime.

## License

This repository is currently private and unlicensed for external use unless a
separate license grant says otherwise.
