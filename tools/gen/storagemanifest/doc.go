// Package storagemanifest implements DB-002, DB-003 and DB-004: it reads the
// compiled internal/intent/model registry, cross-checks it against the real
// physical schema in migrations/*.sql, and generates three deterministic
// manifests into definitions/model/:
//
//   - storage-disposition.yaml (DB-002): one physical disposition, owner and
//     schema target per registered entity/property/relationship/authority
//     item.
//   - property-sql-mappings.yaml (DB-003): one SQL type/constraint mapping
//     per registered property.
//   - relationship-lifecycle-constraints.yaml (DB-004): generated SQL
//     constraint fragments per relationship definition and aggregate
//     lifecycle, annotated with whether the real migrations already declare
//     the equivalent constraint.
//
// Generation is a pure function of the compiled registry and the migration
// source text: given the same inputs, [BuildDispositionManifest],
// [BuildPropertyMappings] and [BuildConstraints] always produce byte-identical
// output, which is what makes two `go run`/test runs byte-identical.
//
// This package never edits migrations/*.sql. A registered item with no
// physical counterpart in the real schema is reported as a MISMATCH
// disposition, not silently invented.
package storagemanifest
