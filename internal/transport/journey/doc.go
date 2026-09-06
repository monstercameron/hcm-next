// Package journey is the gRPC adapter for the Promotion journey engine: it
// implements hcmnext.journey.v1.JourneyService (gen/go/hcmnext/journey/v1)
// over a single [Dependencies] port, converting between the wire shape and
// the plain Go types internal/humanwork/workspace's JourneyEngine port
// defines, and exposes [Register] as the hook that adds the service to an
// already-constructed *grpc.Server.
//
// Semantic owner: experience-and-transport. Phase: P1A.
//
// The service exists so the browser-resident GoWebComponents/WASM journey
// page can reach the same engine the workspace surface reads and acts
// through, over the gRPC-over-WebSocket tunnel, without a second HTTP/JSON
// business surface being invented for one page. Protobuf/gRPC is the
// canonical contract in this repository, and this package is where every
// gRPC- and Protobuf-touching line of the journey surface lives, because
// internal/transport is one of the few roots
// definitions/architecture/dependency-roles.yaml's library firewall admits
// for importing google.golang.org/grpc and google.golang.org/protobuf
// directly (LIB-002/LIB-003; see tools/policy/libfirewall).
//
// Nothing here decides an HCM business rule. Every method is a thin forward
// to workspace.JourneyEngine, whose implementation (internal/intent/app)
// owns the intent service, the execution driver adapter and the database.
// This package's whole job is three mechanical translations: port types to
// and from wire messages ([convert.go]), the port's sentinel refusals to the
// repository's owned error model ([envelope.Error], projected to a gRPC
// status exactly as internal/transport/admin projects it), and the
// change-detection digest WatchJourney's stream emits on ([watch.go]).
//
// [watch.go] carries one further piece of transport mechanics that is not a
// translation: the resumable position of WatchJourney's stream. Each message
// it sends is numbered and carries an opaque, tenant-bound, expiring cursor
// minted by internal/transport/streaming, and a cursor presented back on a
// reconnect is validated before the stream opens, so a client that lost its
// socket can prove it missed nothing (PROTO-007). That machinery lives in
// internal/transport/streaming rather than here; this package supplies the
// tenant (from the admitted invocation, never from the request) and the
// stream identity, and decides which refusal each rejection projects to.
//
// Admission is not this package's job either. [Register] installs no
// interceptor: srv must already carry the shared trusted-request chain,
// which caps the deadline, screens caller-selected trusted context,
// authenticates and constructs the immutable invocation before any handler
// here runs.
//
// That chain covers both cardinalities. internal/transport/grpcserver
// installs grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor) and
// grpc.ChainStreamInterceptor(grpcserver.StreamInterceptor), and the two
// share one implementation of the boundary, so WatchJourney is a real
// server-streaming RPC whose handler sees the admitted principal and
// invocation exactly as the five unary methods do. Every method in this
// package therefore reads its trusted context the same way, from the
// context, and none of them reads transport metadata.
package journey
