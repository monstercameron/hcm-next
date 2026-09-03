package legal

import (
	"errors"
	"fmt"
	"strings"
)

// Jurisdiction errors. All are matchable with errors.Is.
var (
	ErrJurisdictionCountry  = errors.New("legal: jurisdiction country is not an uppercase ISO 3166-1 alpha-2 code")
	ErrJurisdictionState    = errors.New("legal: jurisdiction state/subdivision code is malformed")
	ErrJurisdictionLocality = errors.New("legal: jurisdiction locality is malformed")
)

// Jurisdiction is the country/state/locality tuple that a legal rule pack is
// published against. It is a plain fact carrier: nothing in this type infers
// a jurisdiction from anything else, including a locale.
//
// Country is mandatory. State is mandatory whenever the jurisdiction's law is
// state-level (every rule pack this package seeds is state-level). Locality
// is optional; a rule pack scoped to a whole state leaves it empty.
type Jurisdiction struct {
	// Country is an uppercase ISO 3166-1 alpha-2 code, e.g. "US".
	Country string
	// State is an uppercase ISO 3166-2 principal-subdivision code without the
	// country prefix, e.g. "CA", "NY". Empty means the jurisdiction is
	// resolved only to country level.
	State string
	// Locality is a free-form city or county name. Empty means the
	// jurisdiction is resolved only to state (or country) level.
	Locality string
}

// Validate reports whether j is a well-formed jurisdiction fact. It does not
// check that the country/state pair actually exists in any registry; that is
// the [Registry]'s job when it looks up a rule pack.
func (j Jurisdiction) Validate() error {
	if len(j.Country) != 2 {
		return fmt.Errorf("%w: %q", ErrJurisdictionCountry, j.Country)
	}
	for i := 0; i < 2; i++ {
		c := j.Country[i]
		if c < 'A' || c > 'Z' {
			return fmt.Errorf("%w: %q", ErrJurisdictionCountry, j.Country)
		}
	}
	if j.State != "" {
		if len(j.State) < 2 || len(j.State) > 3 {
			return fmt.Errorf("%w: %q", ErrJurisdictionState, j.State)
		}
		for i := 0; i < len(j.State); i++ {
			c := j.State[i]
			if c < 'A' || c > 'Z' {
				return fmt.Errorf("%w: %q", ErrJurisdictionState, j.State)
			}
		}
	}
	if strings.TrimSpace(j.Locality) != j.Locality {
		return fmt.Errorf("%w: %q has leading or trailing space", ErrJurisdictionLocality, j.Locality)
	}
	return nil
}

// IsZero reports whether j carries no jurisdiction fact at all.
func (j Jurisdiction) IsZero() bool { return j == Jurisdiction{} }

// IsStateResolved reports whether j resolves to at least country and state.
// A rule pack lookup requires this; country-only never matches a state-level
// pack.
func (j Jurisdiction) IsStateResolved() bool {
	return j.Validate() == nil && j.Country != "" && j.State != ""
}

// Equal reports whether two jurisdictions name the same place.
func (j Jurisdiction) Equal(other Jurisdiction) bool { return j == other }

// String returns "<country>-<state>[-<locality>]", or the empty string for a
// zero jurisdiction.
func (j Jurisdiction) String() string {
	if j.IsZero() {
		return ""
	}
	s := j.Country
	if j.State != "" {
		s += "-" + j.State
	}
	if j.Locality != "" {
		s += "-" + j.Locality
	}
	return s
}

// canonicalBytes appends a deterministic, length-prefixed encoding of j to
// dst. It is part of the canonical encoding every signed or digested legal
// type shares; see canonical.go.
func (j Jurisdiction) canonicalBytes(dst []byte) []byte {
	dst = appendField(dst, "country", j.Country)
	dst = appendField(dst, "state", j.State)
	dst = appendField(dst, "locality", j.Locality)
	return dst
}
