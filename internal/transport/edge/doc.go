// Package edge is the HTTP projection of the canonical Human Capital Management Suite gRPC surface.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todos: TOOL-008,
// ENDPOINT-002, ENDPOINT-003, CAP-003.
//
// # Edge selection (TOOL-008)
//
// The transport-edge qualification fixture is TestGRPCBridgeUnaryParity in
// this package: it stands the canonical gRPC service up in process on a
// loopback listener, runs one Protobuf vector through native gRPC and through
// the edge, and requires identical domain results, typed errors, evidence
// identifiers, server-derived principal, authorization results, deadline and
// cancellation behavior, plus proof that unauthenticated metadata cannot
// select trusted context.
//
// The selected edge is connect-go (connectrpc.com/connect v1.20.0). The
// evidence for the three candidates:
//
//   - grpcbridge (github.com/monstercameron/GoGRPCBridge v1.1.2,
//     pkg/grpctunnel) is REJECTED for this fixture. Its package documentation
//     and its whole API surface are gRPC over WebSocket: BuildBridgeHandler,
//     HandleBridgeMux, Wrap, Dial and DialContext all establish a WebSocket
//     session and tunnel gRPC frames through it. It publishes no unary
//     HTTP/JSON projection of a gRPC method, and TOOL-008 explicitly excludes
//     WebSocket and SSE from this fixture, so there is no HTTP invocation for
//     it to be compared against native gRPC. Independently, importing it also
//     requires adding eight module requirements to go.mod (gorilla/websocket,
//     go.opentelemetry.io/otel and its metric/trace/auto-sdk siblings,
//     cespare/xxhash, go-logr/logr and go-logr/stdr), which is a dependency
//     decision, not a transport one. grpcbridge remains the candidate for
//     TOOL-009's streaming conformance work, where WebSocket is the point.
//   - grpc-gateway v2 (github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0) is
//     technically capable and is the only candidate that also produces the
//     RESTful path projection the endpoint contract's inventory describes
//     (POST /v1/intents and friends). It is NOT selected now because importing
//     its runtime package requires adding google.golang.org/genproto/
//     googleapis/api to go.mod, and because its route projection is generated
//     from the manifest that ENDPOINT-001 has yet to produce. It is the
//     natural successor once both are true.
//   - connect-go v1.20.0 is SELECTED. It builds against the pinned module
//     graph with no new requirement, serves the same Protobuf messages over
//     HTTP/1.1 and HTTP/2 with both binary and JSON bodies, propagates
//     deadlines and cancellation, and carries typed error details, which is
//     what makes identical error projection provable rather than asserted.
//
// One connect-go behavior had to be overridden rather than adopted: its
// default status mapping projects FAILED_PRECONDITION to HTTP 400, while the
// canonical error projection table requires 412. The edge therefore projects
// HTTP status from envelope.Code.HTTPStatus and rewrites the response status
// accordingly; see statusOverrideMiddleware. That is the only place this
// package disagrees with its transport library, and it disagrees in favor of
// the Human Capital Management Suite contract.
//
// # What this package is not
//
// The edge adapts protocol and owns no business rule. It does not authenticate
// (trust does), it does not construct trusted context (transport.Admit does),
// it does not decide owned error codes (envelope does) and it does not decide
// anything about an HCM operation (the handler ports do). Its own additions
// are exactly two: the strict JSON admission screen, because JSON is a laxer
// encoding than Protobuf and the contract requires unknown fields, duplicate
// keys, invalid UTF-8 and ambiguous numbers to be rejected rather than
// silently absorbed; and the HTTP status projection above.
package edge
