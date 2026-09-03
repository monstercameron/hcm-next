// Package schemafluxsql turns the SchemaFlux model registry into reviewed SQL
// disposition and migration-input records. It is deliberately pure: callers
// may render or review the returned SQL, but this package never writes a
// migration or changes a generated artifact.
package schemafluxsql
