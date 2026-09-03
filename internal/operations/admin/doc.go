// Package admin holds the pure, transport-independent half of SVC-011/
// ADMIN-001's governed operator surface: AdminService's distinct trust/route
// policy ([OperatorRole], [RequireOperator]) and the small amount of
// projection-shaping logic its two governed passthroughs share
// ([ParseFields], [ParseSections], [OperatorWorkerAuthorization],
// [OperatorTransactionAuthorization]).
//
// Semantic owner: operations-and-assurance. Phase: P1A. Todos: SVC-011,
// ADMIN-001.
//
// This package imports no gRPC or Protobuf library directly - it operates
// on plain internal/trust and internal/domains types only
// (definitions/architecture/dependency-roles.yaml's grpc/protobuf library-
// firewall rows admit only internal/transport, gen, tools/gen and cmd as
// import roots for those two libraries; a capability package outside that
// list must not touch them). The actual gRPC service - the
// hcmnext.admin.v1.AdminService implementation, its Register hook, and the
// wire-shape conversions - lives in internal/transport/admin, which imports
// this package for the policy decisions documented here and calls the
// governed domain functions (internal/domains/people.ExplainWorkerState,
// internal/domains/intelligence.ExplainTransaction) directly. The CLI lives
// at internal/transport/admin/hcmctl for the identical reason: it dials
// gRPC.
//
// # What "distinct trust/route policy" means here
//
// [RequireOperator] is the whole of it: a caller's authenticated
// trust.Principal must carry [OperatorRole], a role an ordinary user or
// first-party service credential is never issued. Everything else about how
// a request reaches a handler - authentication, deadline capping, strict
// validation, the canonical envelope.Error projection - is the identical
// shared internal/transport/grpcserver.UnaryInterceptor chain every other
// service on the process uses; this package adds nothing to that chain and
// invents no parallel one.
//
// # What this package is not
//
// AdminService has no method that creates, submits, cancels, supersedes,
// updates or deletes anything, and neither does anything in this package:
// every exported function here is either a pure read/validate helper
// (ParseFields, ParseSections) or a policy predicate (RequireOperator) or an
// authorization-decision constructor (OperatorWorkerAuthorization,
// OperatorTransactionAuthorization). An operator who needs to change
// workforce data uses the ordinary governed BusinessIntent lifecycle
// (hcmnext.intents.v1.IntentService) like every other caller, under the
// same authorization decisions - never a shortcut through this package.
package admin
