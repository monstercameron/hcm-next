package snapshot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ErrRequirementIncomplete is returned when a ConsistencyRequirement (or one
// of its watermark floors) is malformed. Matchable with errors.Is.
var ErrRequirementIncomplete = errors.New("snapshot: consistency requirement is malformed")

// InputWatermarkFloor is the minimum watermark one named input's resolved
// entry must meet or exceed within its own revision stream.
type InputWatermarkFloor struct {
	// InputName names the InputEntry this floor applies to.
	InputName string
	// Minimum is the lowest acceptable watermark, in InputName's own stream.
	Minimum values.RevisionToken
}

// Validate reports whether the floor is well formed.
func (f InputWatermarkFloor) Validate() error {
	if strings.TrimSpace(f.InputName) == "" {
		return fmt.Errorf("%w: watermark floor has no input name", ErrRequirementIncomplete)
	}
	if err := f.Minimum.Validate(); err != nil {
		return fmt.Errorf("%w: watermark floor %q: %w", ErrRequirementIncomplete, f.InputName, err)
	}
	if !f.Minimum.IsSpecified() {
		return fmt.Errorf("%w: watermark floor %q has no minimum revision", ErrRequirementIncomplete, f.InputName)
	}
	return nil
}

// ConsistencyRequirement is the resolve-time contract every ReadSnapshot is
// checked against: every resolved entry shares the same known-at horizon and
// the same tenant, and every named input meets its own declared minimum
// watermark.
//
// It carries no REQUIRED/OPTIONAL/CONDITIONAL per-input policy -- SNAPSHOT-002
// adds that. Under this baseline every input named in a Resolve call is
// mandatory: an input with no matching entry, or an entry that fails any of
// these checks, refuses the whole resolve rather than degrading it.
type ConsistencyRequirement struct {
	// Tenant is the single tenant every resolved entry must belong to.
	Tenant values.TenantId
	// KnownAtHorizon is the single knowledge cut-off every resolved entry
	// must share.
	KnownAtHorizon values.KnownAt
	// MinWatermarks are the per-input minimum watermark floors to enforce.
	// It may be empty: a requirement with no floors still enforces the
	// shared tenant and known-at horizon.
	MinWatermarks []InputWatermarkFloor
}

// Validate reports whether the requirement is well formed: a real tenant, a
// real known-at horizon, and watermark floors that each name a distinct
// input.
func (r ConsistencyRequirement) Validate() error {
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrRequirementIncomplete, err)
	}
	if r.KnownAtHorizon.Canonical() == nil {
		return fmt.Errorf("%w: requirement has no known-at horizon", ErrRequirementIncomplete)
	}
	seen := make(map[string]struct{}, len(r.MinWatermarks))
	for _, floor := range r.MinWatermarks {
		if err := floor.Validate(); err != nil {
			return err
		}
		if _, dup := seen[floor.InputName]; dup {
			return fmt.Errorf("%w: watermark floor %q declared twice", ErrRequirementIncomplete, floor.InputName)
		}
		seen[floor.InputName] = struct{}{}
	}
	return nil
}
