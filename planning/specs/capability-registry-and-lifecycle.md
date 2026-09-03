# Capability Registry, Manifest, and Lifecycle Contract

The Capability Registry owns semantic operation identity, manifests, versions,
implementation/transport bindings, publication, activation, adoption, quarantine,
deprecation, and retirement. A route, Go function, workflow node, or agent tool is
not a capability until an active manifest binds it.

## State and Manifest

```text
CapabilityIdentity
CapabilityManifest
CapabilityVersion
ImplementationBinding
TransportBinding
CapabilityOwnership
CapabilityPublication
CapabilityActivationReceipt
CapabilityDeprecation
CapabilityQuarantine
```

```text
DRAFT -> VALIDATED -> REVIEWED -> PUBLISHED -> ACTIVE
       -> DEPRECATED -> RETIRED
Any published/active version may become QUARANTINED.
```

A manifest has a required core and optional extensions. The core is what the
gateway needs to authorize and invoke safely:

```text
core (required from the first endpoint)
  capability id + version
  owner domain
  request / response / error schema refs
  side-effect profile
  read and write data domains and field sets
  risk class
  idempotency policy
  agent eligibility (default: not eligible)

extensions (added when a consumer needs them)
  consistency/freshness, obligations, execution modes, bulk/population/cost
  limits, SLO class, implementation build/provenance, transport bindings,
  dependencies, retention/evidence policy, conformance results
```

An extension is added to the manifest schema only when a real consumer reads
it. Nothing may require an extension before that consumer exists.

## Bootstrap Profile

The registry must never be the reason the first endpoint cannot ship. Two
profiles exist:

```text
BOOTSTRAP (P1A and P1B)
  the registry is a compiled-in Go table generated from Protobuf service
  annotations at build time; the build is the publication; the binary's
  digest is the snapshot fingerprint; no separate publish/activate flow,
  no signed control bundle, no distribution

MANAGED (Gate C and later)
  the full propose / validate / review / publish / activate lifecycle,
  signed Control Bundle distribution, quarantine SLA, and adoption tracking
```

Under `BOOTSTRAP`, the workflow compiler, gateway, UI action binder, and agent
gateway resolve the same compiled table, so the single-source rule still holds.
The lifecycle states below describe the `MANAGED` profile.

## APIs and Runtime Resolution

```text
capabilities.propose|validate|review|publish|activate
capabilities.quarantine|deprecate|retire
capabilities.read|search|resolve|explain|adoption
capabilities.snapshots.status
```

Capability ID/version is globally unique within platform namespace; customer
extensions use tenant-owned namespaces. Duplicate/conflicting identities fail
publication. Schema, domain owner, implementation build, transport, policy
requirements, and tests must resolve before validation. Manifest and dependency
digest are signed and distributed in the Control Bundle.

Workflow compiler, grpcbridge edge, agent tool gateway, UI action binder, SDK
generator, billing meter, and runtime resolve the same signed locally applied
snapshot. Unknown, inactive, quarantined, retired, incompatible, or unadopted
versions fail closed. Deprecation does not stop existing pinned workflows until
their declared support window; retirement requires dependency/adoption proof or
an approved migration.

## Security, Failure, and Evidence

Domain owner proposes semantic/effect metadata; schema/security/privacy/operations
review their owned fields; publisher and runtime activation executor are separate.
No implementation may self-assert lower risk, narrower writes, safer side effects,
or agent eligibility. Quarantine/kill propagation has a measured SLA, prevents new
invocations, and handles in-flight work by declared safe-point policy.

Evidence records manifest/digest/signature, ownership, validations/reviews,
schemas/dependencies/build/transports, publication/activation receipts, runtime
snapshot, consumers/adoption, invocations by version, quarantine, deprecation,
migration and retirement. P1A and P1B run the `BOOTSTRAP` profile with the
core manifest fields; the `MANAGED` profile, signed bundles, and quarantine
tests are Gate C.
