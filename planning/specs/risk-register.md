# Architecture and Product Risk Register

Extracted from the HCM Next architecture constitution so this contract can evolve independently. The master delivery scope remains governed by [../execution-plan.md](../execution-plan.md).

## 15. Major Risks and Responses

### Risk 1: Scope Expansion

The platform could attempt to become an HCM suite, workflow engine, integration platform, AI agent system, and analytics product at once.

Response:

> One workflow family, one customer problem, one measurable proof before expansion.

### Risk 2: Existing Suites Appear Good Enough

Response:

Demonstrate the cross-system transaction that suite-local workflow cannot govern, including approvals, downstream effects, reconciliation, and repair.

### Risk 3: Generic Workflow Platforms Compete

Response:

Differentiate through effective-dated HCM semantics, field-level access, immutable proposal approval, transaction simulation, authority policy, and reconciliation.

### Risk 4: Authority Conflicts Create Unsafe Writes

Response:

Require explicit authority configuration, effective-dated write sets, conflict policy, execution-time revalidation, and constrained automatic repair.

### Risk 5: Durable Workflow Complexity Consumes the Company

Response:

Limit graph expressiveness, use release gates for advanced runtime behavior, and separate HCM differentiation from commodity orchestration infrastructure.

### Risk 6: AI Damages Trust

Response:

Keep humans accountable, restrict AI visibility, validate structured output, measure correctness, control model changes, and retain a complete non-AI path.

### Risk 7: Privacy and Identity Controls Lag Authorization

Response:

Build enterprise identity assurance, data minimization, retention, regional controls, and ledger privacy before requesting production write authority.

### Risk 8: Implementation Services Dominate Economics

Response:

Constrain workflow scope, standardize integration packages, use templates, measure deployment effort, and avoid bespoke platform features for one customer.

### Risk 9: Customers Reject Another Write-Path Dependency

Response:

Start in low-authority modes, prove measurable value, earn authority incrementally, and make bypass and exit credible.

### Risk 10: Commercial Assumptions Are Wrong

Response:

Treat buyer, price, timing, implementation, and ROI assumptions as an explicit learning agenda with decision dates and evidence thresholds.

### Risk 11: The Ledger Becomes a Log Archive or Query Bottleneck

Response:

Keep business-event admission strict, route runtime diagnostics to telemetry, and serve operational questions from versioned projections. Treat the ledger API as a logical contract so storage can scale without leaking physical design into every product.

### Risk 12: Derived State Silently Diverges From Truth

Response:

Track source sequences and freshness, run continuous replay sampling and domain invariants, distinguish business correction from derived-state rebuild, and make governed repair a first-class product workflow.

### Risk 13: API Breadth Produces Semantic Drift

Response:

Define one transport-independent capability contract, prefer semantic commands for business mutations, publish machine-readable manifests, enforce compatibility policy, and test that HTTP, gRPC, SDK, workflow, and agent paths produce the same authorization, ledger, and error behavior.

### Risk 14: Agent Composability Amplifies Data or Mutation Risk

Response:

Give agents separate identities and capability grants rather than credentials, intersect delegated authority, separate planning from execution authorization, govern query composition and derived data, distinguish bulk permissions, and route material operations through simulated deterministic workflows.

### Risk 15: Escape Hatches Become Governance Bypasses

Response:

Replace generic force controls with typed interventions, separate repair and override authority from normal approval, require structured obligations, constrain operations to safe points, and monitor retrospective review and reconciliation completion.

### Risk 16: Workflow Migration Corrupts Live Business Meaning

Response:

Keep instances pinned by default, require a versioned migration plan and compatibility classification, forbid unsafe migrations, preserve complete execution fingerprints, and use repair, supersession, or controlled old-version completion when meanings cannot be mapped safely.

### Risk 17: Corporate Inheritance Creates Ambiguous or Leaky Policy

Response:

Keep hierarchy inside the tenant boundary, scope every resource, make sharing directional, distinguish configuration precedence from security precedence, preserve person/employment separation, and expose complete scope-resolution explanations.

### Risk 18: Organization Policy Complexity Causes Privilege Leakage

Response:

Separate authentication from authorization, make organization scope explicit, use stable data domains instead of table names, inherit mandatory denies downward, simulate customer policies before publication, apply authorized scope in repositories and serializers, and continuously test record, field, population, and obligation enforcement.

### Risk 19: Localization Assumptions Corrupt Global Business Meaning

Response:

Resolve language, country, currency, timezone, calendar, and jurisdiction independently; keep canonical APIs locale-neutral; use explicit Money and business-time types; version calendars, rates, translations, and documents; distinguish localization from legal policy; and block unsafe fallback for regulated content.

### Risk 20: HCM Next Encodes Incorrect or Stale Legal Interpretations

Response:

Treat legal content as effective-dated packs with authoritative references, interpretation status, tests, owners, and review dates; require customer-counsel approval for tenant interpretation; simulate changes before activation; quarantine unsafe versions; and represent ambiguity as a legal-review obligation rather than an invented answer.

### Risk 21: Immutable Audit Conflicts With Privacy, Retention, or Legal Holds

Response:

Keep raw sensitive payloads outside immutable events, minimize ledger facts, apply domain-specific retention, support restriction and cryptographic destruction, model holds separately from access, and prove every erasure, retention exception, processor notification, and hold release through governed workflows.

### Risk 22: Semantic Data Becomes Untraceable HR Truth

Response:

Keep facts, observations, decisions, interactions, and inferences as separate typed classes; govern semantic schemas; require provenance, confidence, purpose, classification, expiration, and supersession; and prohibit inference from mutating canonical facts without a new authorized business action.

### Risk 23: Workforce Intelligence Becomes Employee Surveillance

Response:

Collect only meaningful in-product activities for declared purposes, prohibit invasive monitoring, apply legal review and short identifiable retention, enforce organization and cohort controls, require transparency, and separate security evidence from workforce evaluation uses.

### Risk 24: Cross-Domain Analytics Produces Misleading or Unsafe Conclusions

Response:

Use certified semantic relationships and metrics, preserve bitemporal state, label correlation versus causation, record decision inputs actually visible, enforce query-time AuthZ and legal policy, suppress unsafe cohorts, and expose query-plan and lineage explanations.

### Risk 25: Agent Interface Becomes an Ungoverned Mutation Path

Response:

Require all agents to use capability contracts, separate planning from execution, compile and simulate material plans, enforce the autonomy ladder, prohibit raw credentials and direct state writes, and preserve current AuthZ, LegalContext, approvals, and deterministic execution at every side-effect boundary.

### Risk 26: Model Routing Optimizes Cost at the Expense of Trust

Response:

Route through typed task and eligibility profiles, make legal region and provider approval hard constraints, require quality floors and evaluation evidence, shadow alternatives safely, preserve deterministic non-AI paths, and quarantine models or agents when thresholds fail.

### Risk 27: Agent Memory Leaks, Stales, or Institutionalizes Bad Advice

Response:

Separate memory layers, attach scope, purpose, classification, provenance, expiry, and current authorization, require explicit promotion to organizational knowledge, track outcomes and review dates, and make revocation and deletion propagate through caches and indexes.

### Risk 28: Autonomous Agents Create Scale-Amplified Failures

Response:

Bound A6 operations by capability, population, organization, rate, budget, confidence, duration, and stop conditions; aggregate incidents; use safe points and RepairPlans; require kill switches; and lower autonomy automatically on drift, anomaly, policy change, or repeated error.

### Risk 29: Premature Polyglot Persistence Multiplies Failure Modes

Response:

Begin with PostgreSQL ledger, projections, and outbox plus an object plane; introduce a separate event bus, cache, search engine, analytical store, or vector plane only against explicit throughput, latency, isolation, recovery, or regulatory gates. Preserve semantic contracts so technologies remain replaceable.

### Risk 30: Tamper Evidence Is Misrepresented as Impossible Corruption

Response:

State the assurance precisely, prohibit normal mutation, separate administrative authority, chain natural streams, sign integrity epochs outside the database boundary, preserve selected evidence under WORM controls, verify backups and restores, and rehearse the response to failed integrity checks.

### Risk 31: Search and Analytics Leak Data Outside Authorization Scope

Response:

Apply tenant, organization, classification, purpose, population, and cohort constraints during candidate and query planning; reauthorize canonical resources before disclosure; test counts, facets, suggestions, snippets, exports, timing, and derived outputs; and fail closed when security metadata is stale.

### Risk 32: Rebuildability Exists Only on Architecture Diagrams

Response:

Set recovery objectives for each derived plane, retain sufficient source data and versions, run scheduled independent and shadow rebuilds, measure watermarks and drift, preserve backfill capacity, and block authority expansion when a required plane cannot be reconstructed and verified in time.

### Risk 33: Billing Complexity Arrives Before Product-Market Fit

Response:

Keep early pilots commercially simple, treat prices and packages as hypotheses, use an external invoicing provider where sensible, and build only the semantic entitlement, usage, contract-version, and reconciliation foundations required to evolve safely.

### Risk 34: Technical Refactoring Changes Customer Charges

Response:

Meter at published customer-visible capability boundaries, distinguish internal cost events from customer usage, version meter definitions, regression-test representative workflows, and require commercial impact review before changing metering semantics.

### Risk 35: Retries, Redelivery, or Replay Double-Bill Customers

Response:

Bind every UsageEvent to a stable semantic business execution and meter-specific idempotency key, reconcile executions to usage and invoice lines, isolate late and duplicate events, and correct financial history through append-only adjustments.

### Risk 36: Variable AI and API Charges Become Unpredictable

Response:

Provide included allowances, stable AI credits and API compute units, pre-execution estimates, budgets, warning thresholds, configurable overage, semantic cost explanations, and contract-level caps while preserving provider tokens and infrastructure cost internally.

### Risk 37: Billing Hierarchy Leaks Sensitive Subsidiary Activity

Response:

Separate payer, usage, invoice, and allocation scopes; minimize worker data in usage events; authorize billing records independently; support aggregate-only parent visibility; and redact sensitive HR purpose labels from invoices and showback.

### Risk 38: Model Routing Sacrifices Quality or Compliance for Margin

Response:

Treat privacy, residency, legal eligibility, customer approval, quality floors, and service levels as hard routing constraints. Use cost and margin only to choose among already eligible options, and preserve routing and cost provenance for evaluation.

### Risk 39: Reference Workflows Become an Accidental Full-Suite Roadmap

Response:

Use them first as specifications, simulation fixtures, and architecture conformance tests. Implement them incrementally according to product evidence and phase gates; do not build payroll, benefits, ATS, leave, or WFM merely to complete a diagram.

### Risk 40: Happy-Path Demonstrations Hide Missing Transaction Semantics

Response:

Require every reference workflow to test stale approval, concurrent change, future and retroactive dates, ambiguous identity, partial external failure, correction, privacy boundaries, cost limits, policy evolution, replay, and reconciliation.

### Risk 41: Generic Workflow Abstractions Erase Domain Meaning

Response:

Standardize simulation and execution envelopes while preserving typed Person, Employment, Position, compensation, leave, termination, payroll, IAM, and legal semantics. Reuse primitives without reducing material HCM operations to opaque generic nodes.

### Risk 42: Simulation Creates False Confidence About External Outcomes

Response:

Classify deterministic, policy-derived, connector-derived, estimated, and unknown effects; expose provenance, assumptions, and confidence; revalidate before execution; observe actual outcomes; and reconcile every material external effect.

### Risk 43: One Generic Rules Engine Produces Legally Incorrect Composition

Response:

Share jurisdiction, versioning, provenance, obligations, and publication infrastructure while giving tax, wage, leave, privacy, reporting, immigration, contracts, and benefits explicit domain engines and composition strategies.

### Risk 44: Country-Level Labels Overstate Regulatory Coverage

Response:

Publish granular coverage manifests by jurisdiction, domain, worker type, calculation, report, effective period, connector, validation level, exclusion, and counsel-required configuration. Never equate one supported workflow with complete country compliance.

### Risk 45: Regulatory Content Becomes Stale

Response:

Assign owners and review dates, monitor authoritative sources, model future-effective changes, simulate customer impact, maintain golden tests, publish before effective dates, quarantine defective packs, and alert customers when coverage is uncertain or expired.

### Risk 46: Deterministic Output Is Mistaken for Legal Advice

Response:

Preserve source and interpretation provenance, distinguish vendor baselines from customer-counsel-approved rules, expose assumptions and unresolved facts, require specialist approval for ambiguous or material uses, and avoid product claims that replace legal or tax professionals.

### Risk 47: Tax or Filing Errors Create Financial and Regulatory Exposure

Response:

Use fixed-precision deterministic engines, golden and jurisdiction-specific tests, dual control for filings, reconciliation to authority-bearing payroll facts, immutable FilingPackages, acknowledgement tracking, amendment workflows, error reserves, and phased authority gates.

### Risk 48: Worker Location and Classification Evidence Is Wrong

Response:

Treat work location, residence, establishment, worker classification, contract, and CBA applicability as effective-dated governed facts with evidence, uncertainty, review, correction, and downstream impact analysis rather than inferred defaults.

### Risk 49: Go-Only Cutover Loses Proven Product Behavior

Response:

Extract contracts, events, schemas, and reference fixtures before implementation. Build only the bounded Phase 1 vertical slice in Go, compare it through offline fixtures, deterministic replay, and isolated environments, and move authority only after parity, recovery, and customer evidence. Do not preserve product learning by carrying Node or TypeScript into the target runtime.

### Risk 50: GWC/GoWebComponents Creates Browser Performance or Accessibility Regressions

Response:

Adopt one vertical workspace first; budget WASM size, startup, memory, hydration, and list rendering; use SSR, islands, and virtualization deliberately; test assistive technology and slow devices; and preserve server-rendered recovery paths for critical operations.

### Risk 51: grpcbridge Becomes an Under-Maintained Critical Gateway

Response:

Keep Protobuf and gRPC as the authority contract, isolate transport adaptation from business semantics, pin and test releases, fuzz and load-test every used transport, restrict reflection, and assign explicit grpcbridge maintenance, upgrade, incident, and internal-fork ownership.

### Risk 52: SchemaFlux Is Stretched Beyond Its Structured-Compiler Strengths

Response:

Begin with the bounded Phase 1 catalogs, retain Protobuf and migrations as authoritative formats for their domains, use explicit custom backends, verify deterministic output and diagnostics, and keep non-structured business logic in ordinary Go rather than forcing it into SchemaFlux.

### Risk 53: Open-Source-First Becomes Self-Hosting Everything

Response:

Evaluate total operating cost, security response, availability, upgrade burden, staffing, portability, and exit cost. Buy managed durability or key custody where it lowers material risk while preserving open protocols, export, replay, and provider replacement.

### Risk 54: Seventy Gaps Become Seventy Services and Teams

Response:

Group capabilities into portfolios, default to a modular Go monolith and scalable workers, introduce a service boundary only for demonstrated isolation or scale, and promote a shared subsystem only after it works across two domains.

### Risk 55: Identity Resolution Causes Irreversible False Merges

Response:

Separate candidate generation from authoritative merge, require review for uncertainty, preserve do-not-merge evidence, simulate downstream effects, protect identity attributes, and support ledgered separation and repair.

### Risk 56: Conflict and Reservation Controls Deadlock Business Work

Response:

Use typed domain conflicts, minimal scopes, explicit expiry, safe merge and rebase policies, operator visibility, supersession, and measured contention. Do not replace business conflict semantics with indefinite database locks.

### Risk 57: Classification or Provenance Metadata Drifts From Data

Response:

Compile metadata with schemas, propagate lineage automatically, block unclassified sensitive contract changes, reconcile projections and exports, test derived outputs, and make classification/provenance coverage a release gate.

### Risk 58: Connector and Secret Frameworks Become Privilege Escalation Paths

Response:

Use capability-scoped short-lived identities, secret references, signed connector packages, isolated runtimes, outbound policy, credential rotation, data classification, contract certification, complete access evidence, and emergency revocation.

### Risk 59: Architecture Diagrams Drift From Executable Contracts

Response:

Treat the ASCII atlas as an explanatory index, assign owners to major views, link diagrams to charters and source contracts, review affected diagrams during architecture changes, and prefer generated inventories where SchemaFlux can keep names and relationships synchronized. Schemas, manifests, policies, and tests remain authoritative when prose or diagrams disagree.

### Risk 60: The Cell Control Plane Becomes a Global Single Point of Failure

Response:

Cache signed placement leases at the edge and in cells, keep data paths cell-local, replicate the minimal directory safely, test stale-but-valid routing, and ensure an unavailable placement controller blocks new placement changes without taking healthy tenant cells offline.

### Risk 61: Tenant Relocation Produces Split-Brain Writes

Response:

Use versioned placement leases, one authoritative write epoch, a bounded safe-point fence, ordered delta replication, target reconciliation, atomic route promotion, rollback windows, and invariant tests that reject writes under stale placement versions.

### Risk 62: Load Shedding Sacrifices the Wrong Work

Response:

Attach semantic criticality at ingress, propagate it automatically, reserve capacity for P0/P1, validate degradation contracts, prohibit silent priority escalation, exercise overload scenarios, and review all fail-open behavior as a security decision.

### Risk 63: Workload Identity Adds Credentials Without Reducing Trust

Response:

Issue short-lived attestable identities, bind identity to capability and tenant context, default-deny network paths, authorize every material service call, rotate automatically, detect reuse or abnormal destinations, and remove long-lived shared service secrets.

### Risk 64: Signed Vulnerable Software Is Treated as Safe

Response:

Treat signature as identity and provenance evidence rather than safety. Deployment admission also evaluates SBOM policy, vulnerability exposure, builder trust, test evidence, exceptions, expiry, and runtime drift.

### Risk 65: Agent Security Becomes Prompt Filtering Theater

Response:

Preserve semantic taint, mediate every tool call outside the model, validate typed arguments and outputs, apply AuthZ/legal/DLP/risk checks, constrain egress, require human confirmation for sensitive tainted actions, and continuously run adversarial fixtures.

### Risk 66: Kill Switches Exist but Fail During the Incident

Response:

Keep containment independent from model providers and ordinary agent control paths, test it on a schedule, support progressively scoped shutdown, revoke active leases and queued writes, preserve deterministic HCM, and measure containment time.

### Risk 67: Backups Succeed but Recovery Fails

Response:

Restore into a separately administered environment, validate manifests and keys, replay projections, execute AuthZ/invariant/reference-workflow tests, measure RPO/RTO, retain failed exercise evidence, and block recovery claims unsupported by a recent full-stack restore.

### Risk 68: Clock or Cryptographic Migration Corrupts Evidence

Response:

Use ordered sequences independently of wall clocks, monitor authenticated time sources and skew, quarantine time-sensitive writes outside bounds, inventory every cryptographic use, version envelopes, support dual verification and key rewrap, and rehearse compromised-key and algorithm migrations.

### Risk 69: Telemetry Leaks HCM Data or Collapses Under Cardinality

Response:

Classify and redact before export, disable sensitive GenAI content by default, bound metric labels, apply risk-aware sampling, isolate telemetry retention from business evidence, monitor collector health and drops, and treat privacy-gateway failure as a fail-closed export condition.

### Risk 70: Architectural Completeness Is Mistaken for Delivery Scope

Response:

Keep the architecture constitution, Phase 1 execution plan, focused specifications, and reference scenarios separate. Assign every subsystem an implementation-depth label, prohibit deferred planes from becoming accidental pilot dependencies, require explicit scope tradeoffs, and promote shared infrastructure only after a vertical workflow and second domain demonstrate its necessity.

### Risk 71: HRIS DataOps Amplifies Administrative Privilege or Scope

Response:

Separate preview, bulk execution, export, redrive, configuration publication, reference mapping, and identity operations into distinct capabilities. Apply tenant, organization, field, purpose, population, egress, cost, and dual-control policy; preserve immutable evidence; enforce resumability and idempotency; and productize an operator capability only after adversarial and support-readiness gates pass.

### Risk 72: A Generic Connector Framework Erases Vendor Semantics or Overstates Support

Response:

Keep transport, typed schema, canonical semantics, governance, and certification as explicit maturity levels. Preserve vendor payload and behavior provenance at the boundary, publish object/operation/version support matrices with known exclusions, require provider contract and failure-mode fixtures, and never equate a configured adapter or vendor logo with certified bidirectional support. Promote a second connector only after it reuses the shared mapping, capacity, operation-journal, redrive, and reconciliation runtime without creating a parallel vendor-specific architecture.

### Risk 73: The Workflow Kernel Becomes a Domain Monolith or an Unsafe General-Purpose Engine

Response:

Keep the primitive vocabulary small and domain-neutral; require payroll, legal, AuthZ, IAM, billing, and agent behavior to enter through typed capabilities. Compile immutable plans before execution, reject undeclared effects and unsafe retry/cancellation paths, prohibit arbitrary code and unbounded graph behavior, expose only governed intervention APIs, and implement only the Promotion-required primitive slice in Phase 1. Treat runtime state, business evidence, projections, and telemetry as separate contracts so operational convenience cannot create a second source of business truth.

### Risk 74: Communication Delivery Is Mistaken for Human Receipt or Legal Satisfaction

Response:

Separate provider acceptance, transport delivery, recipient read, acknowledgement, response, signature, legal evidence, and workflow obligation satisfaction. Require semantic MessageIntent, current audience and field authorization, endpoint classification policy, deterministic rendered-content hashes, durable observations, fallback/reconciliation, and jurisdiction-specific evidence rules. Keep sensitive content in the secure inbox when required, prevent preferences from disabling mandatory messages, and ensure provider retries cannot duplicate logical messages or silently satisfy business obligations.

### Risk 75: The Coverage Matrix Creates False Confidence That All Gaps Are Known

Response:

Treat the matrix as a change-detection and ownership mechanism, not a completeness claim. Add responsibilities discovered through workflows, incidents, customer implementation, threat modeling, and regulatory review; require evidence before `DEFINED`; review `IMPLIED` dependencies at every authority gate; preserve explicit `MISSING` and `DEFERRED` states; and measure whether newly discovered gaps are classified and owned promptly.

### Risk 76: Forms and Rules Become Hidden Application Code

Response:

Use bounded typed question and expression vocabularies, immutable versions, compilation, dependency impact, deterministic server evaluation, resource limits, field-level authorization, classification propagation, accessibility checks, and reference fixtures. Prohibit arbitrary scripts, side effects, network access, unbounded loops, and regulated calculations; keep workflow topology, Legal/AuthZ policy, payroll/tax kernels, and agent inference in their own authority domains.

### Risk 77: Workflow Bypasses Domain Ownership

Response:

Require every material workflow effect to target a registered domain capability. Prohibit workflow repositories from writing domain or projection tables, constructing canonical domain events, or embedding salary, payroll, tax, benefits, IAM, or vendor semantics. Enforce package boundaries, capability manifests, compiler effect analysis, architecture tests, and ledger provenance linking each write to its owning domain capability.

### Risk 78: Derived or External Planes Enter the Critical Transaction Path

Response:

Commit authoritative domain facts and the outbox before invoking connectors, messaging providers, search, vectors, OLAP, reporting, or process mining. Declare exceptional required derived inputs explicitly with staleness and failure policy. Test that analytics, search, messaging, and connector outages degrade only the capabilities that depend on them and cannot block unrelated payroll, access revocation, or employee transactions.

### Risk 79: Desired Configuration Is Mistaken for Applied Runtime State

Response:

Compile hermetic signed bundles, distribute them through scoped canaries, require per-workload activation receipts and health gates, preserve a verified last-known-good local snapshot, distinguish desired/published/applied fingerprints, automatically pause or roll back on loss of control, and keep bootstrap trust independent from ordinary tenant configuration.

### Risk 80: Identity Recovery or Token Refresh Bypasses Normal Assurance

Response:

Carry identity, authentication, and federation assurance in every PrincipalContext; require PKCE, exact redirect validation, audience restriction, sender-constrained high-risk tokens, refresh-family rotation/replay detection, bounded session lifetimes, explicit step-up and revocation propagation, governed recovery evidence, and security notices for identity-link or authenticator changes.

### Risk 81: Untrusted Content Becomes Executable Instruction or Parser Attack

Response:

Quarantine every external byte stream, validate allowlisted type and signature independently, isolate bounded parsers, scan malware, apply content disarm where supported, cap archive expansion, classify and DLP-check derivatives, admit only safe promoted derivatives to search/RAG/forms/workflows, preserve taint, and support policy-version rescans and revocation.

### Risk 82: Ambient Timezone or Locale Data Breaks Reproducibility

Response:

Treat tzdb, locale, currency, holiday, and geographic reference datasets as versioned signed dependencies; record their fingerprints on calculations and timers; distinguish locale from jurisdiction; declare future-timer recalculation policy; impact-test updates; and retain historical versions needed for replay and evidence.

### Risk 83: Deletion Is Declared Complete While Copies Remain Recoverable

Response:

Build cross-plane deletion manifests, evaluate legal holds, distinguish logical purge, cryptographic erase, and media sanitization, inventory derived stores and exports, revoke connectors and credentials, record backup-expiry and restore re-delete behavior, verify every target, and issue a signed certificate only when remaining exceptions are explicit.

### Risk 84: Deferred Systems Are Reimplemented Invisibly Inside Phase 1 Services

Response:

Require every responsibility to name authority, state, typed APIs, failure semantics, evidence, security, and phase depth. Keep deferred boundaries explicit, prohibit hidden schedulers/case stores/file gateways/rule engines in workflow or connector code, review dependency-graph edges, and update the coverage matrix and execution scope before increasing implementation depth.

### Risk 85: Phase 1 Becomes Platform Completion Before Product Evidence

Response:

Use separate paid-observation, limited-write, and general-production gates. Gate A remains read/observe/simulate and measures paid customer value; Gate B adds only the bounded write kernel after evidence; Gate C owns broader trust and operations. Name staff, owner, critical path, and schedule assumptions, and require equivalent scope removal or explicit re-estimation whenever a requirement is promoted.

### Risk 86: Position Is Treated as Reference Data

Response:

Give Position/Headcount its own domain authority, effective-dated capacity and occupancy, fixed-precision FTE arithmetic, vacancy derivation, budget separation, reservations and fencing, invariants, AuthZ, commands/events, multi-stream transaction participation, and source-authority reconciliation. Reference data supplies job/location/cost-center concepts only.

### Risk 87: Mixed Application Versions Corrupt Durable State During Upgrade

Response:

Publish explicit binary, API, event, reducer, workflow, outbox, projection, connector, and storage compatibility ranges. Use expand/contract, idempotent checkpointed backfills, adoption watermarks, shadow validation, cell/tenant rollout, abort gates, and irreversible-step approval; reject unknown writer versions and prove rollback before the declared boundary.

### Risk 88: Consent Becomes an Undifferentiated Boolean

Response:

Separate lawful processing authority, notice presentation, consent decision, communication preference, objection, restriction, and withdrawal. Scope each by subject/controller/purpose/data/operation/recipient/jurisdiction/time, prohibit consent from replacing another required basis, emit withdrawal obligations, collect downstream receipts, and keep incomplete propagation visible.

### Risk 89: IdP Outage Causes Unsafe Fail-Open or Total P0 Lockout

Response:

Declare capability-specific identity-continuity profiles, bound existing sessions and cached keys, forbid new sessions from stale assertions, define revocation uncertainty and step-up behavior, isolate tenant federation failures, and permit only separately controlled hardware-bound emergency identities for explicitly listed P0 actions with dual control, recording, expiry, and reconciliation.

### Risk 90: Customer-Configured Destinations Enable SSRF or Egress Bypass

Response:

Require signed DestinationTrust for every connector, webhook, provider, model, MFT, filing, and support endpoint. Normalize schemes/hosts/ports, validate and pin all resolved addresses, exclude private/link-local/metadata ranges, revalidate every redirect, authenticate proxy/TLS identity, enforce residency/classification/purpose at a shared egress gateway, quarantine destination drift, and run negative rebinding/encoding tests.

### Risk 91: Workflow and Ledger Are Better Defined Than the Business Mutation

Response:

Give People/Employment/Assignment, Compensation, and Position/Headcount explicit domain ownership. Define temporal entities, arithmetic, invariants, source authority, commands/queries/events, security compartments, corrections, planned appends, projections, and reconciliation. Workflow invokes their capabilities and cannot manufacture their events or persist their facts.

### Risk 92: Hash Guarantees Depend on Nondeterministic Serialization

Response:

Use versioned canonicalization profiles that bind schema, algorithm, scope, and exact canonical bytes. Specify ordering, maps/sets, null/default/absent, fixed decimal, time/tzdb, Unicode, unknown fields, binary/artifact references, and excluded display data. Bind full material proposal context, preserve historical profiles, support dual algorithms, and gate releases on cross-version golden byte/digest vectors.

### Risk 93: Security-Critical Transitional Systems Lack Separation of Duties

Response:

Give upgrades, identity continuity, and destination trust explicit owners, lifecycle states, capabilities, dependency behavior, author/approver/executor separation, step-up/dual control, typed failures, immutable journals, evidence APIs, retention, and conformance. Coverage cannot be `DEFINED` when those fields are absent.

### Risk 94: Source Authority Falls Back to Last Write Wins

Response:

Resolve signed effective-dated authority by tenant/org/domain/resource/field/operation/source/time with deterministic precedence and explicit ambiguity. Fail closed for writes on unknown/stale policy, distinguish observation from owned fact, constrain import/repair separately, preserve decision evidence, and perform handoff through dual observation, writer fencing, watermark cutover, verification, reconciliation, and rollback.

### Risk 95: Authorization Consumes an Invalid or Stale Organization Graph

Response:

Separate Organization Domain ownership from scoped AuthZ. Define typed graph lifecycles, cardinality/cycle/legal-boundary invariants, stream versions, reorganization and correction behavior, projection/cache invalidation watermarks, source authority, external reconciliation, and freshness limits that block writes when the required graph cannot be trusted.

### Risk 96: False Identity Merge Causes Cross-Domain Privacy and History Damage

Response:

Centralize identity resolution, minimize candidate disclosure, preserve do-not-merge decisions, require governed thresholds and human authority, enumerate every merge/separation consumer and effect, fence concurrent changes, keep immutable former-ID redirects, reconcile external links, verify invariants, and repair/split through append-only plans rather than rewriting history.

### Risk 97: Budget Checks Conflate Capacity, Compensation, and Finance Authority

Response:

Require typed BudgetAuthorityRef with owner, scope, period, unit, currency, baseline, reservation and observation. Keep headcount, compensation pool, finance cost, and workflow cost distinct; use fixed precision and explicit FX; fence internal/external holds; expose ambiguity; enforce expiry/release/correction and SoD; and never let one passing budget type imply another.

### Risk 98: Approval Occurs Before the Material Proposal Is Simulated

Response:

Separate intent, proposal, approval, execution, external consistency, reconciliation, and closure dimensions. Preflight and deterministically simulate before submitting the CanonicalDigest for approval. Revalidation may confirm it; any material difference creates a new revision and invalidates affected approvals. Cancellation, supersession, correction, and closure remain typed evidenced transitions.

### Risk 99: Conflict Preflight Races With Execution

Response:

Register normalized write intents and baselines, version conflict snapshots and domain rules, then close the race at commit with expected domain/conflict sequences in one transaction or a fenced serialization lease. Fail closed on stale/unavailable indexes, require domain merge proof, hide unauthorized conflict details while preserving the block, and test two successful preflights with only one permitted commit.

### Risk 100: Missing or Downgraded Classification Leaks Data Everywhere Consistently

Response:

Centralize taxonomy and label authority, combine static/runtime/content/derived labels most-restrictively by default, propagate provenance and consumer receipts, fail closed for egress/models/messages/connectors and lower-class stores, isolate partial quarantine explicitly, and require reviewed transformation proof, expiry, and SoD for any declassification.

### Risk 101: Retention and Deletion Operate Without a Complete Record Inventory

Response:

Declare every material record into a series with custodian, source/derived copies, schedule, trigger/cutoff, classification, holds, archive/deletion method, and vital status. Calculate eligibility, reconcile minimum/maximum conflicts, require complete inventory and dual review, execute through archive/deletion authorities, verify backups and copies, and issue a signed disposition certificate or explicit repair state.

### Risk 102: Incompatible Transaction Plans Are Manufactured by Workflow or Domains

Response:

Centralize immutable TransactionPlan assembly and local commit coordination. Require domain/governance validation, exact approval digest, conflict/reservation/authority/sequence/idempotency checks at prepare, one ACID event/projection/outbox commit, no external calls inside it, scoped commit identity, durable receipts, ambiguous-outcome lookup before retry, and explicit saga/repair handoff.

### Risk 103: Secret References Exist Without a Safe Secret Lifecycle

Response:

Define secret kinds, versions, policies, leases, bootstrap, rotation, revocation, compromise and recovery. Bind short-lived access to workload/tenant/region/purpose/destination, prohibit plaintext persistence/logs/UI/agents/config, require overlap and adoption receipts, reject stale versions, separate use/recovery/destruction authority, rehearse provider outage and leak response, and preserve metadata-only evidence.

### Risk 104: Missing or Stale Quality Inputs Are Reported as Passing

Response:

Use PASS/FAIL/UNKNOWN/NOT_APPLICABLE with required watermarks and completeness. Keep domain ownership of hard invariants, bound synchronous and customer rule resources, quarantine or block on UNKNOWN as declared, rerun invariants at commit, govern exceptions/overrides with expiry and SoD, and verify remediation before closure.

### Risk 105: Lineage Appears Complete Because IDs Exist but Edges Are Missing

Response:

Register typed lineage publishers and required edge manifests, attach versions/classification/times/watermarks, expose partial/lagging/quarantined publishers, reauthorize every node/edge/purpose/inference traversal, use opaque boundaries for protected relationships, retain direct critical references, rebuild from authoritative sources, and never let the graph invent authority or claim completeness without receipts.

### Risk 106: False Capability Metadata Authorizes Unsafe Execution

Response:

Give the Capability Registry signed versioned manifests bound to domain owner, schemas, implementation build, transports, effects, risk, idempotency, consistency, governance, agent/bulk/cost/SLO and conformance. Require review/SoD, local applied snapshots, unknown-version failure, activation receipts, adoption watermarks, quarantine kill propagation, and dependency-safe retirement.

### Risk 107: One Governance Plane's Allow Cancels Another Plane's Deny

Response:

Compose authoritative subdecisions under fixed rules: mandatory deny dominates, restrictions intersect, obligations union/deduplicate, impossible requirements become an explicit contradiction, and unknown/stale mandatory inputs fail closed. Bind snapshots/freshness/invalidators, revalidate at execution, and preserve the full composition trace without letting the coordinator edit source policies.

### Risk 108: Alerts, Repairs, and Customer Messages Disagree About Incident State

Response:

Centralize incident lifecycle, affected-set watermarks, business and technical severity, commander/responder assignments, fingerprint/merge/split/duplicate semantics, mitigation/repair/communication/SLO links, compartmented views, resolution criteria, reopen and post-incident review. Preserve conditions and revisions rather than deriving truth independently in each consumer.

### Risk 109: SLOs Are Met by Changing Measurement or Excluding Failures

Response:

Version and approve immutable SLI good/valid/total semantics, sources and windows; mark excessive missing telemetry UNKNOWN; distinguish contractual/internal targets; calculate multi-window burn and consequences; prohibit unapproved retroactive exclusions; require scoped, expiring maintenance exclusions; link budget exhaustion to freeze/degradation/incident obligations; and retain measurement evidence.

### Risk 110: People, Position, and Organization Disagree About the Manager

Response:

Assign every reporting relationship to one typed Organization/Relationship stream. People assignments and Position projections retain relationship references/watermarks only. Resolve ManagerOf by assignment/effective time/purpose with explicit acting/direct/position/fallback precedence, never infer dotted/project authority, expose vacancy/ambiguity, block on projection disagreement, and atomically publish canonical changes plus invalidation/reconciliation evidence.

### Risk 111: Protected Matching Claims Leak Into Ordinary People Records

Response:

Keep claims, normalized matcher values, verified linkage identifiers, candidates/scores, do-not-merge and match evidence in Identity Resolution. Store opaque resolution references in People; promote only an authorized provenance-bearing fact under SourceAuthority; never rewrite historical match evidence on correction; and test repository, API, search, export, agent and audit views against hidden matcher data.

### Risk 112: AuthZ Creates a Second Worker Assignment State Machine

Response:

Keep Assignment lifecycle solely in People, Occupancy in Position, and reporting relationships in Organization. AuthZ consumes a minimal effective-dated projection with source sequences and graph/occupancy watermarks, cannot transition business state, invalidates caches on source events, independently rebuilds/reconciles, and fails closed for material decisions when required freshness or canonical agreement is absent.

### Risk 113: Incumbent Language Erases the Positioning Difference

Response:

Treat workflow, APIs, webhooks, analytics, AI, agentic orchestration, and `Workforce Operating Platform/System` language as table stakes. Lead with cross-system workforce transaction control and require dated competitive evidence. Position the durable chain of proposal integrity, source authority, conflict control, revalidation, observation, reconciliation, repair, and provenance rather than generic platform nouns.

### Risk 114: The Roadmap Becomes an Incumbent Feature-Parity Checklist

Response:

Assign each scoped domain `OBSERVE`, `ORCHESTRATE`, `OWN_SELECTED_SCOPE`, `REPLACE_SELECTED_PRODUCT`, `PARTNER_LONG_TERM`, `DEFER`, or `REJECT`. Require paid pull, shared-model advantage, authority readiness, regulatory/support operations, migration/recovery, SLO/security evidence, and acceptable economics before ownership. Adding a box or intent name never funds a native product.

### Risk 115: Aspirational Architecture Is Presented as Current Competitive Capability

Response:

Label every comparative claim `INCUMBENT_CURRENT`, `HCM_NEXT_IMPLEMENTED`, `HCM_NEXT_PILOT`, `HCM_NEXT_CONTRACTED`, or `HCM_NEXT_ASPIRATION`, with verification date and evidence. Prohibit unlabeled radar charts and feature scores. Current automated/operating evidence is required before marketing a contract as shipped behavior.

### Risk 116: HCM Next Adds More Complexity Than It Removes

Response:

Baseline the incumbent process and measure handoffs, duplicate entry, errors, reconciliation latency, audit effort, repair time, integration ownership, and operating cost. Exercise bypass, rollback, recovery, and tenant exit. Fail the pilot thesis if it merely relocates tasks or if removed risk and labor do not exceed the new control-plane dependency.

### Risk 117: Vendor-Level Connector Claims Hide Edition and Semantic Limits

Response:

Scope compatibility by product/edition, API/version, licensed feature, tenant configuration, object/field, operation, direction, authority, rate/idempotency behavior, observation method, and reconciliation guarantee. Only certified connector versions may be marketed as production-ready; a vendor logo or successful HTTP test is not semantic support.

### Risk 118: Native Payroll or WFM Is Entered Before Company-Scale Readiness

Response:

Default to integration, observation, reconciliation, and repair. Treat native payroll and deep WFM as separate authority-expansion investments requiring domain and correction kernels, regulatory content operations, country/industry scope, support, migration, scale, SLO/RPO/RTO, security, economics, and parallel-run evidence. Keep them deferred when any gate is absent.

### Risk 119: Catalog and Contract Maturity Is Overstated

Response:

Distinguish accepted candidate vocabulary, `CATALOGUED`, `DRAFT_CONTRACT`, `CONTRACTED`, `COMPILED`, and `PUBLISHED`. Keep unresolved schema/capability/rule/test references below `CONTRACTED`; fail publication on incomplete partitions. Report exact persisted counts rather than treating a conversational list as a governed source artifact.

### Risk 120: Phase 1 Architecture Contracts Become Forty-Eight Staffed Projects

Response:

Separate coverage, constraint, implementation, and staffed-delivery status. Require a Gate A/B delivery manifest with named owner, estimate, evidence and displaced scope for late additions. Stop or reapprove when the agreed people/time envelope cannot contain the fixed vertical slice; never satisfy the matrix with fake production dependencies.

### Risk 121: Harmed Workers Cannot Understand or Contest a Decision

Response:

Resolve decision rights by jurisdiction, contract, policy and decision type. Support notice, visible-input explanation, correction, independent human review, contest/appeal, accessibility/language, representation, non-retaliation, deadlines and remedy. Preserve the original decision; affirm, supersede or correct append-only. Prohibit autonomous agent/model adverse action.

### Risk 122: Outcome Analytics Converts Correlation Into Causation or Surveillance

Response:

Require outcome contracts with measure, population, window, baseline, completeness, confounders, purpose and claim strength. Type descriptive, associative, quasi-experimental, randomized and non-evaluable results. Suppress unsafe cohorts, reauthorize new purposes, expose disputes/corrections, and prevent agents/dashboards from upgrading claim strength.

### Risk 123: Manual Continuity Creates an Uncontrolled Shadow HCM

Response:

Treat manual continuity as a governed, scoped, time-bounded mode with activation authority, trusted watermark, dual control, numbering/idempotency, evidence, secure storage and re-entry mapping. On recovery inventory, simulate, approve, commit, reconcile and destroy temporary copies. Preserve uncertain time/order and detect duplicates/conflicts.

### Risk 124: Exhausted Work Disappears Into Dead-Letter Queues

Response:

Create owned `QuarantinedWork` with semantic identity, classification, attempts, ambiguity, owner, SLA and next action. Require redrive, repair, incident/case, supersession/cancellation or governed discard evidence. Redelivery retains original event/effect identity, and dead-letter movement remains in SLO/error-budget totals.

### Risk 125: Model, Infrastructure and Vendor Semantics Drift While Health Checks Stay Green

Response:

Separate model, control and semantic drift. Maintain versioned baselines, completeness/freshness, affected sets, quarantine/rollback, false-positive review, incidents and reactivation gates. Endpoint/deployment availability never proves model eligibility, security posture or connector semantic compatibility.

### Risk 126: Required Core Libraries Do Not Form a Reproducible Build

Response:

Pin one Go toolchain, GWC/grpcbridge/SchemaFlux revisions, shared dependency matrix, Protobuf compiler/plugins and generated-file policy. Prove deterministic offline SchemaFlux parse/normalize/relate/validate/emit behavior, dirty-output detection, tests, SBOM and provenance. Block Gate A implementation claims until the root Go-native pipeline exists.

### Risk 127: Role Names Exist but Nobody Is Operationally Accountable

Response:

Require accountable person/rotation, on-call route, security/privacy/domain reviewers, support/customer communication, SLO/error-budget and deprecation owners with review deadlines before activation. Missing or expired ownership is a publication/activation failure, not an administrative warning.

### Risk 128: Technical Success Masks Customer Process Bypass

Response:

Treat training, accessible instructions, customer process ownership, communications, support routing, bypass detection, adoption measurement and feedback as release evidence. Persistent spreadsheet/email/incumbent-UI bypass fails the pilot outcome even if technical transactions pass.

### Risk 129: Requirements Become Orphaned Across Registers and Executable Artifacts

Response:

Assign stable requirement identifiers and generate a mandatory crosswalk from charter and coverage responsibility through specification, owner, phase depth, schema/code, conformance test and evidence. Reject missing, falsely complete, contradictory or retired-but-reachable traces.

### Risk 130: A Validly Signed but Stale Control Bundle Re-Enables Revoked Behavior

Response:

Bind activation to a monotonic per-scope epoch and anti-rollback floor, validity interval, parent bundle, signed revocation set and dual-approved rollback receipt. Signature validity alone never authorizes an older state.

### Risk 131: External Effects Execute Out of Order or After an Authority Handoff

Response:

Bind operations to an external resource key, causal predecessor/sequence, expected provider version where supported, source-authority decision, writer-fence epoch and cutover watermark. Revalidate immediately before send; drain, cancel, quarantine or re-plan pre-handoff work.

### Risk 132: Architecture Prose Is Misreported as a Defined or Implemented Capability

Response:

Apply the coverage-matrix definition mechanically. Atlas prose alone is `PARTIAL` or `DEFERRED`; `DEFINED` requires a focused contract or equivalent executable schema/tests, and implementation claims require current evidence.

### Risk 133: First Write Authority Precedes Pilot Operational Readiness

Response:

Block Gate B without staffed on-call, support hours, JIT diagnostics, incident/advisory routes, continuity/redrive/rollback procedures, service profile and exercised exit runbook.

### Risk 134: Legacy Demo Identity and Global Object Lookup Survive the Go Cutover

Response:

Prohibit demo actor headers and ID-only tenant-owned repository APIs in release artifacts. Require verified human/workload identity, logical placement, tenant-scoped repositories, composite foreign keys, database isolation and negative tests.

### Risk 135: An External Effect Exists Without Durable Local Transaction Evidence

Response:

Commit immutable local intent/plan/domain/ledger/projection/outbox state atomically before dispatch. Classify uncertain responses as ambiguous, observe before retry, and never infer consistency from provider acceptance.

### Risk 136: SchemaFlux Is Assumed to Provide a Compiler It Does Not Implement

Response:

Qualify the actual library surface. Build or add a pure-Go offline HCM compiler adapter with strict source decoding, descriptor resolution, deterministic normalization/emission and drift tests before claiming compilation.

### Risk 137: Demo Legal Rules or Binary Floating-Point Money Escape Into Authority

Response:

Mark existing termination/compensation fixtures non-authoritative. Replace universal notice/final-pay/benefit assumptions with jurisdiction/rule resolution and use fixed-decimal money with explicit scale, currency, basis and rounding.

### Risk 138: Offline or Shared-Device Use Creates an Unreviewed Approval or Privacy Breach

Response:

Limit high-risk offline behavior to read/draft. Require online revalidation for submit/approve/sign/commit, device-bound encrypted storage, session/user partitioning, expiry/revocation and verified wipe on logout or user switch.

### Risk 139: Cross-Tenant Artifact Deduplication Defeats Deletion or Isolation

Response:

Prohibit cross-tenant deduplication for sensitive artifacts, or use tenant-keyed encryption/reference wrappers with safe reference counting. Test deleting one tenant while preserving another through restore and re-delete.

### Risk 140: The Pilot Duplicates Incumbent Capability or Cannot Support Its Economics

Response:

Require licensed-edition/topology qualification, an independently owned downstream effect, real handoff/observation, transaction volume, customer labor, cost-to-serve, contribution margin and numeric stop/reselect thresholds before Gate A.

### Risk 141: Lineage or Process Variation Is Presented as Causation

Response:

Use `TRIGGERED`, `DERIVED_FROM` and `ASSOCIATED_WITH` for provenance. Reserve causal language for governed evaluations with comparison design, assumptions, confounders, uncertainty, claim-strength classification and contestability.
