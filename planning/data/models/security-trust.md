# Security, Trust, Secrets and Egress Entities

These entities make human, workload, support, cryptographic and outbound trust
explicit. Raw secret, key, token, certificate private material, sensitive
payloads, and authentication proofs are referenced by opaque handles only and
must never be copied into ledgers, logs, traces, agent prompts, configuration
packages, or support bundles.

## Authorization policy, simulation and delegation

```text
AuthorizationPolicySnapshot
  snapshot_id, tenant/org/environment/policy release
  compiled rule/relationship/reference digests
  source/build/signature/provenance, effective interval
  graph/source watermarks, status

AuthorizationRule
  rule_id, policy snapshot, priority/composition
  principal/resource/capability/field/purpose/channel predicates
  condition/risk/population/effective-time predicates
  allow/deny/restrict/unknown outcome and obligations

AuthorizationObligation
  obligation_id, decision/rule
  STEP_UP | APPROVAL | MASK | PURPOSE_LIMIT | DESTINATION_GATE |
  RECORD_REASON | DUAL_CONTROL | EXPIRY
  parameters/satisfaction/evidence/invalidators

AuthorizationSimulation
  simulation_id, hypothetical principal/session/workload/delegation
  resource/field/capability/purpose/channel/context inputs
  pinned policy/graph/source watermarks, assumptions/unknowns
  result/trace/expiry, explicit no-side-effect proof

AuthorizationExplanation
  explanation_id, decision/simulation/audience
  redacted matched/failed rule tree, restrictions/obligations
  evidence digests/unknowns, policy-disclosure filtering, expiry

DelegationRequest
  request_id, delegator/delegate/scope/purpose
  effective interval/transitivity/depth/use limits
  justification/approval/SoD/status

DelegationDecision
  decision_id, request/proposal digest
  delegator authority snapshot, approver/SoD checks
  approve/deny/modify result, reason/validity/invalidators

DelegationChain
  chain_id, root grant, ordered grant refs[]
  effective intersection of tenant/org/capability/resource/field/
  purpose/population/channel scopes, depth/cycle validation

DelegationUse
  use_id, grant/chain/principal/session/intent
  capability/resource/purpose, authorization decision
  used-at/count/limit state/evidence

DelegationRevocation
  revocation_id, grant/descendant grants
  actor/reason/effective time/revocation epoch
  in-flight decision/session invalidation refs
```

## Sessions, authenticators and privileged access

```text
AuthenticationAttempt
  attempt_id, claimed principal/issuer/channel/device/network
  method/authenticator/provider assertion/key version
  started/completed times, risk/result/error/evidence

AuthenticatorBinding
  binding_id, principal/authenticator type/provider
  assurance/attestation/device, enrolled/verified times
  lifecycle/revocation/recovery constraints

SessionTokenFamily
  family_id, session/principal/issuer/audience
  opaque token hashes/rotation lineage only
  issued/last-use/idle/absolute expiry, revocation epoch/status

StepUpChallenge
  challenge_id, session/capability/risk reason
  required assurance/methods/nonce/expiry
  attempts/result/evidence, replay prevention

SessionRevocation
  revocation_id, session/token family/principal scope
  actor/reason/effective time/epoch
  propagation/acknowledgement/non-use verification refs

PrivilegedAccessRequest
  request_id, requester/customer/security approver refs
  ticket/reason/tenant/org/resource/capability scope
  duration/device/recording/dual-control requirements
  step-up/risk/status

PrivilegedAccessDecision
  decision_id, request/proposal digest
  customer/security/owner approvals and SoD
  allowed scope/TTL/conditions/invalidators/result

PrivilegedAccessSession
  session_id, decision/acting principal/support principal
  tenant/resource scope, start/expiry/revocation
  hardware step-up/device/network, recording/action refs
  monitoring/after-action/reconciliation/status

AfterActionReview
  review_id, privileged/emergency session
  action/evidence/recording completeness
  scope violations/data access/egress findings
  reviewer/result/remediation/closure
```

## Workload zero trust

```text
WorkloadIdentity
  identity_id, service/runtime instance/namespace/cell/tenant binding
  issuer/subject/certificate/key refs, image/build provenance
  issued/effective/expiry/revocation, lifecycle

WorkloadAttestation
  attestation_id, workload/node/runtime/image refs
  claims/nonce/verifier/trust-root versions
  verified-at/valid-until/result/evidence

ServiceAuthorizationPolicy
  policy_id, caller/callee workload selectors
  capability/resource/purpose/audience/tenant/destination constraints
  network/egress requirements, effective interval/version

ServiceAuthorizationDecision
  decision_id, caller/callee identities/attestations
  request capability/resource/purpose/tenant/audience
  policy/network/credential lease refs
  allow/deny/restrict/unknown, trace/expiry/invalidators

NetworkIdentityBinding
  binding_id, workload identity/network namespace/address/policy refs
  effective interval, attestation/observation/status

WorkloadCredentialLease
  lease_id, workload/secret-or-key version/purpose/destination
  issued/expiry/revocation/fence/use-limit
  access decision/use receipts/status
```

## Secrets, keys, certificates and crypto agility

```text
SecretReference
  secret_ref_id, logical purpose/owner/tenant/cell/region
  custody provider/path handle, classification/lifecycle

SecretVersion
  version_id, secret ref/custody version handle
  created/activated/retire/revoke times
  consumer bindings/rotation/compromise refs, status

SecretAccessPolicy
  policy_id, secret ref
  allowed workload/purpose/destination/region selectors
  step-up/approval/lease/usage/recording requirements

CredentialLease
  lease_id, secret or key version/consumer/workload
  purpose/destination/scope/use count
  issued/expires/revoked times, fence/access decision

KeyRing
  key_ring_id, owner/tenant/domain/usage
  custody/KMS/HSM/region, algorithm policy/lifecycle

KeyVersion
  version_id, key ring/custody handle
  algorithm/parameters/usage identifiers
  created/activated/retired/revoked times
  public material/certificate refs, compromise/status

CertificateProfile
  profile_id, issuer/subject/SAN/key usage/assurance rules
  algorithms/validity/renewal/revocation policy/version

CertificateInstance
  certificate_id, profile/key version/workload-or-partner ref
  serial/public artifact/issuer/trust bundle
  not-before/not-after/revocation/status

TrustBundle
  bundle_id, purpose/owner/version
  trusted issuer/certificate refs, constraints
  effective interval/publication/signature/status

CryptoAlgorithmPolicy
  policy_id, use case/data class/jurisdiction
  permitted/deprecated/prohibited algorithms and parameters
  migration deadline/fallback/compatibility/version

CryptoEnvelope
  envelope_id, artifact/data object
  algorithm/key version/nonce/tag/wrapped-key refs
  canonicalization/version/created-at

RotationCampaign
  campaign_id, secret/key/certificate population
  old/new versions, overlap/cutover/deadline
  consumer adoption/non-use verification/rollback/status

ConsumerAdoptionReceipt
  receipt_id, campaign/consumer/new version
  deployed/verified times, use evidence, old-version non-use status

CompromiseCase
  case_id, affected credential/key/cert/workload refs
  detected-at/scope/impact graph/containment/revocation
  rotation/recovery/notifications/verification/status

BYOKBinding
  binding_id, tenant/customer key/external KMS refs
  allowed domains/regions/operations, availability/revocation policy
  verification/rotation/recovery/status
```

## Classification, DLP and egress

```text
ClassificationTaxonomy
  taxonomy_id, version/owner
  labels/compartments/handling/combination rules
  effective interval/publication/status

DataLabel
  label_id, entity/artifact/field/value ref
  classification/compartment/source/authority/confidence
  effective interval/propagation/declassification refs

DerivedLabel
  label_id, source label refs[]/derivation rule
  resulting classification/compartment/purpose restrictions
  version/effective interval

DLPIntent
  dlp_intent_id, actor/workload/business intent
  data manifest/recipient/destination/channel/purpose
  operation/bytes/classifications, requested-at

DLPDecision
  decision_id, DLP intent/policy/scanner versions
  allow/deny/redact/quarantine/review result
  field transformations/restrictions/expiry/invalidators/trace

DestinationTrust
  trust_id, normalized scheme/host/port/path/method
  owner/recipient/region/residency/provider identity
  DNS answers/TTL/pinned resolution, proxy/TLS/trust-bundle refs
  private/link-local/metadata/redirect checks, valid-until/status

EgressPolicy
  policy_id, tenant/data class/purpose/channel/destination scope
  allowed recipients/regions/providers/bytes/actions
  encryption/redaction/approval/exception requirements

EgressDecision
  decision_id, DLP intent/destination trust/policy
  payload/classification/recipient/region checks
  allow/deny/restrict/unknown, required transforms/expiry/trace

EgressInspection
  inspection_id, payload/artifact/decision
  scanner/rule/tool versions, findings/redaction hashes
  malware/secret/PII/content results, status

EgressAttempt
  attempt_id, decision/destination/connection
  exact payload digest/bytes/idempotency
  request/receipt/observation/ambiguity/status

EgressException
  exception_id, policy/decision/requester
  bounded scope/reason/approval/expiry/evidence
  prohibited legal/tenant-boundary bypass proof, status
```

## Trusted time and cryptographic integrity

```text
TimeAuthorityProfile
  profile_id, source/protocol/stratum/NTS-or-equivalent
  trust roots/max offset/uncertainty/holdover policy
  region/cell/effective interval/status

TrustedTimeObservation
  observation_id, host/workload/cell/profile
  wall/monotonic readings, offset/uncertainty/stratum
  synchronization epoch/leap/tzdb versions, observed-at/signature

ClockSkewAssessment
  assessment_id, observation/policy/use case
  allowed/defer/quarantine/fail-closed result
  measured/max skew/uncertainty, incident ref

TimeProof
  proof_id, event/decision/signature/deadline ref
  trusted observations/monotonic anchor/uncertainty
  canonical digest/signature/verification

IntegritySnapshot
  snapshot_id, ledger/shard/cell/event range
  first/last sequence/root hash, algorithm/key version
  WORM binding/signature/trusted time

HashChainVerification
  verification_id, snapshot/range
  sequence/prior-hash/root/signature/key/algorithm checks
  PASS | FAIL | UNKNOWN, tamper/incident/quarantine refs

TamperEvidence
  evidence_id, affected stream/range/artifact
  expected/observed hashes/signatures/sequences
  detection source/time, custody/integrity incident refs
```

## Security detection and investigation

```text
SecuritySignal
  signal_id, actor/session/workload/device/network/tenant/cell refs
  detection rule/model/source/version, observed-at
  category/confidence/severity/baseline deviation
  evidence hashes, status/false-positive state

SuspiciousAccessAssessment
  assessment_id, signal/affected resource/data refs
  expected/observed behavior, risk/impact/unknowns
  investigator/authority, containment/revocation/incident refs

ContainmentAction
  action_id, incident/assessment
  DISABLE_SESSION | REVOKE_CREDENTIAL | QUARANTINE_WORKLOAD |
  BLOCK_EGRESS | FREEZE_ACCOUNT | PRESERVE_EVIDENCE
  target/scope/authority/effective time/observation/repair/status

PrivilegedAccessReview
  campaign_id, population/scope/deadline
  reviewer/authority/SoD policy, item refs/status

PrivilegedAccessReviewItem
  item_id, campaign/grant/session/use refs
  risk/evidence/reviewer snapshot
  certify/revoke/modify/unknown decision, remediation/status
```

## Security invariants

```text
UNKNOWN never becomes ALLOW.
Simulation never emits effects.
Delegation cannot broaden authority, cycle, or survive root revocation.
Every privileged action has bounded scope, trusted time, recording and review.
Inside a network never implies trust; every service request is authenticated and
authorized for tenant, audience, purpose, capability, and destination.
Raw secret/key material is never persisted outside approved custody.
Every sensitive use receives a short-lived, scoped credential lease.
Revocation epochs are monotonic and checked at every gateway.
Missing/stale labels, destination drift, DNS rebinding, scanner failure, or
residency ambiguity fail closed.
Integrity FAIL or UNKNOWN quarantines the affected stream and opens an incident.
```
