// Package integrationmeta is the tenant-scoped store for the integration and
// external-system metadata migration 00031 creates (DB-014).
//
// It owns twelve tables: the external system and connector registries, the
// tenant's connections and mapping profiles, the immutable inbound receipt
// and receipt-item evidence, mapping executions, the outbound effect graph
// (connector_operation and its attempts), external observations, and the
// reconciliation job/result pair.
//
// Three rules this package refuses to let a caller break, each of which the
// schema also enforces so a raw SQL path cannot route around it:
//
//   - A credential is a reference into a governed secret store, never a
//     secret. [ConnectorConnection.Validate] rejects a credential_ref that
//     is not one, and rejects a configuration object carrying a
//     secret-bearing key.
//   - Provider acceptance is not business completion. [CompleteOperation] is
//     the only way to reach completion_state COMPLETE, and it requires the
//     observation that confirmed the external state.
//   - Nothing loses its correlation, idempotency key, version or deadline:
//     the Validate methods reject a zero value for each.
//
// Content-addressed bytes stay in the governed object store; every payload
// column here is a digest.
package integrationmeta
