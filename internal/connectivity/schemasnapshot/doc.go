// Package schemasnapshot ingests an externally discovered schema capture into
// quarantine, runs bounded validators against it, and only then admits or
// rejects it -- never as a side effect of receiving bytes (owner:
// connectivity; phase: P1A; INTG-004).
//
// # What a schema snapshot is, and is not
//
// planning/specs/integration-platform.md's own discovery pipeline is
// "discover/import -> normalize schema snapshot -> registry", and its
// coverage table marks schema snapshots a MINIMAL CONTRACT: this package is
// the "normalize schema snapshot" step and nothing past it. A [SchemaSnapshot]
// is never a canonical schema release, never a diff input and never a
// [Validator] the mapping compiler may consume while it is anything other
// than [StateAdmitted] -- [SchemaSnapshot.RequireAdmitted] is the one call a
// consumer needs to enforce that, and [Ingest] never returns a snapshot in
// [StateAdmitted] without having produced the [Evidence] that says why.
//
// # The quarantine gate is structural
//
// [Ingest] always performs the same four steps in the same order, and there
// is no shorter path to [StateAdmitted]: store the raw bytes as a
// content-addressed artifact, record a [StateQuarantined] row under the
// identity those bytes and their provider derive
// ([Identity.SnapshotID]), run every configured [Validator] against the
// declared format, and only then transition to [StateAdmitted] or
// [StateRejected] -- with an [Evidence] record written in the same step,
// either way. A snapshot that never reaches a verdict stays quarantined
// forever; nothing here has a "publish anyway" method.
//
// # Idempotent re-ingest, not silent overwrite
//
// [Identity.SnapshotID] is deterministic over (tenant, provider, canonical
// digest), so ingesting byte-identical content again for the same provider
// resolves to the same row: an already-decided snapshot is returned
// unchanged rather than re-validated, and a still-quarantined one resumes
// validation rather than being duplicated. Different identity metadata
// arriving under the same digest -- a caller error, not a legitimate
// re-ingest -- is [ErrImmutable].
//
// # Dependency direction
//
// This package declares [Store] and [ArtifactStore] as the two ports
// [Ingest] needs; [MemoryStore] and [MemoryArtifactStore] satisfy them for
// tests that need no database. The PostgreSQL adapter in ./adapters/postgres
// implements the same two ports over migrations/00025_schema_snapshot.sql and
// internal/data/artifacts, so business logic here never imports a driver.
package schemasnapshot
