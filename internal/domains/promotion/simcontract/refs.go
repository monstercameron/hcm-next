package simcontract

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// schemaVersion pins the canonical encoding this package digests under. It is
// bumped only when the encoding changes, never when a field's value changes.
const schemaVersion = 1

// IntentRef binds a contract to one published intent identity. It never
// carries the intent's mutable lifecycle state: a contract is a point-in-time
// artifact, and re-deriving "what is true about the intent now" from it would
// let a stale contract masquerade as current.
type IntentRef struct {
	IntentID      string
	IntentType    string
	IntentVersion string
}

// Validate rejects an incompletely named intent reference.
func (r IntentRef) Validate() error {
	for _, req := range []struct{ field, value string }{
		{"intent_id", r.IntentID},
		{"intent_type", r.IntentType},
		{"intent_version", r.IntentVersion},
	} {
		if req.value == "" {
			return fmt.Errorf("%w: intent ref has no %s", ErrInvalidInput, req.field)
		}
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r IntentRef) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simcontract.IntentRef", schemaVersion).
		String("intent_id", r.IntentID).
		String("intent_type", r.IntentType).
		String("intent_version", r.IntentVersion).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// SnapshotRef binds a contract to the exact governed input snapshot (for
// example a PROMO-001 PromotionInputSnapshot) it was computed from.
type SnapshotRef struct {
	SnapshotDigest string
	Tenant         values.TenantId
	Subject        values.EntityRef
}

// Validate rejects a snapshot reference missing its digest or identity.
func (r SnapshotRef) Validate() error {
	if r.SnapshotDigest == "" {
		return fmt.Errorf("%w: snapshot ref carries no digest", ErrInvalidInput)
	}
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: snapshot ref tenant: %w", ErrInvalidInput, err)
	}
	if err := r.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: snapshot ref subject: %w", ErrInvalidInput, err)
	}
	if r.Subject.Tenant != r.Tenant {
		return fmt.Errorf("%w: snapshot ref subject %s is outside tenant %s", ErrInvalidInput, r.Subject, r.Tenant)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r SnapshotRef) Canonical() []byte {
	if err := r.Validate(); err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.promotion.simcontract.SnapshotRef", schemaVersion).
		String("snapshot_digest", r.SnapshotDigest).
		String("tenant", string(r.Tenant)).
		Value("subject", r.Subject).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}
