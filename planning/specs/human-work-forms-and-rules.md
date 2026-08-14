# Human Work, Forms, and Business Rules

This specification defines three reusable business-interaction kernels that sit beside the [Workflow Execution Kernel](workflow-runtime.md). They are separate services because workflow structure, human work, data collection, and business decision logic evolve independently.

```text
                    WORKFLOW KERNEL
                          |
       +------------------+------------------+
       v                  v                  v
  HUMAN WORK            FORMS           BUSINESS RULES
 queues/tasks/SLA   questions/answers   expressions/tables
       |                  |                  |
       +------------------+------------------+
                          v
            typed decisions / artifacts / signals
```

The workflow coordinates. Human Work owns responsibility and completion. Forms own structured interaction and answers. Business Rules own deterministic customer logic. None owns canonical worker facts merely because it references them.

## Human Work Management

### WorkItem

```text
WorkItem

work_item_id
tenant_id
organization_scope

work_type
subject_refs[]

queue_expression
assignee_expression
resolved_queue_id?
resolved_candidates[]
assigned_principal_id?

status
priority
risk_class

input_artifact_refs[]
required_output_schema_ref
form_definition_ref?

due_at?
sla_policy_ref?
escalation_policy_ref?
delegation_policy_ref?

workflow_instance_id?
node_execution_id?
case_id?
correlation_id

created_at
claimed_at?
completed_at?
```

```text
CREATED -> ROUTED -> AVAILABLE -> CLAIMED -> IN_PROGRESS
                          |                        |
                          |                        +-> WAITING_INPUT
                          +-> ASSIGNED             +-> COMPLETED
                          +-> ESCALATED             +-> RETURNED
                          +-> EXPIRED               +-> CANCELLED
```

An approval is a specialized WorkItem whose output is an `ApprovalDecision` bound to an immutable proposal. A form does not become a task until a WorkItem assigns responsibility for completing it.

### Queues and Assignment

```text
WorkQueue

queue_id
scope
eligible_principal_expression
work_type_filter
claim_policy
assignment_strategy
capacity_policy
visibility_policy
business_calendar_ref
sla_policy_ref
```

Assignment strategies may be explicit, relationship/role resolved, round-robin, least-loaded, skill-qualified, geographic/timezone aware, or capability-resolved. They remain deterministic and explainable. Automated assignment cannot expand the candidate authority set.

```text
WorkItem created
      |
resolve queue + eligible candidates
      |
AuthZ / relationship / availability / capacity
      |
assign or expose for claim
      |
current-authority check at every material action
```

### Claim, Reassignment, Delegation, and Escalation

- Claim uses an atomic item version and optional lease to prevent double ownership.
- Reassignment preserves previous ownership and reason.
- Delegation uses the Platform delegation/proxy authority contract and may narrow but not expand scope.
- Escalation changes attention, priority, queue, or approver resolution according to workflow policy; it does not silently approve work.
- Separation-of-duties constraints apply to assignment and completion.
- Unavailable, departed, suspended, or unauthorized assignees trigger re-resolution rather than stranded tasks.

The [Messaging Plane](messaging-and-notification-plane.md) notifies assignees and returns delivery observations. It does not own assignment or SLA escalation.

### Completion

```text
WorkCompletion

work_item_id
item_version
completed_by
delegation_context?

output_artifact_ref
form_submission_ref?
decision_ref?
evidence_refs[]

authority_decision_ref
completed_at
```

Completion validates current authority, expected item version, required output schema, form version, evidence, separation of duties, deadline policy, and workflow correlation before emitting a typed workflow signal.

## Forms and Questionnaire Engine

### FormDefinition

```text
FormDefinition

form_id
version
name
purpose

subject_types[]
input_context_schema_ref
answer_schema_ref

sections[]
questions[]
repeat_groups[]

visibility_rules[]
required_rules[]
validation_rules[]
calculated_fields[]

attachment_policy
acknowledgement_policy?
signature_requirement?

locale_variants
classification
retention_policy_ref

status
effective_from
effective_to?
```

Supported question primitives should remain bounded:

```text
text | rich text display | number | money | date | date-time
boolean | single choice | multiple choice | lookup/reference
address | person | organization | file attachment | acknowledgement
```

Repeated groups, conditional visibility, requiredness, and validation compile into deterministic rules. Arbitrary browser/server code is prohibited.

### Render and Submission

```text
Form task
   |
resolve exact definition + locale + subject context
   |
field AuthZ + Legal + purpose + classification mask
   |
FormRenderPlan
   |
human answers + protected attachments
   |
server-side validation using same compiled rules
   |
FormSubmission (immutable)
   |
typed artifact / HumanTask completion / workflow signal
```

```text
FormSubmission

submission_id
form_id
form_version

subject_refs[]
respondent_principal_id
delegation_context?

render_context_hash
visible_question_ids[]
answer_artifact_ref
attachment_refs[]

validation_result_ref
acknowledgement_ref?
signature_evidence_ref?

submitted_at
supersedes_submission_id?
```

The system preserves what the respondent was shown, which questions were hidden, the exact locale/content/rules, answer revisions, and validation outcome. Form submissions are observations/claims until a separately authorized domain capability accepts them as canonical facts.

### Security and Accessibility

- Field visibility and editability are evaluated per respondent, subject, purpose, and current context.
- Hidden fields are not serialized to the client.
- Server validation is authoritative; client validation is usability only.
- Attachments pass the document ingestion/security pipeline.
- Sensitive repeat groups and conditional answers inherit explicit classification.
- Labels, errors, instructions, focus order, keyboard behavior, and assistive metadata are locale/version governed.
- A signature requirement invokes the document/e-signature subsystem; a checkbox is not a legal signature.

## Generic Business Rules and Decision Tables

Rules answer a typed question. They do not perform side effects or encode workflow topology.

```text
RuleDefinition

rule_id
version
name
purpose

input_schema_ref
output_schema_ref

rule_type
  EXPRESSION
  DECISION_TABLE
  FORMULA
  VALIDATION

body_ref
dependencies[]

effective_from
effective_to?
scope
owner
status
```

### Expression Subset

The language supports typed boolean/arithmetic/string/date operations, null-safe comparisons, bounded collection predicates, reference-data lookups, and named pure functions. It excludes network/database access, filesystem access, time/randomness without explicit inputs, unbounded loops, recursion, dynamic code loading, and agent/model calls.

```text
inputs + reference snapshots + rule version
                    |
                    v
              deterministic evaluator
                    |
                    v
typed result + trace + dependencies + warnings
```

### Decision Tables

```text
RaiseApprovalTable

inputs:
  raise_percent
  compa_ratio
  country

rows:
  country=US AND raise>10%       -> FINANCE_REQUIRED
  country=DE AND raise>8%        -> FINANCE_AND_HR_REQUIRED
  otherwise                      -> MANAGER_ONLY

hit policy:
  FIRST | UNIQUE | COLLECT
```

Compilation rejects ambiguous `UNIQUE` tables, uncovered required outputs, incompatible types, invalid effective intervals, cycles, unavailable reference versions, and formulas that violate bounded-computation limits.

### Rule Boundaries

```text
Workflow graph     HOW and WHEN work proceeds
Business rule      deterministic customer/business decision
Legal rule         law/policy obligation with legal provenance
AuthZ policy       whether a principal may act/access
Payroll/tax kernel specialized regulated numerical computation
Agent inference    derived hypothesis, never deterministic rule truth
```

A generic business rule cannot override mandatory Legal/AuthZ denies or reproduce regulated tax/payroll calculations merely because the expression language is capable of arithmetic.

## Shared Lifecycle and Promotion

Forms, rules, queues, and assignment policies use:

```text
DRAFT -> VALIDATED -> REVIEWED -> PUBLISHED -> ACTIVE
                                    |
                                    +-> QUARANTINED
                                    +-> DEPRECATED -> RETIRED
```

Publication includes schema compatibility, dependency impact, effective date, test fixtures, security/classification review, locale completeness where relevant, approval, immutable version, and rollback/forward plan. Running tasks/forms/rule evaluations remain pinned unless an explicit migration/re-resolution policy says otherwise.

## Capability Surface

```text
work.items.create
work.items.read
work.items.claim
work.items.assign
work.items.reassign
work.items.delegate
work.items.complete
work.items.cancel

work.queues.read
work.queues.publish
work.sla.evaluate

forms.definitions.validate
forms.definitions.publish
forms.render
forms.submissions.validate
forms.submissions.submit
forms.submissions.supersede

rules.validate
rules.simulate
rules.evaluate
rules.publish
rules.dependencies.explain
```

## Go-Only Implementation Shape

```text
internal/work/
  items/ queues/ assignment/ delegation/ sla/ projections/

internal/forms/
  definitions/ compiler/ renderer/ submissions/ validation/

internal/rules/
  definitions/ compiler/ expressions/ tables/ evaluator/ traces/

api/proto/work/v1/
api/proto/forms/v1/
api/proto/rules/v1/
```

Use Protobuf for contracts, PostgreSQL for durable state/versioning, SchemaFlux where useful for definition IR/generation, grpcbridge for web delivery, and GoWebComponents for accessible schema-driven task/form experiences. Prefer a small audited deterministic expression implementation over embedding a general scripting runtime.

## Phase Classification

| Capability                                 | Phase 1 depth                             |
| ------------------------------------------ | ----------------------------------------- |
| Approval/review WorkItem lifecycle         | **IMPLEMENT**                             |
| Queue, assignment, claim, reassignment     | **IMPLEMENT** to pilot depth              |
| Delegation, SLA, escalation                | **MINIMAL CONTRACT**                      |
| Promotion approval/reason form             | **IMPLEMENT**                             |
| General conditional/repeated questionnaire | **DESIGN / CONFORMANCE ONLY**             |
| Typed expression/decision-table compiler   | **MINIMAL CONTRACT** for pilot thresholds |
| Customer-authored arbitrary formulas       | **OUT OF PHASE**                          |
| Case/service-catalog specialization        | **OUT OF PHASE**                          |

## Phase 1 Acceptance Contract

- Approval/task responsibility survives worker-process failure and recipient relationship changes.
- Two principals cannot claim an exclusive WorkItem concurrently.
- Completion rechecks authority, proposal/item version, form schema, evidence, and separation of duties.
- The Promotion form preserves exact questions, visibility, locale, rules, answers, and submission evidence.
- Hidden/unauthorized fields never reach the browser or message template.
- Client and server validation use one compiled rule definition, with the server authoritative.
- Rules are deterministic, bounded, side-effect free, versioned, explainable, and simulation-safe.
- A business rule cannot bypass AuthZ, Legal obligations, approval binding, or execution revalidation.
- WorkItem, FormSubmission, RuleEvaluation, workflow node, communication intent, and ledger evidence are traversable through correlation identifiers.
