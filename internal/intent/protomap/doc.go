// Package protomap maps kernel value types to and from the generated Protobuf
// contracts, and supplies the digest port the intent kernel depends on.
//
// Semantic owner: intent-and-capability. Phase: P1A.
//
// The kernel packages stay free of wire types so that a schema change cannot
// reach into business logic. Everything that knows about
// hcmnext.common.v1 and hcmnext.intents.v1 lives here.
//
// Two rules govern every conversion:
//
//  1. Round-trip fidelity. Converting to Protobuf and back returns a value
//     equal to the original, including the distinctions the wire format makes
//     easy to lose: an entity id's kind, a reference's tenant, a resource key's
//     ordered segments, an unspecified revision token versus sequence zero, and
//     an absent optional versus an empty one.
//  2. Fail closed. A decode never invents a value. A tenantless EntityRef, a
//     reserved kernel-family enum number, an unknown enum and a malformed
//     canonical text are all errors rather than zero values.
package protomap
