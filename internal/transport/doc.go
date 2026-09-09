// Package transport owns protocol adaptation for Human Capital Management Suite and nothing else.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todos: ENDPOINT-002,
// ENDPOINT-003, CAP-003, TOOL-008.
//
// The package holds the transport-neutral half of the edge: the handler ports
// that service implementations delegate to ([IntentHandler],
// [RegistryHandler]), the admission sequence that turns an authenticated
// connection into an immutable [Invocation] ([Admit]), the trusted-field
// enforcement that stops a caller selecting tenant, principal, organization
// scope or purpose ([ApplyTrustedContext]), and strict structural validation
// ([Validate]).
//
// Native gRPC (internal/transport/grpcserver) and the HTTP edge
// (internal/transport/edge) are thin: each adapts its own metadata carrier to
// [Metadata] and calls the same [Admit]. That is not a stylistic preference.
// It is the mechanism by which "equivalent gRPC and HTTP requests resolve the
// same trusted context" becomes a property of the code rather than a pair of
// implementations that happen to agree today.
//
// This package owns no business rule. It does not decide whether a promotion
// is allowed, what a revision means or which approvals are required; it
// decides only that a request is structurally admissible and who the server
// determined the caller to be. Per
// definitions/architecture/package-dependency-policy.yaml it never imports a
// data or ledger port or adapter.
package transport
