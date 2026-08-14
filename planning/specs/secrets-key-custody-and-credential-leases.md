# Secrets, Key Custody, and Credential Lease Contract

The Secrets and Key Custody Service owns secret metadata/versioning, access
policy, credential leases, rotation, revocation, compromise, and recovery. It
shares evidence conventions with PKI/BYOK but does not conflate values, keys,
certificates, OAuth grants, or customer-controlled custody.

## Canonical State

```text
SecretReference
SecretVersion
SecretAccessPolicy
CredentialLease
RotationCampaign
Revocation
CompromiseCase
RecoveryBinding
```

```text
Secret: REQUESTED -> ACTIVE -> ROTATING -> ACTIVE_NEW -> DISABLED -> DESTROYED
Lease:  REQUESTED -> ISSUED -> ACTIVE -> EXPIRED | REVOKED
Compromise: SUSPECTED -> CONTAINED -> ROTATED/REVOKED -> VERIFIED -> CLOSED
```

Kinds are `OPAQUE_SECRET`, `DATABASE_CREDENTIAL`, `API_CREDENTIAL`,
`OAUTH_CLIENT_SECRET`, `OAUTH_GRANT`, `SYMMETRIC_KEY`, `ASYMMETRIC_KEY_HANDLE`,
`SIGNING_KEY_HANDLE`, `CERTIFICATE_KEY_HANDLE`, and `EXTERNAL_KMS_REFERENCE`.
Raw cryptographic keys remain in the eligible KMS/HSM where required.

## APIs and Access

```text
secrets.create|import|version|disable|destroy|metadata
credential_leases.request|renew|revoke|status
secrets.rotate|rotation.status
secrets.revoke|compromise.declare|contain|resolve
secrets.recovery.bind|verify
secrets.access.attest_non_use
```

Secret-zero bootstrap uses separately provisioned workload identity and trusted
bootstrap roots; no universal plaintext bootstrap secret exists. Workloads request
short-lived leases bound to workload, tenant/cell/region, purpose, secret version,
operation, destination, and maximum duration. Values are never returned to UI,
workflow definitions, agents, logs, traces, config bundles, database rows, crash
dumps, or evidence packages. Go components minimize lifetime and prevent copies
where practical; subprocess/environment/file exposure is prohibited unless a
specific adapter profile requires and cleans it.

Rotation supports declared overlap, consumer adoption receipts, new-version
activation, old-version rejection time, external-provider update, rollback before
revocation, and verification of non-use. Provider outage follows per-secret
profile: bounded cached lease, queue, degrade, or fail closed; no ambient fallback.

## Security, Failure, and Evidence

Create/import, policy grant, value use, rotate, revoke/destroy, recovery, and
audit are separate capabilities. High-impact keys/secrets use dual control,
hardware-backed custody, step-up, and tenant/region restrictions. Support cannot
retrieve raw values. Recovery authority is separated from production-use authority.

Typed failures include unknown/disabled/destroyed version, identity/purpose/scope
denial, lease expiry, wrong region/destination, stale consumer, KMS/provider
unavailable, rotation incomplete, revocation uncertainty, and compromise. A
compromise graph identifies every workload, connector, artifact, signature,
tenant, and release affected.

Evidence stores references and metadata only: actor/workload, purpose/scope,
version, lease issuance/use counters/destination, rotation/adoption/revocation,
policy decision, provider/KMS receipt, compromise/containment/recovery, and
non-use verification. Secret material is expressly excluded.

Gate A covers connector read credentials, database, workload bootstrap, config
signing and artifact encryption. Gate B adds write connector credentials, ledger/
proposal signing handles, rotation overlap, revocation propagation, and leak drill.
