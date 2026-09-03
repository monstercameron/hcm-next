// Package integration owns the DB-014 relational contract for integration,
// messaging, document, and artifact metadata.  The physical migration is
// intentionally staged separately from the platform schema registry; this
// package makes the contract executable without coupling it to that registry.
package integration

import (
	"fmt"
	"slices"
)

// Table describes the metadata that every DB-014 table must materialize.
// Columns are semantic names, rather than SQL types, so this contract remains
// useful to migration and schema-review tooling without duplicating SQL.
type Table struct {
	Name       string
	Columns    []string
	Lifecycle  string
	TenantKey  bool
	AppendOnly bool
}

// Tables is the complete DB-014 contract. Payloads and secrets are references
// to governed stores; no table is permitted to carry their raw bytes.
var Tables = []Table{
	{Name: "connector_definition", Columns: []string{"connector_id", "tenant_id", "connector_version", "vendor", "status", "definition_digest", "classification", "created_at"}, Lifecycle: "REVISIONED_REFERENCE", TenantKey: true},
	{Name: "connector_connection", Columns: []string{"connection_id", "tenant_id", "organization_scope", "connector_id", "connector_version", "environment", "credentials_ref", "status", "mapping_version", "correlation_id", "idempotency_key", "created_at", "updated_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "connector_health", Columns: []string{"health_id", "tenant_id", "connection_id", "status", "observed_at", "freshness", "error_class", "latency_ms", "queue_depth", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "integration_receipt", Columns: []string{"receipt_id", "tenant_id", "connection_id", "provider_receipt_id", "payload_artifact_ref", "raw_hash", "schema_version", "classification", "correlation_id", "received_at", "status", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "mapping_profile", Columns: []string{"mapping_id", "tenant_id", "version", "source_schema_ref", "destination_schema_ref", "mapping_digest", "classification", "effective_from", "effective_to", "status", "created_at"}, Lifecycle: "REVISIONED_REFERENCE", TenantKey: true},
	{Name: "sync_job", Columns: []string{"sync_id", "tenant_id", "connection_id", "object_type", "direction", "mode", "mapping_version", "cursor", "source_watermark", "correlation_id", "status", "started_at", "completed_at", "created_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "event_subscription", Columns: []string{"subscription_id", "tenant_id", "connection_id", "event_type", "schema_version", "destination_ref", "cursor", "generation", "expires_at", "status", "created_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "webhook_receipt", Columns: []string{"receipt_id", "tenant_id", "connection_id", "provider_event_id", "payload_artifact_ref", "payload_hash", "signature_status", "replay_key", "schema_version", "correlation_id", "received_at", "status", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "external_conflict", Columns: []string{"conflict_id", "tenant_id", "canonical_resource_ref", "field_path", "effective_from", "effective_to", "expected_version", "observed_version", "conflict_class", "status", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "connector_operation", Columns: []string{"operation_id", "tenant_id", "connection_id", "semantic_operation", "resource_key", "correlation_id", "idempotency_key", "mapping_version", "canonical_input_hash", "mapped_payload_hash", "expected_external_version", "deadline", "status", "accepted_at", "observed_at", "reconciliation_status", "created_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "artifact_object", Columns: []string{"artifact_id", "tenant_id", "object_store_ref", "media_type", "byte_size", "canonical_digest", "classification", "encryption_key_ref", "residency_ref", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "document", Columns: []string{"document_id", "tenant_id", "document_type", "owner_ref", "subject_ref", "current_version_ref", "classification", "retention_ref", "hold_ref", "status", "created_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "message_intent", Columns: []string{"intent_id", "tenant_id", "purpose", "audience_ref", "template_ref", "content_artifact_ref", "correlation_id", "idempotency_key", "deadline", "status", "created_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "delivery_attempt", Columns: []string{"attempt_id", "tenant_id", "intent_id", "provider_ref", "provider_request_id", "payload_hash", "idempotency_key", "correlation_id", "status", "accepted_at", "observed_at", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "message_receipt", Columns: []string{"receipt_id", "tenant_id", "attempt_id", "provider_receipt_id", "response_hash", "observed_at", "freshness", "status", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
	{Name: "document_signature", Columns: []string{"signature_id", "tenant_id", "document_id", "signer_ref", "provider_ref", "request_id", "signature_artifact_ref", "status", "deadline", "created_at"}, Lifecycle: "REVISIONED_FACT", TenantKey: true},
	{Name: "artifact_reference", Columns: []string{"reference_id", "tenant_id", "artifact_id", "owner_kind", "owner_ref", "purpose", "classification", "created_at"}, Lifecycle: "IMMUTABLE_EVIDENCE", TenantKey: true, AppendOnly: true},
}

var requiredLifecycle = map[string]bool{"REVISIONED_REFERENCE": true, "REVISIONED_FACT": true, "IMMUTABLE_EVIDENCE": true}

// Validate checks the DB-014 contract for structural omissions that otherwise
// tend to become security or reconciliation bugs in a migration.
func Validate(tables []Table) error {
	seen := make(map[string]bool, len(tables))
	for _, table := range tables {
		if table.Name == "" || seen[table.Name] {
			return fmt.Errorf("integration schema: duplicate or empty table %q", table.Name)
		}
		seen[table.Name] = true
		if !table.TenantKey {
			return fmt.Errorf("integration schema: %s is not tenant scoped", table.Name)
		}
		if !requiredLifecycle[table.Lifecycle] {
			return fmt.Errorf("integration schema: %s has unknown lifecycle %q", table.Name, table.Lifecycle)
		}
		for _, forbidden := range []string{"secret", "credential", "raw_payload", "payload", "content"} {
			if slices.Contains(table.Columns, forbidden) {
				return fmt.Errorf("integration schema: %s stores raw sensitive field %q", table.Name, forbidden)
			}
		}
		if !slices.Contains(table.Columns, "tenant_id") {
			return fmt.Errorf("integration schema: %s omits tenant_id", table.Name)
		}
	}
	return nil
}
