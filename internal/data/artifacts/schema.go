package artifacts

// SchemaSuffix names the companion object-store schema migrations/00010
// creates alongside whatever schema the migration tree itself is applied
// into. See that migration's header comment for why the artifact tables live
// in a schema of their own rather than a table in the core schema.
const SchemaSuffix = "_artifact_store"

// Schema returns the artifact-store schema name for a given core schema
// (the schema the rest of the migration tree -- tenant, ledger_event, outbox
// and so on -- was applied into). Every function in this package that talks
// to PostgreSQL takes that computed name explicitly, the same way
// internal/data/tenancy.WithTenant takes a tenant id explicitly: nothing in
// this package infers it from a connection's search_path.
func Schema(coreSchema string) string {
	return coreSchema + SchemaSuffix
}
