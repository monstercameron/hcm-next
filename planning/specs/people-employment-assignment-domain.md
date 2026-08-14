# People, Employment, and Assignment Domain Contract

This domain owns the bounded business meaning of a promotion. It separates the
human, workforce participation, contractual employment, and effective-dated work
assignment instead of treating `worker` as one mutable document.

## Authority and Entity Model

```text
Person
  one human identity; may predate and outlive employment
   |
   +-- Worker
   |     tenant workforce participation and worker number
   |
   +-- Employment
         contractual relationship with legal entity
           |
           +-- Assignment
                 job / level / org / manager / position / location / FTE
```

Candidate, employee, contractor, former worker, and internal applicant are
non-exclusive roles/relationships. A Person may have multiple sequential or
simultaneous Employments; each Employment may have primary, temporary, acting,
matrix, project, or secondary Assignments according to policy.

Canonical objects:

```text
Person              person_id, HCM person facts and opaque resolution_ref
Worker              worker_id, tenant, worker number, lifecycle status
Employment          employment_id, legal entity, worker type, contract interval
Assignment          assignment_id, type, effective interval, FTE, status
AssignmentRevision  job, level, org, manager_relationship_ref, position, location and source
```

Identity ownership is explicit:

- The Identity Resolution Service owns matching claims, normalized matching
  values, verified identifiers used for linkage, do-not-merge constraints,
  external identity links, candidate scores/sets, and protected matching evidence.
- People owns HCM person-domain facts only where SourceAuthority grants it, such
  as legal/preferred name, pronouns, or contact channels required by the selected
  HCM capability.
- People stores opaque `resolution_id`/`resolution_ref` and permitted external
  aliases, not copied candidate sets, matching signals, government-ID evidence,
  or normalized matcher features.
- Promoting a claim into a People fact is a separate authorized
  `people.person_fact.propose|accept` operation with source, purpose,
  classification, authority decision, provenance, and effective time.
- Correcting a People fact may trigger identity re-evaluation but never rewrites
  the historical claim or match decision; a new claim/decision supersedes it.

The People Domain owns a fact only where effective-dated SourceAuthority grants
it ownership. Otherwise it owns the proposal/transaction and records incumbent
facts as `EXTERNAL_OBSERVATION`.

## Lifecycle and Invariants

```text
Employment: PROPOSED -> ACTIVE -> SUSPENDED? -> ENDED -> REINSTATED?
Assignment: PLANNED  -> ACTIVE -> SUPERSEDED | ENDED | CANCELLED
```

- Entity identity is stable; role and status changes do not create a new Person.
- Effective intervals are half-open `[from,to)` and never silently overlap where
  assignment policy requires one primary assignment.
- An assignment's legal entity must be compatible with its Employment.
- Job, level, org, location, and manager references must be active for the
  interval or have an approved future version.
- Position-backed assignments must reconcile to Position Occupancy and consume
  declared FTE/capacity exactly once.
- Manager authority is owned by the [Organization and Relationship Domain](organization-and-relationship-domain.md). Assignment stores only
  `manager_relationship_ref` and an authorized projection watermark; it cannot
  independently mutate a manager person field.
- Manager and organization relationships cannot introduce forbidden cycles.
- `Former Worker` is derived from employment history, not an exclusive Person
  state.
- Correction appends a superseding assertion and preserves what was previously
  recorded and acted upon.

## Commands, Queries, and Events

```text
people.person.read
people.worker.read|timeline
people.employment.read|create|suspend|reactivate|end|reinstate
people.assignment.read|simulate_change|change|cancel_future|correct
people.promotion.simulate|execute
```

Person matching/linkage is delegated to the [Identity Resolution and Entity-Linkage
Service](identity-resolution-and-entity-linkage.md). The People Domain accepts a
resolved `person_id` plus resolution evidence; it does not run an independent
fuzzy matcher or merge identities.

Ordinary `people.person.read` and `people.worker.read` repositories are prohibited
from joining protected identity-resolution claims, candidate sets, or match scores.
Those require separate Identity Resolution capabilities and purpose. Repository,
API, search, export, agent, and audit-view tests enforce this boundary.

Manager-chain queries use `organization.relationships.manager_of|manager_chain`
from the [Organization and Relationship Domain](organization-and-relationship-domain.md).
If People exposes a convenience façade, it delegates to that capability and
returns its relationship ID, resolver-policy version, and graph watermark; it
does not implement a second resolver.

Events include `PersonResolved`, `WorkerCreated`, `EmploymentCreated`,
`EmploymentSuspended`, `EmploymentEnded`, `EmploymentReinstated`,
`AssignmentCreated`, `AssignmentChanged`, `AssignmentEnded`,
`AssignmentCorrectionRecorded`, and `WorkerPromoted` as a semantic transaction
summary referencing the underlying planned appends.

Domain command handlers validate the input, source authority, expected stream
sequences, effective-date conflicts, reference versions, Position occupancy,
AuthZ/legal obligations, and invariants. They return typed planned appends and
projection changes to the Multi-Stream Transaction Coordinator. Workflow cannot
construct these events.

## Failure, Security, and Evidence

Typed failures distinguish identity ambiguity, inactive employment, invalid
assignment/reference, position/capacity conflict, manager cycle, source-authority
denial, stale baseline, overlapping future change, AuthZ/legal block, and
reapproval requirement. No default assignment is invented.

AuthZ evaluates tenant, owning and proposed organization, legal entity,
relationship, worker population, fields, purpose, current/proposed state, and
effective time. Person identity claims, immigration, medical, investigations,
and payroll/bank data remain separate compartments and are not loaded merely
because a promotion targets the worker.

Evidence binds intent/proposal, person/worker/employment/assignment IDs, prior and
proposed revisions, reference and Position versions, source authority, write set,
expected sequences, AuthZ/legal decisions, approval hash, transaction/events,
external write/observation, reconciliation, and correction.

## Phase Depth and Acceptance

Gate A implements authorized reads, timeline, assignment-change simulation, and
incumbent observations. Gate B implements only the promotion-required job, level,
manager, organization, and position-assignment writeback/transaction slice.
Employment creation/end and identity merge remain conformance-only.

Acceptance includes current and future assignment, simultaneous employment,
temporary assignment, stale baseline, concurrent transfer, manager cycle,
position mismatch, correction, field redaction, incumbent-authority writeback,
and multi-stream atomicity fixtures.

All implementation is Go with Protobuf contracts, grpcbridge edges, GWC views,
and SchemaFlux-compiled schema/conformance artifacts.
