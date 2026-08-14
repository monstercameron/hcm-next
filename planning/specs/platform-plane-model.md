# Canonical Platform Plane Model

## Purpose

This contract defines the top-level responsibility boundaries and permitted
dependency direction for HCM Next. A plane is a logical ownership boundary, not
necessarily a separately deployed service.

The central rule is:

> Control defines behavior. Workflow coordinates behavior. Domains own HCM
> meaning and authoritative transactions. Data preserves and serves it.
> Intelligence interprets it. Governance constrains every route.

## Complete Logical Architecture

```text
                    EXPERIENCE / API PLANE
          GWC UI | HTTP | gRPC | CLI | Agents | Partners
                              |
                              v
                    IDENTITY / TRUST PLANE
          AuthN | sessions | federation | workload identity
                              |
                              v
                     GOVERNANCE PLANE
        AuthZ | Legal | Privacy | Entitlements | Risk | DLP
                              |
                              v
                      CONTROL PLANE
    tenant | org | config | capabilities | policies | schemas
    versions | workflows | agents | connectors | feature state
                              |
                              v
                     WORKFLOW PLANE
    orchestration | approvals | tasks | timers | signals | retry
    checkpoints | compensation | repair | pause | migration
                              |
                              v
                  DOMAIN + REGULATORY PLANE
    People | Workforce | Talent | Rewards | Experience | Access
    payroll | benefits | recruiting | IAM | regulatory computation
                              |
                              v
                         DATA PLANE
      ledger | artifacts | projections | outbox | operational state
      cache | search | event distribution | semantic indexes
                              |
                     durable effect intents
               +--------------+--------------+
               v                             v
      INTEGRATION CONNECTIVITY       HUMAN COMMUNICATIONS
      APIs | webhooks | files        email | SMS | inbox
      events | government gateways   push | Slack | Teams
               +--------------+--------------+
                              |
                  observations / delivery signals
                              |
                              v
                    DATA OBSERVATION PATH
                              |
                              v
                    INTELLIGENCE PLANE
      reporting | analytics | metrics | semantic model | knowledge
      decisions | predictions | process mining | outcome analysis

  +----------------------------------------------------------------+
  | OPERATIONS / ASSURANCE wraps every plane                         |
  | telemetry | SLOs | reconciliation | incidents | integrity | DR |
  | security monitoring | repair | audit evidence | trusted time    |
  +----------------------------------------------------------------+

  +----------------------------------------------------------------+
  | BILLING / METERING observes semantic capability boundaries      |
  | entitlement | usage | rating | cost | budget | chargeback       |
  +----------------------------------------------------------------+
```

Integration and Messaging are distinct semantic systems inside the broader
Connectivity Plane. Regulatory computation is a specialized cross-domain
capability plane attached to Workflow and Domain; government submission itself
uses Connectivity.

## Nine Primary Planes

|   # | Plane               | Owns                                                                                                                        | Must not own                                                     |
| --: | ------------------- | --------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
|   1 | Experience / API    | Human and machine entry points, presentation, protocol exposure                                                             | Business authority or direct persistence                         |
|   2 | Identity / Trust    | Authentication, federation, sessions, assurance, workload and agent identity                                                | Business permission or legal interpretation                      |
|   3 | Governance          | AuthZ, legal, privacy, risk, commercial entitlement, DLP, obligations                                                       | Domain mutation implementation                                   |
|   4 | Control             | Versioned definitions and applicability: tenant, org, capability, policy, schema, workflow, agent, connector, feature state | Processing Jane's promotion                                      |
|   5 | Workflow            | Durable orchestration, approvals, tasks, waits, signals, retry, compensation, intervention                                  | Payroll, salary, tax, email, vendor, or worker-storage semantics |
|   6 | Domain + Regulatory | Commands, queries, validation, invariants, calculations, canonical projections, domain events                               | Presentation or provider transport                               |
|   7 | Connectivity        | System integrations, human messaging, events, files, callbacks, government gateways                                         | Canonical HCM meaning                                            |
|   8 | Data                | Truth, artifacts, operational state, serving views, distribution state                                                      | Unversioned business policy                                      |
|   9 | Intelligence        | Reports, metrics, analytics, semantic access, decisions, predictions, outcomes                                              | Authoritative domain mutation                                    |

Operations/Assurance and Billing/Metering are cross-cutting overlays rather
than numbered downstream stages.

## Material Command Path

Material business changes follow this dependency spine:

```text
human / agent / system intent
              |
              v
Experience/API through GWC or grpcbridge
              |
              v
Identity -> PrincipalContext
              |
              v
Governance -> allow / deny / obligations
              |
              v
Control resolution -> pinned versions and configuration
              |
              v
Workflow -> coordinate a compiled plan
              |
              v
Domain capabilities -> validate and execute HCM semantics
              |
              v
ACID transaction -> ledger + critical projections + outbox
              |
              +--> Connectivity effects and observations
              |
              +--> rebuildable Intelligence consumers
```

Control resolution is conceptually inline but need not be an independent
network request. A signed/pinned configuration snapshot can be resolved locally
and recorded in the execution fingerprint.

Governed reads do not require a workflow:

```text
Experience/API
      -> Identity
      -> Governance
      -> Domain Query or Intelligence Query
      -> authorized result
```

Workflow is required when business process, approval, durable waiting,
coordination, side effects, or material mutation requires it.

## Domain Ownership Boundary

The Workflow Plane never writes domain tables or assembles domain events
directly.

```text
WRONG

Promotion workflow -> UPDATE worker_projection

RIGHT

Promotion workflow
      |
      +--> people.promote
      +--> rewards.compensation.change
      +--> access.entitlements.recalculate
                  |
                  v
          domain-owned transaction
```

Each domain owns:

```text
commands and queries
validation and invariants
canonical transaction behavior
events and critical projections
domain-specific idempotency and concurrency
simulation and explanation where applicable
```

The Workflow Plane coordinates typed capability results. It does not become a
generic place for HCM business logic.

## Control Plane Boundary

The Control Plane determines which immutable versions apply:

```text
ExecutionContext
      |
      +-- tenant and organization configuration
      +-- capability and schema versions
      +-- workflow compiled plan
      +-- AuthZ/legal/policy packs
      +-- reference data and mappings
      +-- connector and feature configuration
      +-- agent/model/tool configuration
      v
ExecutionFingerprint
```

It publishes, activates, resolves, quarantines, and retires configuration. It
does not execute the employee transaction represented by that configuration.

## Connectivity Plane

Connectivity has two primary semantic branches:

```text
                        CONNECTIVITY
                             |
             +---------------+---------------+
             v                               v
       SYSTEM INTEGRATION              HUMAN MESSAGING
  connectors | APIs | files       audience | template | channel
  webhook | event | government    inbox | delivery | reply
             |                               |
             v                               v
 external operation/observation       delivery/response signal
```

They may share queueing, leases, backpressure, retry budgets, idempotency,
telemetry, encryption, and provider-health infrastructure. They retain separate
identity, evidence, preference, schema, replay, and reconciliation semantics.

## Regulatory Attachment

Regulatory services consume typed Domain and Control inputs and return typed
calculations, obligations, prohibitions, reports, and calendar results:

```text
Workflow -----------------------+
                                |
Domain command -> Regulatory capability
                                |
                 tax | wage/hour | leave
                 reporting | obligations
                                |
                                v
                  typed result / required step
                                |
                                v
                 Workflow and Domain transaction
```

Legal governance decides whether and under what constraints the organization
may act. Regulatory computation deterministically calculates what applies. A
government filing uses the Connectivity Plane after calculation, validation,
reconciliation, and approval.

## Data Subplanes

```text
                           DATA PLANE
                                |
       +------------------------+------------------------+
       v                        v                        v
    TRUTH                   SERVING                DISTRIBUTION
 ledger events          projections/cache       outbox/event streams
 content artifacts      search/semantic         consumer checkpoints
       |                        |                        |
 authoritative             rebuildable              replay-safe
                                |
                                v
                       ANALYTICAL DATA
                 OLAP / warehouse / lake projections
                           rebuildable
```

Rules:

- The ledger and authoritative artifacts are irreplaceable truth sources.
- Serving and analytical representations carry provenance, versions, and
  watermarks and are reconstructable.
- The Workflow Plane and agents do not directly mutate any of these stores.
- Domain repositories perform authoritative writes; projectors and consumers
  maintain derived planes.

## Intelligence Isolation

Intelligence normally consumes committed facts and observations asynchronously:

```text
domain transaction commits
         |
         +--> product response may complete
         |
         +--> search projector
         +--> analytical ingestion
         +--> semantic indexing
         +--> metric aggregation
         +--> outcome evaluation
```

If analytics, search, vectors, process mining, or dashboards are unavailable,
material transaction and security-critical workflows continue unless a
capability explicitly declares that derived result as a required governed
input. Staleness is exposed, never hidden.

## Agent Attachment

Agents are an overlay, not an authority bypass:

```text
                         AGENT RUNTIME
                               |
        +----------------------+----------------------+
        v                      v                      v
 Control discovery       Intelligence query     Workflow drafting
        |                      |                      |
        +----------------------+----------------------+
                               |
                         Governance Gateway
                               |
                    permitted Domain capabilities
```

An agent may discover, reason, explain, analyze, and draft within delegated
scope. Every data read, workflow action, message, or domain capability call is
independently governed. Deterministic Go services own authoritative execution.

## Operations, Assurance, and Billing Overlays

```text
                   OPERATIONS / ASSURANCE
      +------------+------------+------------+------------+
      v            v            v            v            v
   Identity      Workflow      Domain        Data        Agents

 telemetry | SLO | admission | reconciliation | integrity
 incident | repair | security | recovery | audit evidence
```

Billing observes semantic usage at stable customer-visible capability
boundaries. It does not insert a synchronous invoicing dependency into each
domain transaction. Entitlement checks may be synchronous governance inputs;
usage recording commits idempotently with, or is durably caused by, the
business execution.

## Go-Only Realization

```text
GWC / GoWebComponents        Experience implementation
grpcbridge                   Experience/API transport edge
Go capability services       Identity, Governance, Workflow, Domain,
                             Connectivity, Intelligence, Operations
SchemaFlux                   Control-plane definition compilation,
                             registries, compatibility and dependency artifacts
PostgreSQL                   Initial truth, runtime, serving, and outbox stores
```

All planes are implemented in Go. Logical plane separation does not imply one
microservice per plane; Phase 1 uses a modular Go platform plus separately
scalable Go workers where required.

## Dependency Invariants

1. Experience never bypasses Identity and Governance for protected behavior.
2. Workflow never directly mutates domain or projection tables.
3. Domain capabilities own HCM validation, invariants, transactions, and events.
4. Control configuration is immutable/versioned when used by an execution.
5. Connectivity records external intent and observation; it does not redefine
   canonical HCM state.
6. Serving, search, semantic, and analytical stores are reconstructable.
7. Intelligence cannot silently promote inference into domain truth.
8. Agents use governed capabilities and never receive hidden database or
   provider credentials.
9. Analytics, indexing, and reporting failures do not block unrelated material
   commands.
10. Operations, security, recovery, and billing evidence correlate across every
    plane without becoming the business ledger.
