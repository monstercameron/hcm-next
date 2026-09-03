// Package clientsgen is the TOOL-007 generator: it reads the compiled
// Protobuf descriptors (via blank imports of gen/go/hcmnext/*) and
// definitions/api/endpoint-manifest.json, and emits the typed Go capability
// clients committed under internal/transport/clients.
//
// Semantic owner: experience-and-transport. Phase: P1A. Todo: TOOL-007.
//
// The generator is deliberately descriptor-driven rather than hand-written
// per service: the endpoint manifest is the one list of "which services and
// methods are public", and the Protobuf descriptor registry is the one
// source of "what Go package and type each message/service compiles to" (it
// reads each file's go_package option rather than assuming a naming
// convention), so adding a thirteenth or fourteenth method never requires a
// second hand-maintained client to fall out of sync with the wire contract.
//
// Generation is pure and deterministic: [Render] takes no input but the
// checked-in manifest and the descriptors already linked into this binary,
// and produces the same bytes on every call. [TestGeneratedClientsCurrent]
// in this package proves that a second run reproduces the committed tree
// byte for byte, the same guarantee TOOL-010 makes for the buf-generated
// gen/go tree.
//
// Regenerate with:
//
//	go run ./tools/gen/clients/cmd/generateclients
package clientsgen
