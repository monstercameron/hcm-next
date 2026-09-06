// Package attestationstore persists attestation revisions, binding evidence,
// approval requirements and approval resolution evidence. Every operation
// opens a transaction and scopes it with internal/data/tenancy.WithTenant.
package attestationstore
