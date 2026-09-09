# Platform Foundation Gap-Closure Contracts

This specification closes production responsibilities that were absent or only
implicit after the plane model and capability coverage review. A name in a
diagram is not a contract. Each system below therefore identifies its authority,
state, API boundary, failure behavior, evidence, and Phase 1 depth.

## Contract Completeness Rule

A platform responsibility is `DEFINED` only when all of these questions have an
answer:

```text
Who owns it?                 authority and accountable service
What does it own?           canonical objects and state machine
How is it invoked?          typed capabilities and events
What may it call?           dependency and side-effect boundary
How does it fail?           fail-open/closed/stale/queued behavior
What proves it happened?    ledger, journal, receipt, or telemetry evidence
How is it secured?          identity, authorization, classification, retention
When is it implemented?     Phase 1 depth and promotion gate
```

No implementation may fill an unspecified field with an undocumented default.
An unresolved value is a compile-, publication-, or execution-time error,
according to where the uncertainty is discovered.

## 1. Control-Plane Publication, Distribution, and Bootstrap

The Control Plane owns desired configuration. Runtime planes own the safely
applied configuration they are currently using. Publication and application are
separate facts.

```text
author/config producer
        |
        v
ConfigRevision --validate--> ConfigBundle --sign--> Publication
                                                    |
                                              scoped rollout
                                                    |
                         +--------------------------+------------------+
                         v                          v                  v
                       Cell A                     Cell B             Cell C
                         |                          |                  |
                   ApplyReceipt              ApplyReceipt       ApplyReceipt
                         +--------------------------+------------------+
                                                    |
                                             convergence view
```

Canonical objects:

```text
ConfigRevision
  config_id, revision, owner, schema_version, content_hash

ConfigBundle
  bundle_id, scope, activation_epoch, parent_bundle, not_before, expires_at,
  manifest, dependency_lock, revocation_set, content_hash, signature

ConfigPublication
  publication_id, bundle_id, scope, required_minimum_epoch,
  rollout_policy, desired_at

ConfigActivation
  cell_id, workload_id, applied_bundle, applied_epoch, anti_rollback_floor,
  prior_bundle, rollback_receipt_ref, applied_at, validation_result,
  health_result, status

BootstrapManifest
  trust_roots, discovery_endpoints, minimum_safe_config,
  minimum_activation_epochs, revocation_roots, recovery_authority,
  expiry, signature
```

Required capabilities:

```text
control.config.validate
control.bundles.compile
control.bundles.sign
control.publications.plan
control.publications.activate
control.publications.pause
control.publications.rollback
control.activations.status
control.bootstrap.verify
```

Invariants and failure semantics:

- A bundle is hermetic: every schema, policy, capability, rule, feature, and
  connector dependency is version-locked or explicitly external with a health
  contract.
- A workload reports the exact applied fingerprint. Desired state is never
  presented as applied state.
- Security deny rules and revocations fail closed. A bounded stale snapshot may
  be used only when its contract declares `USE_STALE`, its maximum age has not
  elapsed, and the evidence records that degraded decision.
- No critical command requires a synchronous read from the remote Control Plane.
  Runtime uses a verified local snapshot.
- Rollout is canary-first, automatically pauses on loss of health or operator
  control, and retains the last known-good bundle.
- A valid signature does not authorize replay or rollback. Each scope has a
  monotonic activation epoch and anti-rollback floor. Expired, revoked, unrelated
  or below-floor bundles are rejected even if their signatures remain valid.
- Rollback below the current floor requires a separately dual-approved,
  time-bounded `RollbackReceipt` naming the exact target epoch, reason, affected
  scope, safety evidence and new floor. Emergency rollback cannot resurrect a
  revoked credential, capability or deny-policy dependency.
- Bootstrap has a separately protected root of trust and an exercised recovery
  path; ordinary tenant configuration cannot replace it.

Phase 1 implements signed, version-locked bundles for capability, schema,
workflow, AuthZ, feature, connector, and reference-data configuration in the
pilot cell. Multi-cell rollout is conformance-only.

## 2. Digital Identity, Federation, Session, and Recovery Lifecycle

The Identity Plane answers who an actor is and how strongly that identity is
currently established. It does not decide business permission.

```text
proof/enroll -> account -> bind authenticator -> authenticate -> session
                    |                                  |
                    |                                  +-> step-up
                    +-> link federation                   |
                    +-> recover --------------------------+
                    +-> suspend -> disable -> terminate
```

Canonical objects:

```text
DigitalIdentity
SubscriberAccount
AuthenticatorBinding
FederationTrust
FederatedIdentifierLink
Session
TokenFamily
RecoveryCase
IdentityAssuranceEvidence
```

Every `PrincipalContext` carries:

```text
principal_id
principal_type        HUMAN | SERVICE | AGENT | APPLICATION | CONNECTOR
identity_assurance    IAL or equivalent
authentication_level  AAL or equivalent
federation_level      FAL or equivalent
auth_methods[]
session_id
authenticated_at
step_up_expires_at?
delegation_chain[]
tenant_binding
```

Required capabilities:

```text
identity.accounts.create|suspend|disable|terminate
identity.authenticators.bind|rotate|revoke
identity.sessions.create|refresh|step_up|revoke|inspect
identity.federation.trust.publish|revoke
identity.identifiers.link|unlink
identity.recovery.start|verify|complete|deny
identity.assurance.explain
```

Session/token rules are explicit:

- Authorization Code flows use PKCE; redirect URIs and anti-CSRF state are
  exact-validated; OpenID Connect flows validate nonce.
- Tokens are audience-restricted. High-risk machine tokens are sender-constrained
  with mTLS or DPoP where supported.
- Refresh-token families rotate and detect replay; replay revokes the family and
  creates a security signal.
- Session inactivity, absolute lifetime, assurance lifetime, revocation
  propagation SLA, and offline behavior are configured by risk class.
- Account recovery cannot silently weaken required assurance. It records the
  evidence, operator/provider, attempts, fraud signals, notifications, and the
  authenticators invalidated or retained.
- Linking and unlinking federated identities requires an authenticated session
  of adequate strength and produces user-visible security notice.
- `DISABLED` preserves recovery state according to retention policy;
  `TERMINATED` explicitly defines whether identifiers and recovery artifacts
  remain.

Phase 1 implements workforce SSO, service/workload identity, session revocation,
step-up for critical actions, and a manual governed recovery path. Consumer-scale
proofing and multiple federation brokers are deferred.

## 3. Hostile Content Ingress and Quarantine

All bytes originating outside a trusted compiled artifact boundary are
untrusted, including uploads, resumes, email attachments, SFTP files, API
documents, RAG material, e-sign artifacts, and generated agent files.

```text
untrusted bytes
      |
      v
RECEIVED -> QUARANTINED -> VALIDATING -> SCANNING -> TRANSFORMING
                    |          |             |
                    +----------+-------------+
                               v
                       SAFE | REJECTED
                               |
                    safe derivative promotion
                               |
                    index / RAG / workflow use
```

Canonical objects:

```text
ContentIngress
QuarantinedObject
ContentInspection
SafeDerivative
ContentPromotion
RescanCampaign
```

Required controls:

- Extension allowlist, independent MIME/signature validation, generated storage
  names, per-file and per-request size limits, and authenticated/authorized
  uploader.
- Storage outside executable/web roots with separate quarantine credentials.
- Malware scanning and sandbox analysis; content-disarm-and-reconstruction for
  supported document formats; active content and macros are removed or blocked.
- Archive depth, expanded-size, compression-ratio, file-count, parser CPU, and
  parser-memory limits prevent decompression and parser exhaustion.
- Parsing/extraction occurs in isolated, non-networked workers with short-lived
  identities and no domain-write capability.
- Classification, DLP, residency, retention, legal hold, and original-versus-
  derivative lineage are applied before promotion.
- Only a `SAFE` derivative may enter search, semantic indexing, RAG, forms,
  messaging, or a domain workflow. The original remains restricted evidence.
- Scanner, signature, extraction, or policy upgrades can schedule replay-safe
  rescans and revoke earlier promotions without erasing history.

Failures are fail-closed for content use. Scanner outage leaves content in
`QUARANTINED`; it does not promote on timeout. Emergency manual release requires
separate authority, reason, time limit, immutable evidence, and downstream taint.

Phase 1 implements CSV and document quarantine, type/size/signature validation,
malware scanning, archive limits, classification, and safe promotion for the
pilot import and evidence paths.

## 4. Global Reference Dataset Lifecycle

Global reference datasets are versioned dependencies, not operating-system
ambient state.

```text
IANA tzdb / Unicode CLDR / ISO / approved geography source
                          |
                    ingest + verify
                          |
                 ReferenceDatasetVersion
                          |
               impact + conformance tests
                          |
                 publish / tenant pin
```

Owned datasets include:

```text
time-zone rules and aliases
locale identifiers, number/date/name/address formats
country and subdivision codes
currency codes and minor-unit metadata
approved holiday/business-calendar sources
postal/geospatial boundary data
```

Each version records source, retrieval time, publisher signature/checksum,
license, effective facts, normalization transform, parent version, and consumers.
Capabilities expose `reference.datasets.read`, `diff`, `impact`, `publish`, and
`resolve`.

Temporal rules:

- Every calculated local instant records zone ID and tzdb version.
- Historical executions retain their original dataset fingerprint.
- A future timer affected by a tzdb/calendar change follows its declared policy:
  `PIN_ORIGINAL`, `RECALCULATE_CURRENT`, or `REQUIRE_REVIEW`.
- A locale controls presentation only. It never supplies legal jurisdiction.
- Currency metadata does not supply exchange rates; FX is a separately versioned
  business input.
- Boundary/geocoder uncertainty returns confidence and evidence; it cannot be
  silently coerced into a legal jurisdiction.

Phase 1 pins Go runtime timezone data plus explicit tzdb/locale/currency dataset
fingerprints and tests future-effective promotion timers. Full geographic
boundary operations are deferred.

## 5. Service and Endpoint Discovery with Dependency Inventory

Logical dependencies are named independently from addresses.

```text
logical capability dependency
           |
           v
EndpointSet(region, cell, protocol, identity, schema)
           |
        health + policy
           |
           v
selected endpoint -> authenticated call -> observation
```

`ServiceDependency` records owner, criticality, tenant/cell scope, required
workload identity, allowed network path, protocol, schema range, timeout,
staleness, fallback, and SLO. `EndpointLease` records resolved endpoints, TTL,
health, region, cell, and trust bundle version.

No service embeds production endpoint URLs or trusts DNS/network location as
identity. Expired discovery data follows the declared dependency behavior:
`FAIL_CLOSED`, `USE_STALE_UNTIL`, `QUEUE`, or `DEGRADE`; there is no universal
fallback. The dependency graph supports vulnerability, configuration, outage,
tenant-relocation, and data-egress impact traversal.

Phase 1 implements dependency manifests and cell-local endpoint resolution for
the pilot services and connector. Dynamic multi-region routing is deferred.

## 6. Idempotency-Key Lifecycle

Idempotency is durable business state, not an in-memory cache convention.

```text
(tenant, capability, semantic_effect_scope, idempotency_key)
                         |
             +-----------+-----------+
             v                       v
       first request              replay
       bind request hash          compare hash
             |                       |
       result/effect ref       same -> same result
                              different -> CONFLICT
```

`IdempotencyRecord` contains tenant, capability/version, semantic effect scope,
key, canonical request hash, originating and current authorized principal/
delegation evidence, execution/transaction/node/operation reference, terminal
response reference, state, created time, replay count, retention class, expiry,
and tombstone.

Rules:

- A key reused with different canonical input is rejected.
- `IN_PROGRESS` replay returns the same execution reference; it does not launch a
  second effect.
- Records outlive the maximum retry/redelivery window of their source. Financial,
  government, and irreversible actions may require permanent compact tombstones.
- Expiry behavior is capability-specific: reject, require a new key, or allow a
  new execution only after an explicit duplication-risk check.
- Connector idempotency never assumes the provider honored its own key; observed
  external state is reconciled.
- Tenant namespaces cannot collide. A change of executor, workflow worker,
  connector worker or governed repair principal does not create a new namespace
  for the same semantic effect.
- Principal belongs in the uniqueness scope only when separate principals are
  intentionally producing distinct business decisions, such as independent
  approval votes. Authorization is always re-evaluated even when an idempotent
  result is replayed.

Phase 1 implements this lifecycle for commands, workflow node effects, connector
operations, message intents, imports, and outbox consumers.

## 7. Certificate and Trust-Bundle Lifecycle

The PKI service owns issuance, distribution, validation, rotation, revocation,
and compromise response—not only secret storage.

```text
request -> approve/policy -> issue -> distribute -> activate
                                      |
                           rotate / revoke / expire
                                      |
                              validation evidence
```

Objects include `CertificateProfile`, `CertificateInstance`, `TrustBundle`,
`Revocation`, and `RotationCampaign`. Every instance identifies subject workload
or partner, tenant/cell scope, algorithm, key custody, issuer, usages, validity,
deployment locations, and replacement.

Trust-bundle rollout supports overlap and dual validation. Revocation has an
explicit propagation SLA and works when an issuer or ordinary control-plane path
is unavailable. OCSP/CRL or equivalent status policy is defined per certificate
class. Compromise creates a graph-based impact set, emergency replacement plan,
and evidence package. Crypto-agility requires algorithm identifiers and versioned
envelopes rather than hardcoded assumptions.

Phase 1 implements workload mTLS, connector/client certificates where needed,
ledger/config signing certificates, rotation, and emergency revocation drills.

## 8. Secure Deletion, Media Sanitization, and Tenant Exit

Deletion is a cross-plane workflow whose completion must be proven.

```text
deletion request
      |
hold + retention + authority evaluation
      |
DeletionPlan
      +-> canonical rows / payload vault
      +-> object versions
      +-> projections / search / vectors / analytics / cache
      +-> exports and connector-held copies where contract permits
      +-> encryption keys and backups
      |
verification -> DeletionCertificate | BLOCKED_WITH_REASONS
```

`DeletionPlan` declares subject/scope, legal basis, holds, stores, method,
dependencies, expected backup expiry, verification, approvers, and exceptions.
Methods distinguish logical deletion, purge, cryptographic erase, media
sanitization, and physical destruction. Crypto-shredding is valid only when key
scope and all recoverable copies are known.

The ledger retains a minimally necessary tombstone/evidence event without
retaining deleted payload. Legal holds block destruction but not unrelated tenant
exit steps. Backups record the date after which deleted data cannot reappear;
restores reapply deletion manifests before service. A tenant exit cannot be
declared complete while undocumented copies, active connectors, credentials,
webhooks, support grants, or unexpired derived datasets remain.

Phase 1 implements subject deletion manifests for pilot data, connector and
credential shutdown, derived-store purge, backup re-delete behavior, and signed
verification. Physical-media disposition remains an infrastructure-provider
control with imported evidence.

## 9. Customer Incident Communication and Status

Operational incident communication is separate from employee messaging and from
legally mandated breach notice.

```text
Incident
   +-> internal responder timeline
   +-> affected tenant calculation
   +-> customer advisory approval
   +-> status publication / direct notice
   +-> update cadence
   +-> resolution + post-incident report
   +-> regulatory/privacy notice workflow when required
```

`IncidentCommunication` records incident, audience and affected-service scope,
classification, facts-known timestamp, approved wording/version, channels,
publisher, update deadline, delivery observations, correction, and final report.
Customer communications cannot expose another tenant, security-sensitive exploit
detail, or unverified root cause. Status assertions are generated from reviewed
incident state, not raw alerts. Missed update deadlines escalate as obligations.

Phase 1 implements tenant-scoped advisories and status history for pilot-impacting
incidents. Public multi-region status automation is deferred.

## 10. Accessibility Assurance Platform

Accessibility is a release property across UI, generated workspaces, forms,
authentication, documents, messages, and human tasks.

```text
semantic definition -> GWC rendering -> static checks -> keyboard/AT checks
                                          |
                                   conformance evidence
                                          |
                                   release gate / defect
```

The platform owns accessible component primitives, focus/order behavior, error
association, status announcements, contrast and target-size tokens, language and
direction metadata, reduced-motion behavior, accessible authentication paths,
document/message templates, and assistive-technology test fixtures.

Agent-generated or customer-configured pages may only compose approved semantic
GWC components. A visual preview is not evidence. Phase 1 requires WCAG 2.2 AA
conformance for the Promotion workspace and authentication path, keyboard and
screen-reader scenarios, zoom/reflow testing, and accessible generated evidence.

## 11. Platform Upgrade and Durable-State Migration

Release deployment, API/schema evolution, and durable data transformation are
one coordinated compatibility problem.

```text
ReleaseCompatibility
   -> expand schema/API
   -> deploy mixed-version readers/writers
   -> backfill/shadow
   -> validate adoption watermarks
   -> cut over behavior
   -> contract/remove old representation
```

Canonical objects are `ReleaseCompatibility`, `MigrationPlan`, `MigrationStep`,
`Backfill`, `Cutover`, `Validation`, `RollbackBoundary`, and
`AdoptionWatermark`. The Upgrade Coordinator exposes
`upgrades.plan|validate|start|pause|resume|abort|cutover|status|rollback`.

The Platform Upgrade Coordinator owns this lifecycle:

```text
DRAFT -> VALIDATED -> APPROVED -> CANARY -> EXPANDING -> BACKFILLING
  -> CUTOVER_READY -> CUTOVER -> CONTRACTING -> COMPLETE

Any active state may enter PAUSED, ABORTED, or REPAIR_REQUIRED according to its
declared rollback boundary.
```

Author, compatibility approver, rollout executor, and irreversible-cutover
approver are distinct capabilities. Production cutover requires step-up and dual
control. The coordinator depends on Build Provenance, Schema/Config Registries,
Cell Placement, Admission/Drain, Backup/Recovery, Conformance, and each migrated
store owner; failure or stale watermarks from a required dependency blocks
progress instead of assuming adoption.

Every release declares minimum and maximum compatible versions for binaries,
Protobuf/API readers and writers, ledger event reducers, projections, workflow
definitions and live instances, outbox producers/consumers, config bundles,
connectors, and stored representations. Expand/contract is the default. Dual
read/write exists only where declared with comparison, divergence, and removal
criteria. Backfills are idempotent, checkpointed, tenant/cell throttled, isolated
from P0/P1 capacity, and preserve source/version lineage.

An irreversible transformation names the exact rollback boundary, backup and
restore point, approval, shadow validation, abort criteria, and repair procedure.
Old and new binaries may coexist only inside the declared version-skew matrix.
Unknown writer versions fail closed instead of corrupting durable state.

The durable migration journal records plan and release fingerprints, approvals,
per-step state, cell/tenant scope, worker/writer version observations, schema and
config watermarks, backfill checkpoints/counts/hashes, shadow comparisons, health
and invariant results, pauses/aborts, irreversible decision, cutover, rollback or
repair, and completion. It is retained at least as long as any affected durable
representation or workflow remains interpretable. Migration workers use scoped
short-lived identity and cannot exceed the plan's stores, tenants, or steps.

Phase 1 must demonstrate a rolling Go binary/schema upgrade, mixed-version
command rejection outside the matrix, projection rebuild, workflow-instance
continuity, and rollback before the first irreversible step.

## 12. Consent, Notice, Objection, and Preference Lifecycle

Consent is one possible processing authority; it is not a generic permission bit
and never substitutes for another lawful basis required by law or contract.

```text
processing purpose + legal context
              |
     ProcessingAuthority
       + NoticePresentation
       + ConsentDecision? / Objection? / Restriction?
              |
        EffectivePermission
              |
     downstream consumer receipts
              |
 withdrawal/change -> obligations -> reconciliation
```

The Privacy Preference Service owns `ProcessingAuthority`, `NoticeVersion`,
`NoticePresentation`, `ConsentDecision`, `CommunicationPreference`, `Objection`,
`Restriction`, `Withdrawal`, `ConsumerBinding`, and `PropagationReceipt`.

Each record explicitly carries subject, controller, purpose, data categories,
processing operation, recipient/provider classes, jurisdiction, source authority,
notice/version/language, decision/evidence, effective interval, expiry,
withdrawal semantics, downstream consumers, retention, and supersession.

Capabilities are `privacy.authority.resolve|explain`,
`privacy.notices.present|acknowledge`, `privacy.consent.grant|deny|withdraw`,
`privacy.objections.submit|resolve`, `privacy.restrictions.apply|release`, and
`privacy.propagation.status|reconcile`.

Unknown authority, expired notice, or unresolved restriction is fail-closed for
optional processing. Withdrawal creates typed stop/delete/restrict/notify
obligations and waits for consumer acknowledgements; incomplete propagation
remains visible and escalated. Messaging preferences cannot suppress mandatory
legal notices. Employment consent is not presumed freely given merely because a
worker used the system.

Phase 1 implements notice/version evidence and communication preferences required
by the pilot. Optional AI/RAG consent and withdrawal are conformance-only unless
the pilot processes data on that basis.

## 13. Identity-Provider Outage Continuity

The Identity Continuity Service owns `ContinuityProfile`, `ProviderIncident`,
`ContinuityActivation`, `EmergencyPrincipalGrant`, and
`RecoveryReconciliation`. Every capability risk class declares a profile:

```text
NORMAL
  federation available

DEGRADED_IDENTITY
  existing verified session? -> bounded continue or require step-up
  new ordinary session?      -> deny
  P0 emergency action?       -> separately controlled emergency identity

RECOVERED
  reconcile revocations, sessions, actions, and federation keys
```

Canonical lifecycle:

```text
NORMAL -> SUSPECTED -> DECLARED -> DEGRADED_IDENTITY -> RECOVERING
       -> RECONCILING -> NORMAL

The incident may enter CONTAINED or REPAIR_REQUIRED at any time.
EmergencyPrincipalGrant:
REQUESTED -> DUAL_APPROVED -> ACTIVE -> EXPIRED | REVOKED -> REVIEWED
```

Capabilities are `identity_continuity.declare|activate|contain|status|recover|
reconcile`, `identity_continuity.profiles.validate|publish`, and
`emergency_principals.request|approve|activate|revoke|inspect`. The Identity
Incident Commander activates a declared profile; Security and the affected
tenant's authorized emergency approver jointly approve a grant. The requester,
approvers, and acting emergency principal are distinct.

The profile specifies existing-session maximum age, cached assertion/key lifetime,
revocation uncertainty behavior, step-up availability, tenant-federation
isolation, required fail-closed capabilities, emergency principal eligibility,
dual control, offline trust roots, maximum emergency duration, containment, and
post-recovery reconciliation.

No new ordinary session is created from a stale federation assertion. Cached keys
verify only still-valid existing assertions within the profile. P0 payroll,
access revocation, integrity repair, and incident response may use separately
provisioned, hardware-bound, JIT emergency identities only where the customer and
platform policy both allow it. Those identities cannot browse ordinary workforce
data and every action requires reason, dual control, recording, expiry, and
after-action review.

Dependencies are trusted time, offline trust roots, hardware authenticators,
session/revocation state, incident management, customer emergency contacts, and
immutable evidence storage. If trusted time, offline roots, dual approval, or
recording is unavailable, the emergency grant fails closed. A provider outage is
isolated by tenant/federation; it cannot trigger global emergency access.

Evidence records incident/provider/tenant scope, detection and declaration,
profile/version, session and key decisions, rejected logins, grant request and
approvals, hardware identity, every emergency capability call, revocation/expiry,
provider recovery observations, session/revocation reconciliation, anomalies,
and after-action closure. Evidence APIs are `identity_continuity.evidence.read|
export` under incident-purpose authorization and retention.

Phase 1 exercises IdP outage, existing-session expiry, emergency revocation,
prohibited ordinary login, P0 emergency access, and recovery reconciliation.

## 14. Outbound Destination Trust and SSRF Boundary

Every outbound network effect passes the shared Egress Gateway using a published
`DestinationTrust`; connector, webhook, messaging, model, MFT, filing, and support
code cannot open arbitrary sockets.

```text
DestinationRequest
      |
normalize scheme/host/port/path
      |
ownership + purpose + classification + residency policy
      |
resolve all A/AAAA -> classify addresses -> pin request resolution
      |
TLS/proxy/trust verification
      |
send with redirects disabled or revalidated hop-by-hop
      |
DestinationObservation + immutable evidence
```

`DestinationTrust` records normalized scheme, host, port, allowed path/method
classes, tenant ownership proof where applicable, resolved address policy,
private/loopback/link-local/multicast/metadata exclusions, DNS TTL and revalidation,
redirect policy, proxy identity, TLS name/trust/pin profile, region/residency,
eligible data classifications/purposes, credential reference, review, expiry, and
revocation.

The Destination Trust Service owns this lifecycle:

```text
DRAFT -> VERIFYING -> APPROVED -> ACTIVE
                         |
              DRIFTED -> QUARANTINED -> ACTIVE | REVOKED
                         |
                      EXPIRED
```

Capabilities are `destinations.propose|verify|approve|activate|revalidate|
quarantine|revoke|read|explain` and `egress.send|observe`. Endpoint owner,
security approver, and invoking workload are separate identities. High-risk
destinations require dual approval and step-up. A connection administrator may
propose but cannot approve its own destination. There is no emergency bypass to
arbitrary network access; emergency destinations use the same verification and a
shorter expiry.

DNS and connection use the same validated resolution to prevent time-of-check/
time-of-use rebinding. Every redirect repeats the full check; automatic redirect
following is off by default. IPv4, IPv6, alternate encodings, embedded credentials,
non-HTTP schemes, Unix/file sockets, and cloud metadata endpoints are normalized
or rejected before policy comparison. A DNS/certificate/ownership change queues
or quarantines the effect pending revalidation; it never silently expands trust.

Typed failures distinguish invalid syntax/scheme/port, unproved ownership,
disallowed address class, DNS drift/rebinding, unsafe redirect, proxy mismatch,
TLS/trust failure, expired/quarantined/revoked trust, residency/classification/
purpose denial, timeout, and ambiguous delivery. Retry occurs only after policy
and idempotency evaluation; validation failure is never blindly retried.

Immutable evidence records DestinationTrust/version, requester workload and
tenant, purpose/classification, normalized request target, DNS answers and TTL,
pinned connection address, proxy/TLS peer and trust bundle, redirect hops, policy
decision, bytes classification/count, timestamps, response class, delivery
ambiguity, and correlation. Payload retention follows its data class; the
resolution/security journal retains enough detail for SSRF investigation without
copying unrestricted HCM content.

Phase 1 routes the pilot connector, webhook, email provider, and model provider
through this boundary and includes negative tests for private ranges, metadata,
rebinding, unsafe redirects, Unicode/encoded hosts, and unapproved proxies.

## 15. Normative Documentation Integrity

Planning specifications are versioned control artifacts. Encoding damage,
unresolvable links, and ambiguous diagrams are release defects.

```text
source markdown
   -> strict UTF-8 validation
   -> mojibake/replacement-character rejection
   -> fence/link/anchor checks
   -> declared ASCII-block validation
   -> generated index + manual atlas review
   -> documentation evidence
```

The documentation conformance job rejects invalid UTF-8, known mojibake patterns,
replacement characters, unbalanced fences, broken relative links/anchors,
duplicate normative identifiers, coverage-number gaps, and non-ASCII glyphs in
blocks explicitly labeled `text-ascii`. Existing Unicode prose is permitted only
when correctly encoded. Normative diagrams use strict ASCII where portability is
required; decorative diagrams may use Unicode only when accompanied by an
accessible textual description.

Every contract has owner, status, last review, source links, and supersession.
Generated inventories never overwrite authoritative prose without a reviewed
change. Phase 1 CI runs these checks on every planning or schema change.

## Source-Guided Decisions

These contracts use current primary guidance as constraints, not as substitutes
for product-specific decisions:

- NIST SP 800-63-4 separates identity proofing, authentication, and federation
  assurance and explicitly covers authenticator, session, account-recovery, and
  revocation lifecycles: <https://pages.nist.gov/800-63-4/sp800-63.html>.
- RFC 9700 establishes current OAuth security practices including PKCE, audience
  restriction, sender-constrained tokens, and refresh-token protection:
  <https://www.rfc-editor.org/rfc/rfc9700.html>.
- OWASP's file-upload guidance supports allowlisting, independent type/signature
  validation, isolated storage, malware scanning, CDR, and archive limits:
  <https://cheatsheetseries.owasp.org/cheatsheets/File_Upload_Cheat_Sheet.html>.
- Google's SRE configuration guidance treats validation, hermetic packaging,
  canarying, rollback, and operator control as configuration safety properties:
  <https://sre.google/workbook/configuration-design/>.
- IANA tzdb is periodically updated when political bodies change UTC offsets,
  boundaries, or daylight-saving rules, which is why the exact dataset version
  must be preserved: <https://www.iana.org/time-zones>.
- NIST SP 800-88 Rev. 2 distinguishes sanitization methods and requires a
  sensitivity-appropriate sanitization program:
  <https://csrc.nist.gov/pubs/sp/800/88/r2/final>.
- NIST SP 800-61 Rev. 3 integrates preparation, response, and recovery into
  cybersecurity risk management:
  <https://csrc.nist.gov/pubs/sp/800/61/r3/final>.
- WCAG 2.2 is the accessibility conformance baseline:
  <https://www.w3.org/TR/WCAG22/>.
- OWASP's SSRF guidance recommends positive destination allowlists, validation of
  all resolved addresses, disabled redirects, and defenses against DNS pinning:
  <https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html>.
- Kubernetes publishes explicit component version-skew and upgrade-order rules;
  Human Capital Management Suite applies the same principle of declared compatibility to its own
  binaries and durable state: <https://kubernetes.io/releases/version-skew-policy/>.
- NIST's Privacy Framework includes processing visibility, provenance, disclosure
  records, and mitigation such as consent withdrawal and deletion:
  <https://www.nist.gov/privacy-framework>.

## Go-Only Realization

All services, workers, validators, compilers, administration tools, and UI in
this specification are authored in Go. Protobuf is the canonical interface
contract; grpcbridge exposes approved HTTP/browser edges; GoWebComponents renders
administrative and end-user surfaces; SchemaFlux compiles structured catalogs,
dependency graphs, validation plans, and evidence manifests. No TypeScript,
Node.js, npm, React, or Vite runtime/build path is introduced.
