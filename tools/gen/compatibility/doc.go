// Package compatibility checks versioned Protobuf contracts without a network
// service or a Buf registry. It compares descriptor sets supplied by a caller,
// so the same policy applies to domain, event, file, and connector contracts.
//
// A Contract carries its schema version and the caller-owned material digest.
// The digest represents semantics that descriptors cannot express (for
// example, which fields participate in a signed event); changing it requires
// a strictly newer contract version. RequiredPaths supplies the corresponding
// required-field semantics for proto3, whose wire descriptors otherwise have
// no required label.
package compatibility
