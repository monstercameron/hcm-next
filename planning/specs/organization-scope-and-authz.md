# Organization Scope and Authorization Contract

## Purpose

Organization is a temporal relationship graph and an authorization input. It
is not a single `department_id`, and it is separate from the tenant security
boundary.

```text
Tenant
  |
  +-- companies / legal entities / operating units
  |       |
  |       +-- departments / teams / locations / cost centers
  |
  +-- projects / programs / committees / representative bodies
          |
          v
authorized assignment projections + relationship projections + scoped role bindings
          |
          v
explainable AuthorizationScope
```

## Organization Vocabulary

The model supports typed, extensible organization units, including:

```text
enterprise      legal_entity      employing_entity
payroll_unit    business_unit     division
department      team              location
region          cost_center       project
program         clinic            store
franchisee      supplier          joint_venture
board           committee         works_council
union           volunteer_group   member_group
```

Types control validation and semantics; they do not force every customer into
one hierarchy.

## Relationships

Organization edges are typed and effective-dated:

```text
part_of | reports_to | owns | controls | employs | operates
funds | governs | represents | franchises | supplies
located_in | allocated_to
```

Every edge includes source, target, type, effective interval, provenance, and
version. Graph validation rejects prohibited cycles and cardinality violations
while allowing legitimate matrix relationships.

## Authorized Assignment Projection

This AuthZ service owns no Worker/Employment/Assignment lifecycle.

```text
People Domain            owns Assignment identity, revisions, effective dates,
                         status, commands, events, and correction

Position Domain          owns Occupancy and capacity consumption

Organization Domain      owns typed manager/reporting/organization relationships

Scoped AuthZ             consumes authorized projections of those facts
```

The authorization projection contains only fields required for scope evaluation:

```text
assignment_id
worker_id / employment_id
assignment_type
organization / legal_entity / location / cost_center / project refs
effective_interval
people_stream_sequence
organization_graph_watermark
position_occupancy_watermark?
projected_at
projection_schema_version
```

Projection states such as `ACTIVE`, `SUSPENDED`, `ENDED`, and `SUPERSEDED` are
mapped from People-owned state; AuthZ cannot transition them. The projection
subscribes to People assignment, Organization relationship, and Position
occupancy events, records source sequences/watermarks, supports independent
rebuild/reconciliation, and invalidates affected decision caches.

Every evaluation declares `effective_at` and required freshness. Reads may return
an explicitly stale/limited answer only where policy permits it. Material writes,
approvals, delegation, and scope-changing operations fail closed when required
assignment/relationship watermarks exceed their maximum age or disagree with a
canonical direct resolution.

## Role Bindings and Scope

```text
RoleBinding

principal
role
scope_expression
effective_interval
grant_provenance
delegation?
conditions?
```

Scope expressions may include:

```text
self
direct_reports
manager_chain
organization + descendants
legal_entity
location / country
cost_center
project / assignment
workflow instance
global tenant
```

The authorization decision combines:

```text
principal + delegated authority
          |
capability and resource
          |
current/proposed organization relationships
          |
record + field + data domain
          |
purpose + channel + context + risk
          |
policy version
          v
ALLOW | DENY | OBLIGATIONS
```

## Evaluation Contract

At minimum the engine:

1. Authenticates and resolves the principal and tenant.
2. Resolves direct, group, role, relationship, and delegated grants.
3. Resolves the resource and its current authority/source.
4. Resolves current, target, and effective-date organization scopes.
5. Applies capability and resource constraints.
6. Applies record, population, data-domain, and field restrictions.
7. Applies purpose, legal, risk, channel, and environment constraints.
8. Applies inherited mandatory denies and obligations.
9. Enforces separation of duties and step-up requirements.
10. Produces an explainable decision and enforceable
    `AuthorizationScope`.
11. Revalidates at material execution time.

Repositories handling sensitive domains require the evaluated scope; callers
cannot fetch a broad record and remove fields afterward.

## Current and Proposed Scope

A transfer or promotion can change the relationships that grant authority.
Therefore a plan may require both source and target visibility:

```text
Source HR / manager ------ current assignment
            \              /
             TransferPlan
            /              \
Target HR / manager ------ proposed assignment
```

Policy declares which participants may see the proposal, current facts,
destination facts, and post-effective record. Approval receipt alone does not
grant continuing access.

## Approval Resolution

Organization relationships feed Human Work resolver expressions such as:

```text
ManagerOf(worker)
HRBPFor(target_org)
FinancePartnerFor(target_cost_center)
LegalApproverFor(destination_legal_entity)
```

The task recipient is snapshotted for delivery, while decision-time authority
is reevaluated according to the approval validity policy.

## Required Evidence

Every authorization decision records or references:

- Principal and delegation chain.
- Capability, resource, fields, and purpose.
- Current and proposed organization inputs.
- Relationship and role-binding versions.
- Policy version/fingerprint.
- Obligations, hidden/masked fields, and reason codes.
- Decision time and correlation identifiers.

## Phase 1 Depth

Consume and enforce the People-owned assignment projection, Organization-owned
manager/relationship projection, organization units,
cost-center scope, and source/target evaluation required by the Promotion and
HarborCare transfer conformance scenarios. Keep the broader organization type
catalog and relationship vocabulary as contracts. Matrix organizations,
representative bodies, acquisitions, and complex cross-tenant sharing remain
deferred.
