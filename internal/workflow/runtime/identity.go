package runtime

import "github.com/google/uuid"

// ParseUUID parses s as one of this package's identity values: a tenant id
// or an instance id share the exact same shape (a canonical UUID), and every
// [Store] method that takes one wants this exact type.
//
// It exists so a caller outside the roots
// definitions/architecture/dependency-roles.yaml allows to import
// "github.com/google/uuid" directly -- internal/transport among them, per
// LIB-002/LIB-004 -- can still obtain the value [Store]'s Load/Record
// methods require, entirely through type inference at the call site: the
// returned value's type is never spelled in the caller's own source, so the
// caller's own file never needs the import.
func ParseUUID(s string) (uuid.UUID, error) { return uuid.Parse(s) }
