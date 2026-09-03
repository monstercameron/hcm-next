// Package admin is the SVC-011/ADMIN-001 gRPC adapter: it implements
// hcmnext.admin.v1.AdminService (gen/go/hcmnext/admin/v1) over
// [Dependencies], converting between the wire shape and the plain Go types
// internal/operations/admin and the domain packages
// (internal/domains/people, internal/domains/intelligence) define, and
// exposes [Register] as the hook that adds the service to an
// already-constructed *grpc.Server.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todos: SVC-011,
// ADMIN-001.
//
// This package (and its internal/transport/admin/hcmctl CLI subpackage) is
// where every gRPC- and Protobuf-touching line of the admin surface lives,
// because internal/transport is one of the few roots
// definitions/architecture/dependency-roles.yaml's library firewall admits
// for importing google.golang.org/grpc and google.golang.org/protobuf
// directly (LIB-002/LIB-003; see tools/policy/libfirewall). The policy
// decisions themselves - [admin.OperatorRole], the operator route-policy
// gate, field/section validation, the authorization-decision constructors -
// live in internal/operations/admin, which imports neither library, so
// those decisions stay testable without a network round trip and reusable
// by whatever other transport (grpcbridge, an HTTP edge) is added later.
package admin
