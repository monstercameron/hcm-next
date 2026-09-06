package auditpack

import (
	"encoding/hex"
	"fmt"
)

// TenantID and CorrelationID are this package's own spelling of a 128-bit
// identifier. Both are genuine type aliases for the unnamed array type
// [16]byte - not defined types - which is deliberate: a value of
// github.com/google/uuid.UUID (itself defined as [16]byte, the type
// internal/data/ledger, internal/data/ledger/checkpoint and
// internal/data/ledger/evidence all use for a tenant or correlation
// identifier) assigns into and out of a TenantID/CorrelationID field or
// parameter with no explicit conversion, because Go's assignability rule
// only requires that one of the two sides be an unnamed type when their
// underlying types are identical.
//
// This package lives under internal/domains, which
// definitions/architecture/dependency-roles.yaml does not list among
// github.com/google/uuid's allowed_import_roots - unlike internal/data,
// internal/ledger and internal/transaction, which is where the ledger,
// checkpoint and evidence packages this package wraps actually live and are
// permitted to depend on that module directly (LIB-002's third-party
// semantic firewall). Aliasing the module's exact wire shape here, rather
// than importing it, is what lets every caller already holding a
// google/uuid value hand it straight to this package's API while this
// package itself never imports that module.
type (
	TenantID      = [16]byte
	CorrelationID = [16]byte
)

// formatUUID renders a 16-byte identifier in the canonical RFC 4122
// 8-4-4-4-12 hex text form - the same one google/uuid.UUID.String()
// produces - without this package importing that module to get it.
func formatUUID(id [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], id[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], id[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], id[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], id[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], id[10:16])
	return string(buf[:])
}

// parseUUID is formatUUID's inverse: it accepts exactly the canonical
// 8-4-4-4-12 hex form and refuses anything else, including the shorter or
// braced spellings google/uuid.Parse itself tolerates - this package only
// ever has to read back text it wrote itself with formatUUID.
func parseUUID(s string) ([16]byte, error) {
	var id [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return id, fmt.Errorf("auditpack: %q is not a canonical uuid", s)
	}
	hexDigits := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36]
	decoded, err := hex.DecodeString(hexDigits)
	if err != nil {
		return id, fmt.Errorf("auditpack: %q is not a canonical uuid: %w", s, err)
	}
	if len(decoded) != len(id) {
		return id, fmt.Errorf("auditpack: %q decodes to %d bytes, want %d", s, len(decoded), len(id))
	}
	copy(id[:], decoded)
	return id, nil
}
