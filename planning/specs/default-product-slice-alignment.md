# Default Product Slice Alignment Contract

## Purpose and authority

This contract aligns three views of the production product:

1. **Experience view** — what a person can discover, understand and do.
2. **Business view** — which domain or workflow owns the meaning, decision and
   effect.
3. **Persistence view** — which PostgreSQL records make the state durable,
   queryable, auditable, recoverable and tenant-isolated.

The views are designed together but keep separate authority. A page is not a
business rule, a workflow is not a database transaction, and a table is not a
public product capability. A feature ships by default only when all three views
join through canonical identifiers, versions, time semantics, authorization,
provenance and tests.

This is a Gate C production contract. It does not broaden the Gate A read,
draft and simulation ceiling or the separately admitted Gate B Promotion
execution slice.

## The three-lens method

Every candidate default feature is reviewed in both directions:

```text
human job -> page state -> query/action -> business owner -> durable records
durable change -> event/projection -> authorized query -> page state -> human outcome
```

The first direction prevents storage-shaped UI and ownerless actions. The
second prevents durable facts that cannot be explained, operated or corrected
through the product.

For each state and action, reviewers answer:

| Experience question                                | Business question                                           | PostgreSQL question                                                         |
| -------------------------------------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------- |
| What job is the person completing?                 | Which capability, domain or workflow owns it?               | Which authoritative or derived record supports it?                          |
| What may the person see or do now?                 | Which authorization, policy and lifecycle decision applies? | Where are scope, policy version and decision evidence retained?             |
| What is current, proposed, simulated or completed? | Which semantic state transition is legal?                   | Which revisions, events and projections represent each state?               |
| What changed and why?                              | Which command, approval and transaction caused it?          | Which ledger, proposal, approval and provenance records prove it?           |
| What happens when data is stale or unavailable?    | Which fail-closed, retry, repair or manual route applies?   | Which versions, watermarks and reconciliation records expose the condition? |
| How can the person recover?                        | Which correction or repair capability is authorized?        | Which append-only correction and durable work records drive recovery?       |

No lens may fill a gap owned by another. The UI cannot infer an allowed action;
business code cannot infer tenant scope from a row identifier; PostgreSQL
defaults cannot invent a business value.

## Shared semantic spine

The following values cross all three lenses and therefore ship as platform
contracts:

- tenant, cell and placement epoch;
- principal, representation, delegation, purpose and organization scope;
- resource, capability, intent, proposal, workflow and work-item identifiers;
- schema, definition, policy, page and widget versions;
- revision, expected version, idempotency key and correlation identifier;
- effective, recorded, known, observed and requested time where applicable;
- lifecycle dimensions rather than one overloaded status;
- source authority, classification, provenance and retention disposition;
- ledger sequence/digest, projection watermark and reconciliation state;
- explanation-safe denial, stale, redacted, unavailable and unknown states.

These values have one canonical vocabulary. Protobuf owns transport shape,
domain contracts own meaning, and the storage manifests own physical mapping.
SSR, WASM, WebSockets, reports and exports consume the same vocabulary.

## What ships by default

The default package is the smallest safe, operable product that remains useful
before customer page customization. It includes:

### Product shell

- authenticated tenant/company context, delegated-acting indicator and session
  state;
- Home, My Work, People and Admin entry points admitted by authorization;
- global search and Action Finder over authorized, registered capabilities;
- accessible error, empty, stale, unavailable and interruption recovery states;
- locale, timezone, density, contrast and reduced-motion preferences;
- help, history, evidence and freshness patterns shared by every floorplan.

### Governed work loop

- discover and inspect an authorized object or task;
- start a registered BusinessIntent or direct read-only capability;
- save and resume a durable draft;
- validate and simulate without material effects;
- compare current and proposed values with provenance and freshness;
- review consequences and submit a proposal-bound decision;
- create, assign, claim and complete durable human work;
- track independent request, execution, business, consistency and obligation
  states;
- inspect the evidence timeline and use authorized correction or repair routes.

### Administration and operations

- configuration/version inventory and safe publication history;
- authorized policy, workflow, page and brand inspection;
- projection freshness, outbox, connector and reconciliation status;
- bounded repair workbench with proposal, review, execution and verification;
- privacy-safe telemetry correlation and operational evidence references.

### Default definitions

The installation includes platform-owned semantic tokens, floorplans, widgets,
page definitions, error vocabulary, lifecycle labels and workflow interaction
patterns. Domain-specific actions and pages are enabled only by an admitted
release manifest. An empty customer override still yields a coherent product.

## Default data package

The shared PostgreSQL substrate is grouped by semantic role, never exposed as
the navigation model.

| Storage role             | Default durable responsibilities                                                         | UI/business relationship                                                     |
| ------------------------ | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Registry/configuration   | versioned schemas, capabilities, pages, widgets, policies, brands and active pointers    | Resolves exact definitions; never grants authority by itself                 |
| Identity/tenancy         | tenants, cells, placements, principal/delegation references and scoped bindings          | Establishes trusted context before queries and actions                       |
| Intent/workflow          | intent instances, proposal revisions, workflow instances, node executions and work items | Drives drafts, simulation, decisions, My Work and tracking                   |
| Authoritative chronology | ledger streams/events, stream heads, approvals, transaction receipts and provenance      | Proves what was requested, decided and committed                             |
| External consistency     | outbox operations, observations, reconciliation findings and repair records              | Drives progress, discrepancy and recovery states                             |
| Critical projections     | authorized worker/task/intent summaries and action-discovery indexes                     | Serves bounded UI queries with source versions and watermarks                |
| Artifact references      | content digest, classification, custody and retention metadata                           | Supplies documents/evidence without placing large content in relational rows |
| Operational evidence     | bounded correlation, release, migration and recovery evidence                            | Supports operators without copying protected payloads into telemetry         |

Authoritative facts and history are append-oriented. Mutable rows are limited to
explicit coordination or active-pointer roles and use optimistic concurrency.
Derived projections declare a rebuild source and applied watermark. Caches,
search and browser storage never acquire authority.

## PostgreSQL invariants shared by every default slice

- Every tenant-scoped row carries `tenant_id`; applicable rows also bind cell
  and placement epoch. Row-level security and repository predicates both apply.
- IDs are opaque and never sufficient authorization. Queries begin with the
  resolved principal/delegation/purpose scope.
- Domain values have no SQL default unless the business contract explicitly
  defines that default. Absence, unknown, redacted and not-applicable remain
  distinct.
- Effective intervals are half-open. `timestamptz`, local dates and business
  periods are not substituted for one another.
- Money and rates use exact decimal representations and explicit currency,
  basis and rounding policy.
- Material changes append immutable revisions/events; corrections point to the
  superseded or corrected record rather than rewriting history.
- Public actions use expected versions and semantic idempotency. The local
  transaction atomically commits domain facts, ledger events, critical
  projections and outbox operations described by the transaction plan.
- Projection reads expose definition/schema version, source sequence,
  watermark, freshness and authorization disposition needed by the UI.
- Sensitive values are encrypted/classified according to their manifest and
  are never duplicated into unrestricted JSON, logs or metric labels.
- Every migration has expand/contract, restore and rebuild implications; a
  table is not production-ready merely because its migration succeeds.

## Canonical page-to-storage loops

| Product surface   | Business contract                                            | Durable source and projection                                     |
| ----------------- | ------------------------------------------------------------ | ----------------------------------------------------------------- |
| Home              | authorized attention and useful-start queries                | work/intent summary projections with watermarks                   |
| My Work           | assignment, claim, decision and completion capabilities      | work item, proposal binding, workflow and ledger records          |
| People lookup     | authorized resource discovery and field dispositions         | People-owned facts plus bounded authorized search projection      |
| Object page       | as-of domain query and semantic action discovery             | effective-dated domain revisions plus action/read projection      |
| Intent Workspace  | intent draft, validation, simulation and execution lifecycle | intent, proposal, workflow, ledger, projection and outbox records |
| Decision page     | proposal-bound human decision with SoD and expiry            | work item, approval binding, decision evidence and ledger event   |
| Timeline/status   | multidimensional lifecycle and provenance query              | ledger chronology plus critical lifecycle projection              |
| Operations/repair | observation, reconciliation and targeted correction          | operation, observation, finding, repair and verification records  |
| Experience Studio | governed definition validation/publication                   | immutable page versions and reconstructable active pointer        |

WebSockets carry only an authorized invalidation or attention hint with bounded
identifiers and versions. The client refetches the corresponding projection;
the message is never the source of display or workflow truth.

## ProductSliceDefinition

Every default-shipped feature is admitted through one machine-readable record:

```text
ProductSliceDefinition
  slice_id, version, owner, phase, default_disposition
  human_jobs[], personas[], routes[], floorplans[]
  business_intents[], capabilities[], workflow_definitions[]
  query_contracts[], command_contracts[], event_contracts[]
  authoritative_entities[], tables[], critical_projections[]
  authorization_inputs[], field_dispositions[], classifications[]
  effective_time_policy, correction_policy, retention_policy
  page/widget/brand/localization definitions[]
  failure_states[], repair_routes[], observability_contract
  migrations[], seeds[], fixtures[], tests[], evidence[]
  compatibility, rollout, rollback, rebuild and support boundaries
```

`default_disposition` is one of:

- `CORE_REQUIRED` — required for every supported installation;
- `DOMAIN_PACK_DEFAULT` — installed and enabled with an admitted domain pack;
- `AVAILABLE_NOT_ENABLED` — shipped but requires explicit customer activation;
- `CUSTOMER_DEFINED` — governed customer configuration using platform parts;
- `DEFERRED` or `PROHIBITED` — not present in a production release.

The compiler rejects a slice when any route/action lacks a business owner, any
material state lacks a durable disposition, any table lacks a consumer and
owner, or any protected field can reach a surface without an authorization and
classification decision.

## Default versus configuration

Customers may rename navigation, choose permitted page variants, arrange
registered widgets, select optional fields/actions, publish help content and
brand the experience. They may activate admitted domain packs and policies.

Customers may not:

- redefine canonical lifecycle or business meaning;
- bind UI controls directly to tables or arbitrary SQL;
- weaken field, population, purpose, retention or evidence constraints;
- convert a projection, report or cached value into an authoritative fact;
- remove required review, warning, explanation, recovery or accessibility
  behavior;
- publish an action whose BusinessIntent, capability, workflow, storage and
  transaction contracts are not admitted together.

## Release and conformance rule

A default slice is releasable only when a single scenario can prove:

1. a permitted principal discovers the correct surface and a denied principal
   learns nothing about it;
2. the SSR page and enhanced browser produce the same semantic state;
3. the displayed values name their source version, effective context and
   freshness without leaking internal storage details;
4. the action invokes the canonical capability with trusted context, expected
   version and idempotency;
5. the accepted transition produces the exact PostgreSQL records, ledger
   chronology, projection and outbox effects—or no write for read/simulate;
6. reconnect, replay, stale state, denial and duplicate submission remain safe;
7. rebuild/restore reproduces the authorized page and evidence timeline;
8. accessibility, localization, privacy, performance and observability gates
   pass for the same scenario.

Release evidence is keyed by slice/version and exact schema, definition,
policy, migration and binary digests. Passing isolated UI, domain or repository
tests is necessary but not sufficient.

## Initial delivery order

1. Contract the ProductSliceDefinition and shared semantic vocabulary.
2. Inventory existing platform tables, projections and UI definitions against
   the storage-role model; reject ownerless or consumerless defaults.
3. Build the shared query projection envelope and fail-closed authorization
   path used by SSR, gRPC and exports.
4. Close the governed work loop for the admitted Promotion prototype.
5. Seed the platform shell, Home, My Work, People lookup, Intent Workspace,
   Decision and status/timeline definitions.
6. Add migration/rebuild/restore and release-manifest gates.
7. Admit each later domain pack only through the same vertical proof.

## Alignment with controlling plans

| Source                                                       | Alignment                                                                                                                                                                 |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `planning/plan.md`                                           | Implements the shared Person/Worker Graph, BusinessIntent, authority, effective-time, workflow, ledger and evidence spine without making every module a system of record. |
| `planning/execution-plan.md`                                 | Keeps Phase 1 focused on Promotion and defers general default-product admission to Gate C.                                                                                |
| `planning/next-steps.md`                                     | Uses explicit SQL/pgx, authoritative migrations and one ACID chronology with restart/reconcile proof.                                                                     |
| `planning/specs/production-frontend-and-page-composition.md` | Supplies the durable and business counterpart for each floorplan and default page.                                                                                        |
| `planning/specs/experience-ui-and-branding.md`               | Preserves PageDefinition and widget authority boundaries and governed publication.                                                                                        |
| Domain contracts                                             | Keep facts, invariants, effective dating, correction and planned appends with their semantic owner.                                                                       |
| `planning/specs/business-intent-and-change-request.md`       | Uses the canonical request/proposal/lifecycle model rather than page-local form state.                                                                                    |
| `planning/specs/workflow-runtime.md`                         | Uses durable workflow and work-item state without allowing UI or tables to invent transitions.                                                                            |
| Transaction, ledger and reconciliation contracts             | Require atomic local chronology, external-effect evidence, observation, comparison and targeted repair.                                                                   |
| AuthZ, privacy, records and provenance contracts             | Apply authorization before projection and action, preserve purpose/classification/retention, and prevent inference through metadata.                                      |
| Storage dispositions and property mappings                   | Reuse declared table ownership, authority, rebuild and exact SQL mapping rather than introducing a parallel storage model.                                                |

## Explicit non-goals

- A database-first CRUD generator.
- One universal worker table or one universal status field.
- Client-side authorization or client-authored workflow truth.
- Arbitrary SQL in pages, reports, widgets or customer configuration.
- Enabling every planned HCM domain in the default installation.
- Treating seeded definitions as immutable product truth; they remain versioned,
  governed and replaceable through admitted publication.
