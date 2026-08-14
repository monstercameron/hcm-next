# HCM Next Business Data Models

This directory is the exploratory business-entity catalog derived from the
530-entry [Business Intent catalog](../../specs/business-intent-catalog.md), the
[workflow research](../../workflows/README.md), and the
[workflow context contract](../../workflows/_engine/workflow-context-contract.md).

The goal is semantic coverage: every intent must be expressible as reads,
proposals, calculations, state transitions, evidence, effects or analyses over
the entities and value objects defined here. This is not a physical database
schema and does not imply one table per entity.

## Documents

- [Modeling conventions and shared value objects](modeling-conventions.md)
- [Wire contract primitives for Protobuf, Go and SchemaFlux](wire-contract-primitives.md)
- [Intent, workflow, governance and evidence entities](kernel-governance-and-evidence.md)
- [People, identity, employment and organization](people-workforce.md)
- [Rewards, payroll, tax, benefits, time and leave](rewards-payroll-workforce.md)
- [Recruiting, talent, learning, experience and cases](talent-experience-cases.md)
- [Access, documents, communications and integrations](connectivity-access-content.md)
- [Privacy, records, analytics, agents, operations and commercial](assurance-intelligence-platform.md)
- [DataOps, schema, reference and configuration](dataops-configuration.md)
- [Business rules and decisions](rules-and-decisions.md)
- [Security, trust, secrets and egress](security-trust.md)
- [Operations, recovery and production assurance](operations-production.md)
- [Registry and coverage contracts](registry-and-coverage-contracts.md)
- [Cross-domain relationship map](relationship-map.md)
- [530-intent coverage matrix](intent-coverage-matrix.md)
- [Adversarial model audit](adversarial-model-audit-2026-08-14.md)

## Entity classes

```text
Aggregate Root
  consistency and command boundary with stable identity

Revision / Fact
  immutable effective-dated assertion about an aggregate

Relationship
  first-class effective-dated edge between entities

Value Object
  typed value without independent lifecycle

Control Artifact
  versioned policy/schema/decision governing behavior

Evidence Artifact
  immutable proof of assertion, decision, effect or observation

Projection
  reconstructable read model; never authoritative merely because convenient
```

## Coverage rule

An intent is covered only when the catalog identifies:

```text
subjects and aggregate roots
required input/value objects
facts and relationships read
facts and relationships proposed or changed
authority and temporal boundaries
calculations/decisions and result entities
human/legal/document evidence
external effects and observations
reconciliation/repair/outcome entities
```

Naming an entity is insufficient. Its identity, properties, temporal behavior,
source authority, classification, invariants and relationships must be explicit.

The domain catalog currently has conceptual coverage across all 530 numbered
intent partitions. Exact `530/530 VERIFIED` status requires the machine-readable
registries and checker defined in
[Registry and coverage contracts](registry-and-coverage-contracts.md). Until
then, the declared status is `CONCEPTUALLY_COVERED, EXACT_BINDING_PENDING`.

## Go-only boundary

Future executable contracts use Protobuf and generated Go types. SchemaFlux owns
structured-definition validation/compilation, grpcbridge owns HTTP/browser to
gRPC translation, and GWC/GoWebComponents owns the product UI. These planning
documents do not introduce TypeScript models or runtime dependencies.
