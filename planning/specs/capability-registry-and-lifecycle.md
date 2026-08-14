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

A manifest binds stable capability ID/name, owner/domain, version, request/
response/error schemas, read/write/data-domain/field sets, side-effect profile,
risk, idempotency/canonicalization, consistency/freshness, AuthZ/legal/privacy/
entitlement/risk inputs, obligations, execution modes, agent eligibility,
bulk/population/cost limits, SLO, implementation build/provenance, transport
bindings, dependencies, retention/evidence, and conformance results.

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
migration and retirement. Gate A implements every read/query/simulate capability;
Gate B adds write/workflow/repair operations and quarantine tests.
