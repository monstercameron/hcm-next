# People and Workflow Authorization

## Status

This is the authorization and visibility contract for the production People,
Person Profile, My Work, Workflow History, and Promotion surfaces. It narrows
the generic rules in `organization-scope-and-authz.md` to the prototype that is
running today.

Authentication establishes an actor. It does not, by itself, disclose a person
or grant a workflow action. Routes, page labels, client-side stage checks, and a
tenant match are not authorization decisions.

## Decisions

1. The server filters people and workflows before serialization. The browser
   never receives a row that it is expected to hide.
2. Visibility and action authority are separate decisions. Seeing a person does
   not imply permission to promote them. Seeing a workflow does not imply
   permission to execute or decide it.
3. Population filtering happens before counts, facets, sorting, and pagination.
   Otherwise totals and page boundaries become employee-enumeration side
   channels.
4. Every material action is reauthorized when invoked. An action descriptor on
   a page is a current projection, not a durable bearer of authority.
5. Current and proposed organization scopes are evaluated independently. A
   promotion may cross the boundary of the initiating manager or HR partner.
6. Approval belongs to the routed WorkItem assignee. Another actor may decide
   only through a current, capability-bounded delegation or coverage grant.
7. The technical `promotion_operator` role authorizes execution machinery. It
   does not grant directory, compensation, or approval visibility by itself.
8. A resource that is absent and one that is outside scope produce the same
   external `NOT_FOUND` shape. Bulk queries silently omit unauthorized rows.
9. The policy version, relationship/source revisions, purpose, actor,
   delegation, and decision evidence reference are retained for every material
   read and action.

## Default participant policy

These are product defaults. Customer policy may narrow them but cannot widen
past platform, legal, separation-of-duty, or data-classification controls.

| Participant                  | People visibility                                                                                    | Workflow visibility                                                                           | Promotion actions                                                                         |
| ---------------------------- | ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Worker                       | Own current record and permitted fields                                                              | Workflows about self, with participant-safe status and evidence                               | None by default                                                                           |
| Manager                      | Current direct/indirect reports proven by an effective-dated manager-chain fact                      | Workflows they initiated or whose subject remains in their managed population                 | Propose for an in-scope worker; never gains execute or approval merely from being manager |
| HR partner                   | Workers in an effective-dated HRBP or assigned-population scope                                      | Workflows whose current or proposed scope intersects that assignment, subject to field policy | Propose for an in-scope worker                                                            |
| Compensation administrator   | Workers inside the granted organization/legal-entity scope, including authorized compensation fields | Promotion workflows in that scope                                                             | Propose; no automatic approval right                                                      |
| Routed approver              | No directory access from assignment alone                                                            | The minimum proposal-bound view required to make the assigned decision                        | Approve or reject the assigned WorkItem while its binding and authority remain current    |
| Promotion operator           | No people visibility from this role alone                                                            | Only execution-ready workflow facts needed by the execution capability                        | Execute a currently authorized proposal; cannot approve it                                |
| Auditor                      | Terminal history in granted audit scope; protected values masked or withheld                         | Immutable chronology and permitted evidence                                                   | Read only                                                                                 |
| Tenant or page administrator | Configuration metadata only                                                                          | None from the administrator role alone                                                        | None from the administrator role alone                                                    |

Holding several roles produces the intersection of mandatory restrictions and
the union of valid grants. Separation-of-duty exclusions are applied after that
union, so adding a role cannot make a requester eligible to approve their own
proposal.

## Resource decisions

### People collection

For every candidate worker, resolve:

```text
tenant boundary
  + principal organization grant
  + current worker assignment
  + manager / HRBP / population relationship at effective_at
  + requested purpose
  + requested fields and operations
  + policy and source watermarks
  -> include row + field dispositions, or omit row
```

Search terms are applied inside this authorized population. The response count
is the authorized count. Filter options contain only values present in that
authorized population. Compensation fields are omitted unless the field ruling
allows raw disclosure; a redacted ruling requires an actual masking transform
and never falls back to the raw value.

### Person profile

Each section is independently resolved. A caller may see identity and placement
without seeing pay, cases, documents, or analytics. A profile response carries
only disclosed sections and fields; it does not return blank placeholders for
protected data.

Contextual workflows are server-resolved for this actor and person. “Promotion”
appears only when the actor can discover the capability and the worker is in
the actor's proposal scope. Its invocation is checked again against current and
proposed organization scope when submitted.

### Workflow lists and history

A workflow is visible when at least one current rule permits it:

- the actor is the requester;
- the actor has a valid relationship to the subject;
- the actor owns a currently assigned WorkItem;
- the actor has an explicit workflow-instance/operator resource grant; or
- the actor has an audit/administrative grant for the workflow's organization
  scope and purpose.

The visible detail is the intersection of that relationship and field policy.
An employee participant view, an approver view, and an auditor view of the same
workflow are intentionally different projections. History never broadens access
after completion: access is reevaluated when the record is opened.

### Actions

The server returns semantic action descriptors only after evaluating all of:

```text
actor and verified session
delegation / acting context
tenant and organization scope
relationship to subject and workflow
capability and resource grant
purpose and field access
workflow stage and proposal revision
WorkItem assignment
separation of duties
assurance / step-up
policy version, validity window, and revocation epoch
```

Required action rules for Promotion:

| Action                                   | Minimum decision                                                                                                                                                                          |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `promotion.propose`                      | Manager, HR partner, or compensation administrator; subject in current scope; destination fields and proposed scope authorized; `compensation_review` purpose                             |
| `promotion.execute`                      | Valid execution authority plus `promotion_operator`; proposal revision current and approved; subject/workflow resource grant; no approval right implied                                   |
| `promotion.approve` / `promotion.reject` | Open WorkItem assignment authorizes the actor directly, or an evaluated non-expansive delegation authorizes acting for the assignee; proposal evidence visible; separation of duty passes |
| `promotion.inspect`                      | A current workflow visibility rule plus field-level projection                                                                                                                            |
| `promotion.audit`                        | Audit-purpose grant; terminal chronology only; protected fields masked/withheld according to policy                                                                                       |

## Service projection

The canonical gRPC response should expose server decisions, not enough input for
the browser to recreate them:

```text
AccessProjection
  policy_versions[]
  decision_evidence_ref
  evaluated_at
  expires_at
  relationship_kind
  acting_as / delegation_ref (when present)
  field_dispositions[]       SHOW | MASK | REDACT | HIDE | SUMMARY_ONLY
  available_actions[]

AvailableAction
  semantic_id
  capability_ref + version
  resource_ref
  proposal_revision_ref (when material)
  label_key
  required_assurance
  confirmation_kind
  obligations[]
```

Unavailable actions are absent unless their existence is safe and the actor can
resolve the condition. In that case the server may return a disabled descriptor
with a redaction-safe reason token. Clients do not derive actions from workflow
stage or role strings.

## Current implementation audit

The prototype already has useful enforcement primitives:

- the shared transport admits an authenticated `trust.Principal`;
- tenant-scoped stores prevent cross-tenant reads;
- `internal/trust/authz` supports tenant, relationship, population, purpose, and
  field decisions;
- the governed worker read evaluates field authorization;
- execution requires the configured `promotion_operator` authority;
- delegation and separation-of-duty evaluators exist;
- WorkItems carry routed assignment evidence.

The following gaps must be treated as open, not as UI polish:

1. `ListWorkers` returns the tenant/corpus population without per-worker scope
   filtering.
2. `ListJourneys`, `GetIntent`, and journey inspection are tenant-scoped but not
   subject-, participant-, or workflow-resource-scoped.
3. Workflow History therefore inherits tenant-wide visibility.
4. The clients derive Propose, Execute, Approve, and Reject controls from route
   and stage instead of consuming server-resolved actions.
5. `Decide` gates on execution authority and records a configured approver, but
   does not yet prove that the actor is that assignee or carries an evaluated
   delegation from them.
6. Relationship policy exists, but the journey query path does not load the
   manager-chain, HRBP, or assigned-population facts it requires.
7. Counts, pagination, workforce options, watch streams, and evidence reads need
   explicit noninterference tests after filtering is wired.

## Delivery slices

### Slice 1 — fail-closed server boundaries

- Introduce one subject/workflow authorization projection port in the
  application composition root.
- Filter People and journey collections before serialization and pagination.
- Reauthorize direct inspect and watch requests with uniform not-found results.
- Require direct WorkItem assignment or evaluated delegation for decisions.
- Add cross-tenant, out-of-population, guessed-ID, count, cursor, and timing-
  tolerant security tests.

Until relationship projections are composed, manager and HR-partner population
access must fail closed. The development principal may use an explicit scoped
compensation-administrator grant; a generic authenticated session may not fall
back to tenant-wide visibility.

### Slice 2 — server-resolved actions

- Add `AccessProjection` and `AvailableAction` to the canonical service
  contract.
- Make Person Profile workflow discovery and journey detail controls consume
  these descriptors.
- Remove stage-derived action invention from both Go/WASM clients.
- Reauthorize every submitted action and return typed stale-authority outcomes.

### Slice 3 — relationship and organization projection

- Consume current assignment, manager chain, HRBP assignment, explicit
  population, organization closure, and source/target scope watermarks.
- Evaluate both current and proposed effective-date scope.
- Invalidate cached decisions and push only filtered invalidations when those
  facts change.

### Slice 4 — enterprise operation

- Add delegation/coverage selection, step-up, break-glass, access explanation,
  policy simulation, periodic access review, and immutable decision evidence.
- Qualify exports, reports, notifications, search, browser storage, telemetry,
  and customer-composed pages against the same projection.

## Required acceptance scenarios

1. A manager sees only proven reports; an unrelated employee changes neither
   rows, totals, facets, cursors, nor autocomplete suggestions.
2. An HR partner loses access immediately when the effective-dated population
   assignment ends, including open tabs and watch reconnects.
3. A compensation administrator can propose in scope but cannot approve their
   own proposal when the WorkItem or separation-of-duty rule excludes them.
4. A promotion operator can execute an authorized revision without gaining
   directory or compensation browsing rights.
5. A routed approver sees the minimum proposal evidence and can decide only the
   bound WorkItem; guessing another intent ID yields the uniform not-found
   response.
6. An auditor can review terminal chronology with masked compensation and no
   mutation controls.
7. Revocation between page render and click removes the action and refuses the
   stale invocation without replaying it.
8. A source-to-destination organization move requires both scope decisions and
   does not leave either participant with unintended post-effective access.
9. SSR, Go/WASM, direct gRPC, WebSocket watch, export, logs, metrics, and traces
   expose the same authorized population and no protected values.
