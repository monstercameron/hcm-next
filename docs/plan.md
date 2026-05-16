# HCM Next Master Plan

## 1. Refined Verdict

HCM Next is a big, venture-scale idea, but the first version must not try to be Workday, UKG, ServiceNow, n8n, Retool, an AI agent platform, and payroll infrastructure all at once.

The refined wedge:

> HCM Next is the vendor-agnostic transaction control plane for enterprise HR changes. It lets complex companies design, approve, simulate, execute, reconcile, and audit employee changes across UKG, Workday, payroll, identity, finance, and internal systems, with permission-aware AI reviewing every step.

The long-term endgame can still be a programmable, AI-native HCM system of record that replaces legacy suites like UKG Pro. But the day-one pitch should not be rip-and-replace.

Day-one pitch:

> Keep your HCM. Replace the chaos around it.

The product should start as **HCM Next ChangeOps**: a governed platform for high-risk employee changes.

## 2. Category

Best category name:

> HCM Transaction Control Plane

Buyer-friendly category:

> Enterprise HR ChangeOps Platform

Operational category:

> People Operations Change Management Platform

Sales phrase:

> The control layer for employee changes across enterprise HCM systems.

HCM Next is not a generic workflow builder, not a chatbot for HR, not a payroll engine, not a benefits platform, and not a ticketing system.

It is:

> The enterprise control plane for HR transactions.

## 3. Customer Pain

The buyer does not wake up wanting metadata-first HCM, workflow graphs, or AI agents.

They wake up with problems like:

- Promotions take three weeks.
- Compensation changes miss payroll cutoff.
- Manager changes break downstream systems.
- Reorgs happen in spreadsheets.
- Every country or business unit has a different approval path.
- HRIS teams manually patch five systems.
- Nobody can explain who approved what.
- HR processes rely on email, tickets, spreadsheets, Slack, and custom scripts around the system of record.
- AI sounds useful, but legal and security will reject it unless it is permissioned and auditable.

The core customer desire:

> Change employee data safely, quickly, and auditably across messy enterprise HR stacks.

## 4. Target Customer

Google-scale enterprise remains the architectural stress test, not the first buyer.

The first ideal customer profile:

> 2,000-25,000 employee companies with complex HR operations, multiple entities, frequent org changes, and an HRIS team drowning in manual workflows around UKG, Workday, Oracle HCM, SAP SuccessFactors, Dayforce, or internal systems.

Best early verticals:

| Vertical                  | Why It Fits                                                          |
| ------------------------- | -------------------------------------------------------------------- |
| Healthcare                | Complex roles, credentialing, departments, payroll sensitivity       |
| Retail / hospitality      | High-volume employee changes, frontline complexity, payroll pressure |
| Manufacturing / logistics | Multi-location, shift-based, union and role complexity               |
| Tech / SaaS with M&A      | Frequent reorgs, comp changes, matrix structures                     |
| Financial services        | Strong audit, approval, compliance, and access control needs         |

The first buyer champion is likely:

> Head of HRIS / People Systems

Buyer committee:

| Buyer              | What They Care About                              |
| ------------------ | ------------------------------------------------- |
| VP People Ops      | Faster changes, fewer manual escalations          |
| Head of HRIS       | Less configuration pain, fewer tickets            |
| CIO / IT           | Fewer shadow tools, safer integrations            |
| Payroll leader     | Fewer downstream errors                           |
| Legal / compliance | Auditability, permissions, policy enforcement     |
| CFO                | Reduced operational waste and implementation cost |

## 5. Positioning

Primary positioning:

> HCM Next helps complex enterprises safely change employee data across HCM, payroll, finance, identity, and internal systems. It turns job changes, compensation changes, manager changes, and reorgs into governed, auditable transactions with configurable workflows, generated approval experiences, transaction simulation, reconciliation, and permission-aware AI review.

Sharp positioning:

> Keep your HCM. Replace the chaos around it.

Investor one-liner:

> HCM Next is the transaction control plane for enterprise HR, starting with high-risk employee changes and expanding into the programmable system of record.

Long-term vision:

> Overlay -> control plane -> transaction authority -> domain system of record -> full programmable HCM core.

## 6. Product Spine

The prior product spine was:

```text
Database record -> Select workflow -> UX interaction -> Transactions -> Review change
```

For the wedge, the clearer product spine is:

```text
Proposed HCM change -> Preflight validation -> Approval workflow -> Transaction plan -> Writeback/reconciliation -> Audit review
```

This centers the product on the object buyers understand and budget against:

> The HCM Change Request

## 7. Core Product Object: HCM Change Request

The HCM Change Request is the central object in v1.

It represents a proposed mutation to employee-related data that must be validated, approved, executed, reconciled, and audited.

Examples:

- Job change
- Title change
- Promotion
- Compensation change
- Manager change
- Department change
- Cost center change
- Position change
- Combined promotion and compensation change
- Reorg batch, later

An HCM Change Request should contain:

- Request ID
- Tenant
- Request type
- Requester
- Target employee or worker
- Effective date
- Business reason
- Current state
- Proposed state
- Impacted fields
- Impacted systems
- Required approvals
- Preflight validation results
- AI change review
- Transaction plan
- Execution status
- Reconciliation status
- Audit trail
- Failure and repair status

Primary lifecycle:

```text
Draft -> Preflighted -> Submitted -> In approval -> Approved -> Simulated -> Executed -> Reconciled -> Reviewed -> Closed
```

Failure and exception lifecycle:

```text
Needs data -> Rejected -> Canceled -> Failed -> Waiting repair -> Resolved -> Superseded
```

## 8. MVP: HCM Next ChangeOps v1

The first product should be narrow, serious, and mission-critical.

### 8.1 Core Objects

Build only the primitives needed for employee changes:

| Object                | Why                               |
| --------------------- | --------------------------------- |
| Person                | Identity anchor                   |
| Worker / employee     | Worker context                    |
| Employment            | Status, legal entity, dates       |
| Job                   | Job profile, family, level        |
| Position              | Seat, reporting, budget           |
| Org unit / department | Routing and scope                 |
| Manager relationship  | Relationship access and approvals |
| Compensation record   | High-value transaction            |
| Change request        | Central product object            |
| Approval task         | Workflow execution                |
| Transaction plan      | What will change                  |
| Event ledger entry    | Audit and replay                  |

Do not build the entire HCM object universe in v1.

### 8.2 First Workflows

Ship exactly these first:

| Workflow             | Why First                                             |
| -------------------- | ----------------------------------------------------- |
| Job / title change   | Common, understandable, medium risk                   |
| Compensation change  | High value, approval-heavy, payroll-sensitive         |
| Manager / org change | Proves relationship permissions and downstream impact |
| Combined promotion   | Combines job, comp, manager/org, and effective dating |

Delay these:

| Later Workflow        | Why Later                                            |
| --------------------- | ---------------------------------------------------- |
| Reorg batch           | Needs batch simulation and heavier reconciliation    |
| Leave / accommodation | Medical, privacy, and legal complexity               |
| ATS conversion        | AI and employment-decision risk                      |
| Offboarding           | Identity, payroll, legal hold, and access complexity |

### 8.3 First UX

Build four product surfaces first:

1. Manager request form
2. HRBP / compensation approval view
3. Transaction simulation view
4. Audit / review timeline

Do not build a full visual workflow canvas first. Build schema-first workflows with a simple admin editor. A canvas can come later.

### 8.4 First AI Scope

AI should do only four things in v1:

1. Summarize what is being changed.
2. Detect missing data and policy conflicts.
3. Explain downstream impact.
4. Draft the approval and audit review.

AI should not execute material HR transactions in v1.

### 8.5 First Integrations

Start with:

1. CSV/SFTP import and export
2. REST and webhook adapter
3. One HCM integration, likely UKG or Workday depending on design partners
4. Slack / Teams / email notifications
5. Payroll export after the core change flow works
6. Identity sync after the manager/org flow works

Do not overbuild integrations before buyer validation. Enterprise integration work can consume the company.

## 9. What To Cut From V1

Cut aggressively.

| Feature                         | Decision     | Why                                      |
| ------------------------------- | ------------ | ---------------------------------------- |
| Full UKG replacement            | Delay        | Too hard and terrifying to buy first     |
| Payroll engine                  | Do not build | Integrate first                          |
| Benefits enrollment             | Delay        | Complex, seasonal, regulated             |
| Time / attendance / scheduling  | Delay        | Incumbents are strong and domain is deep |
| ATS conversion                  | Delay        | AI hiring compliance risk                |
| Leave / accommodation           | Delay        | Medical, privacy, legal complexity       |
| Visual workflow canvas          | Delay        | Expensive, schema-first is enough        |
| Customer-written Go blocks      | Limited beta | Powerful but scary early                 |
| Talent / performance / learning | Cut          | Not needed for wedge                     |
| AI autonomous execution         | Cut          | Risky and unnecessary                    |
| Full analytics suite            | Cut          | Build operational dashboards only        |
| Marketplace                     | Cut          | No ecosystem yet                         |

## 10. Product Modules

### 10.1 Change Request Hub

Create, track, approve, execute, reconcile, and audit employee changes.

### 10.2 Workflow Router

Configurable routing by:

- Role
- Relationship to employee
- Org unit
- Legal entity
- Geography
- Compensation threshold
- Changed field
- Effective date
- Policy rule
- Payroll cutoff

### 10.3 Transaction Simulator

Preview exactly what will change before execution.

The simulator should show:

- Current state
- Proposed state
- Field-level changes
- Effective-date impact
- Approval requirements
- Payroll impact
- Finance impact
- Identity impact
- Downstream systems touched
- Validation failures
- Reconciliation risks

### 10.4 AI Change Review

Permission-aware AI summarizes:

- What is changing
- Why it matters
- Missing data
- Policy conflicts
- Approval path
- Downstream impact
- Risk areas
- Suggested next actions

The console additionally ships an AI-generated workflow UI surface (floating HR-assistant panel) that turns a plain-English request into a `PageDefinition` grounded in the workflow JSON. See [ai-ui-generation.md](ai-ui-generation.md).

### 10.5 HCM Change Ledger

Immutable audit trail of:

- Requests
- Approvals
- AI visibility
- Permission decisions
- Transaction plans
- Internal writes
- External system calls
- Failures
- Retries
- Reconciliation results
- Manual repair actions

### 10.6 Integration And Reconciliation Layer

Syncs or exports changes to:

- HCM
- Payroll
- Finance
- Identity
- Internal APIs
- Data warehouse
- Notification systems

### 10.7 Embedded Approval Widget

Approvals can happen inside:

- Internal portal
- HR workspace
- Slack / Teams-like flow
- Manager workspace
- Customer-owned platform

## 11. Demo Story

The demo should show pain disappearing, not a generic workflow builder.

Demo:

1. A manager opens an employee profile or embedded widget.
2. HCM Next knows the manager relationship and permitted fields.
3. Manager selects "promotion / compensation change."
4. The generated form asks only for relevant fields.
5. The system preflights job level, pay band, budget, effective date, payroll cutoff, and missing justification.
6. AI generates a plain-English change brief.
7. The approval workflow routes to manager's manager, HRBP, compensation, and finance based on policy.
8. HCM Next simulates the transaction before execution.
9. Once approved, it writes or exports the change to the HCM/payroll system.
10. The ledger shows who approved, what changed, what systems were touched, what AI saw, what failed, and what still needs repair.

Example AI brief:

> This change moves Jane from L4 to L5, increases salary by 12%, remains within band, requires HRBP and Compensation approval, and will affect cost center reporting next pay period.

## 12. Data Model Philosophy

Use a hybrid model.

Detailed storage strategy:

> See [Data Model And Storage Strategy](data-model-storage-strategy.md).

```text
Canonical HCM primitives
+ tenant metadata extensions
+ effective-dated facts
+ governed relationships
+ policy and permission overlays
+ immutable event ledger
```

Why:

- Fully schema-less HCM becomes chaos.
- HR data has legal, payroll, reporting, and compliance meaning.
- Enterprises still need custom fields, custom rules, custom job architectures, custom policies, and custom integrations.

Canonical primitives:

- Person
- Worker
- Employment
- Job
- Position
- Org unit
- Location
- Manager relationship
- Compensation
- Change request
- Approval task
- Transaction plan
- Event ledger entry

Extensions:

- Custom fields
- Custom objects, later
- Custom relationships
- Custom validations
- Custom picklists
- Custom policy rules
- Custom approval routes
- Custom integration mappings
- Custom AI review scopes

Metadata must be governed, versioned, validated, permission-aware, and auditable.

## 13. Workflow Model

Workflows are graphs, not scripts.

User mental model:

```text
One initiating change request -> one primary business outcome
```

Internal model:

```text
Many paths, branches, joins, side effects, retries, outputs, and child workflows
```

Rule:

> Every workflow has one primary business goal and one primary business outcome, but may produce many governed outputs, side effects, records, events, documents, integration calls, and child workflows.

Workflow graph concepts:

- Start node
- Primary outcome
- Secondary outputs
- Branches
- Joins
- Parallel branches
- Side-effect branches
- Child workflows
- Terminal states
- Checkpoints
- Retry policies
- Compensation paths
- Manual repair paths

## 14. Block Library

Workflow blocks should be typed, small, inspectable, and permission-aware.

Each block declares:

- Inputs
- Outputs
- Permissions required
- Data read
- Data written
- External systems touched
- AI visibility scope
- Failure behavior
- Retry behavior
- Audit classification
- Runtime cost profile

Core block types:

- Trigger block
- Record context block
- Record lookup block
- Mapping block
- Data transform block
- Calculation block
- Validation block
- Policy evaluation block
- Eligibility block
- Compliance block
- Privacy and redaction block
- Conditional logic block
- Gate block
- Parallel branch block
- Merge block
- Loop block
- Human input block
- Human task block
- Approval block
- Delegation block
- AI analysis block
- AI recommendation block
- Risk scoring block
- Impact analysis block
- Forecast block
- Integration block
- Credential block
- Notification block
- Wait block
- SLA and escalation block
- Transaction block
- Effective dating block
- Reconciliation block
- Rollback block
- Subworkflow block
- Batch operation block
- Review block
- Audit block
- Exception block

## 15. Enterprise Extension Framework

The custom Go block idea is powerful, but should not be the first HR-buyer pitch.

Public language:

> Enterprise extension framework

Technical buyer language:

> Signed, versioned, capability-scoped custom logic modules for HR transaction workflows.

HR buyer language:

> Your IT team can safely encode company-specific rules and integrations without waiting for vendor custom work.

Long-term programming model:

```text
Small typed block -> pure-ish function -> typed output -> graph wiring by config
```

Preferred first language:

> Go

Why Go:

- Fast
- Statically typed
- Easy to compile
- Easy for contractors to learn
- Good for small business-logic modules
- Strong fit for high-performance backend workflows

Custom block shape:

```go
type Input struct {
	EmployeeID    string  `json:"employeeId"`
	ProposedLevel string  `json:"proposedLevel"`
	ProposedSalary float64 `json:"proposedSalary"`
}

type Output struct {
	Passed   bool     `json:"passed"`
	Reasons  []string `json:"reasons"`
	Warnings []string `json:"warnings"`
}

func Run(ctx hcm.Context, in Input) (Output, error) {
	return Output{}, nil
}
```

Custom block lifecycle:

1. Internal adapter blocks built by HCM Next.
2. Design-partner custom blocks built with HCM Next.
3. Certified partner/customer extension SDK.
4. Marketplace, much later.

Security lifecycle:

```text
Upload/connect module -> validate manifest -> scan -> compile -> sign -> test -> publish -> execute with declared capabilities
```

Avoid Go's native in-process plugin model for customer code.

Preferred runtimes:

- WASM/WASI for sandboxed custom blocks
- Locked-down native worker container for heavier enterprise blocks
- Tenant-specific worker pools for large customers

## 16. HCM Change Ledger

The HCM Change Ledger is a core product feature, not backend plumbing.

Pitch:

> For every employee change, we can show who requested it, who approved it, what policy allowed it, what data changed, what systems were touched, what AI saw, what failed, what was retried, and what is safe to reverse.

Each ledger event should record:

- Tenant
- Actor
- Actor role
- Actor relationship to record
- Delegated actor, if any
- Service account, if any
- AI agent identity, if any
- Workflow definition version
- Workflow instance ID
- Block name and version
- Request ID
- Correlation ID
- Idempotency key
- Permission decision snapshot
- Before state
- After state
- Business reason
- External calls
- External responses
- Failure reason, if any

The ledger powers:

- Audit
- Replay
- Reconciliation
- Rollback
- Compensation
- AI review
- Compliance
- Debugging
- Customer trust

## 17. Failure Management

Failed transactions are first-class workflow outcomes.

The platform must answer:

- Who initiated it?
- Who approved it?
- Who executed it?
- Which workflow, block version, AI agent, service account, or integration caused the action?
- What changed before failure?
- What did not change?
- What can be retried?
- What can be reversed?
- What requires manual repair?
- Which systems are now out of sync?

Failure states:

- Failed before mutation
- Failed after partial mutation
- Failed external call
- Failed validation
- Failed permission check
- Failed AI policy check
- Failed approval
- Failed timeout
- Failed rollback
- Failed compensation
- Waiting for manual repair
- Resolved manually
- Resolved automatically
- Superseded

Reversibility models:

- Fully reversible
- Reversible with compensation
- Reversible only before external sync
- Manually reversible
- Not reversible

Enterprise failure capabilities:

- Idempotency keys
- Checkpoints
- Dry runs
- Retries
- Dead letter queues
- Circuit breakers
- Rate-limit handling
- Backpressure controls
- Manual repair console
- Rollback
- Roll-forward repair
- Compensation transactions

Product standard:

> If a compensation change fails during payroll sync, the platform must show exactly what happened, what changed, who or what caused it, what is safe to retry, what must be reversed, and what needs a human decision.

## 18. Permission-Aware Context Engine

Permissions are not optional infrastructure. They are part of the product.

Core claim:

> AI and workflows only see what the actor is allowed to see.

Access model:

```text
Access = role permissions
       + relationship to record
       + business attributes
       + field sensitivity
       + action permissions
       + workflow state
       + tenant policy
       + AI policy
```

Required permission layers:

- RBAC
- Relationship-based access
- Attribute-based access
- Field-level permissions
- Action-level permissions
- AI visibility scopes
- Service account scopes
- Delegation rules
- Break-glass access

Examples:

- Managers can see operational impact, but not unrelated sensitive data.
- Compensation approvers can see pay data only for relevant requests.
- HRBPs can see assigned populations.
- Payroll can see payroll-impacting fields.
- AI summaries can only use permitted fields.
- Audit must show which fields AI could access for each review.

This differentiates HCM Next from generic AI copilots and generic workflow tools.

## 19. AI Governance

AI is not the data authority.

AI in v1:

- Reviews
- Explains
- Drafts
- Summarizes
- Flags missing data
- Identifies policy conflicts
- Describes downstream impact
- Recommends next steps

AI in v1 does not:

- Make employment decisions
- Execute material HR transactions autonomously
- View restricted fields without permission
- Override policy
- Approve changes
- Replace human accountability

AI review scopes should start predefined:

- Employee scope
- Manager chain scope
- Team scope
- Cost center scope
- Payroll impact scope
- Policy impact scope
- Downstream system scope

AI must log:

- Prompt context
- Data sources used
- Field visibility
- Actor permissions
- Generated review
- Model/provider metadata
- Human edits or acceptance

## 20. Integration And Reconciliation

HCM Next should start by sitting on top of existing systems, not replacing them.

Integration modes:

- CSV/SFTP import/export
- REST API
- Webhooks
- Scheduled sync
- Event streams, later
- Native connectors, selectively
- Customer-owned APIs

Reconciliation requirements:

- Compare intended state with external system state.
- Detect failed writes.
- Detect partial writes.
- Detect stale data.
- Detect unauthorized external changes.
- Create repair tasks.
- Suppress unsafe notifications when downstream systems failed.
- Preserve audit history even when external systems are inconsistent.

The product should be useful even before perfect writeback exists:

- Design the change.
- Validate it.
- Route approvals.
- Simulate impact.
- Export or hand off the transaction.
- Track reconciliation manually or automatically.

## 21. Enterprise Operations

Enterprise-grade operations must be designed early.

Requirements:

- Environment separation: development, sandbox, staging, production
- Workflow promotion process
- Approval before publishing high-risk workflows
- Segregation of duties
- Least privilege for workflow authors
- Break-glass access with extra audit
- Audit export APIs
- Data retention policies
- Legal hold support
- Regional data residency controls
- Encryption at rest and in transit
- Key management and rotation
- Secrets rotation
- Regular encrypted backups
- Point-in-time recovery
- Restore testing
- Tenant-level recovery strategy
- Disaster recovery objectives

Observability:

- Structured logs
- Metrics
- Distributed traces
- Correlation IDs
- Request IDs
- Block execution timings
- Queue timings
- Retry counts
- Integration latency and errors
- AI token and cost tracking
- Failure rates by workflow, block, integration, and tenant

Operational dashboards:

- Stuck workflows
- Failed transactions
- High-risk workflows
- Integration health
- Slow blocks
- High-cost AI steps
- Permission denials
- Retry storms
- SLA breaches
- Manual repair backlog

## 22. Moat

The moat is not "AI workflow builder."

Real moat:

| Moat                                  | Why It Matters                                                      |
| ------------------------------------- | ------------------------------------------------------------------- |
| Effective-dated HCM transaction graph | Employee changes are temporal, relational, and downstream-sensitive |
| Permission-aware AI context broker    | Generic copilots are unsafe around HR data                          |
| HCM Change Ledger                     | Audit, replay, rollback, causality, and trust are hard              |
| Cross-system reconciliation           | Enterprises do not have one clean source of truth                   |
| Workflow templates by change type     | Implementation speed compounds                                      |
| Policy-aware transaction simulation   | Prevents payroll and compliance errors before they happen           |
| HRIS implementation data              | Every deployment teaches real enterprise patterns                   |
| Enterprise extension framework        | Customers and partners can encode business-specific logic safely    |

The biggest moat:

> HCM semantics + permissions + transaction execution + audit + integrations.

## 23. Go-To-Market

Do not sell rip-and-replace.

Sell:

> We sit on top of your current HCM and fix broken employee-change workflows.

First sales motion:

- 5 paid design partners
- $50k-$150k paid pilot each
- 90-120 day implementation
- One workflow family: job, manager, compensation changes
- One HCM source
- One measurable outcome

Pilot success metrics:

| Metric                           | Example Target                       |
| -------------------------------- | ------------------------------------ |
| Time to complete job/comp change | Reduce by 40-60%                     |
| HRIS manual touchpoints          | Reduce by 30-50%                     |
| Payroll-impacting errors         | Material reduction                   |
| Approval visibility              | 100% traceable                       |
| Audit prep time                  | Days to hours                        |
| Workflow change time             | Vendor project to admin-configurable |
| Failed integration MTTR          | Measurable reduction                 |

Pricing direction:

| Stage                   | Pricing          |
| ----------------------- | ---------------- |
| Design partner pilot    | $50k-$150k       |
| Initial annual contract | $150k-$300k      |
| Enterprise expansion    | $300k-$750k+     |
| Very large enterprise   | $1M+ after proof |

Charge by:

- Platform base fee
- Employee band
- Workflow modules
- Integration packages
- Premium audit/compliance package
- AI review usage
- Implementation/services

## 24. Roadmap

### Phase 1: ChangeOps Overlay

Orchestrate job, manager, compensation, and org changes across existing HCM systems.

### Phase 2: HCM Workflow Operating Layer

Add more lifecycle workflows:

- Offboarding
- Onboarding
- Leave
- Reorgs
- Employee relations
- Document workflows

### Phase 3: System Of Transaction

Customers trust HCM Next as the place where HCM changes are initiated, validated, approved, and governed, even if another system remains the database of record.

### Phase 4: Domain System Of Record

HCM Next becomes authoritative for selected domains:

- Org and position management
- Employee action records
- Workflow history
- Policy-driven changes
- Change ledger

### Phase 5: Full Programmable HCM Core

Only after years of trust does HCM Next credibly replace UKG/Workday modules.

Correct sequence:

```text
Overlay -> control plane -> transaction authority -> domain system of record -> full HCM suite
```

Wrong sequence:

```text
Hello, please replace your HCM suite with our startup.
```

## 25. Biggest Risks

### Risk 1: Building Too Much

The plan dies if we try to build Data Studio, Process Studio, Experience Studio, Security Studio, Integration Studio, Intelligence Studio, custom Go blocks, full HCM records, AI review, embedded widgets, payroll readiness, benefits, leave, talent, documents, and analytics at once.

Mitigation:

> One product, one workflow family, one buyer, one proof metric.

### Risk 2: Buyer Says Existing HCM Already Does This

Answer:

> Yes, your HCM suite has workflows. But your real change process still spans HRIS tickets, spreadsheets, payroll, finance, identity, approvals, internal APIs, and exceptions. We orchestrate the full change transaction across systems, with audit and reconciliation.

### Risk 3: ServiceNow Owns Workflow

Answer:

> ServiceNow is strong for service delivery and enterprise workflows. HCM Next is purpose-built for effective-dated HCM mutations, compensation-sensitive approvals, field-level HR permissions, payroll-impacting change simulation, and employee-data audit.

### Risk 4: AI Compliance Blowback

Do not sell AI as decision automation.

Sell AI as:

- Review
- Explanation
- Drafting
- Preflight
- Missing-data detection
- Impact analysis

### Risk 5: Implementation Services Eat The Company

Mitigation:

- Narrow workflow scope
- Templates
- Strict integration packages
- Certified partner playbook
- Extension SDK later
- Avoid bespoke everything

### Risk 6: Data Access Is Hard

Mitigation:

- Start with import/export
- Support approval and audit before perfect writeback
- Build one deep integration with design partners
- Avoid promising universal connectors too early

## 26. Answers To Current Design Questions

| Question                                         | Answer                                                                                             |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| Fixed primitives, metadata templates, or hybrid? | Hybrid: fixed HCM primitives plus governed metadata extensions                                     |
| How much workflow flexibility in v1?             | Configurable routing, forms, approvals, validations, AI review; not arbitrary everything           |
| Which flagship workflow?                         | Promotion/job/comp/manager change bundle                                                           |
| Should AI execute transactions?                  | No, not in v1                                                                                      |
| How configurable should AI review scope be?      | Predefined scopes first: employee, manager chain, team, cost center, payroll impact, policy impact |
| Minimum RBAC model?                              | RBAC + relationship access + field permissions + action permissions                                |
| SaaS or local developer-first?                   | SaaS product first; developer extensions later                                                     |
| Visual canvas first?                             | No. Schema-first workflow config, visual editor later                                              |
| First integrations?                              | HCM import/export, REST/webhook, Slack/Teams/email, then payroll/identity                          |
| UKG replacement?                                 | Endgame, not first sales motion                                                                    |

## 27. Final Refined Concept

> HCM Next is an enterprise HR transaction control plane. It helps complex companies manage employee changes across HCM, payroll, finance, identity, and internal systems without relying on spreadsheets, tickets, brittle integrations, or vendor custom work.
>
> The platform starts with high-risk employee changes: job changes, compensation changes, manager changes, and reorgs. It turns them into governed workflows with role-specific UX, policy validation, approval routing, transaction simulation, cross-system reconciliation, and immutable audit history.
>
> Permission-aware AI reviews each change, explains risk, detects missing data, summarizes downstream impact, and drafts approval context, while never seeing fields the actor is not allowed to access.
>
> The long-term vision is a programmable, AI-native HCM system of record. The wedge is the painful transaction layer around today's rigid HCM suites.

Punchline:

> Do not sell better HR software. Sell safe, fast employee changes across broken enterprise HR stacks.

## 28. References To Validate Market Context

- UKG Pro HCM positioning: https://www.ukg.com/products/ukg-pro
- Workday HCM positioning: https://www.workday.com/en-us/products/human-capital-management/overview.html
- ServiceNow HR Service Delivery positioning: https://www.servicenow.com/products/hr-service-delivery.html
- Rippling platform positioning: https://www.rippling.com/
- Gartner HCM Suites research: https://www.gartner.com/en/documents/6927066
- EEOC AI guidance context: https://www.eeoc.gov/sites/default/files/2024-04/20240429_What%20is%20the%20EEOCs%20role%20in%20AI.pdf
- NYC AEDT rules context: https://www.nyc.gov/site/dca/about/automated-employment-decision-tools.page
- EU AI Act context: https://digital-strategy.ec.europa.eu/en/policies/regulatory-framework-ai
