package popscale

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// CountView is the shape of the count an observer receives.
type CountView uint8

// Count views.
const (
	// CountSuppressed carries no count at all. It is the one answer for a
	// denied count, an empty population and an unknown count alike: the
	// observer cannot tell which of the three it received.
	CountSuppressed CountView = iota
	// CountExact carries the exact count.
	CountExact
	// CountBanded carries a 50-wide band, never the exact number.
	CountBanded
)

// String returns the wire token.
func (v CountView) String() string {
	switch v {
	case CountExact:
		return "EXACT"
	case CountBanded:
		return "BANDED"
	default:
		return "SUPPRESSED"
	}
}

// CountResponse is what an observer receives for a count question. The zero
// value is the suppressed response and the one answer shared by denied, empty
// and unknown.
type CountResponse struct {
	View  CountView
	Exact int
	Band  string
}

// Suppressed is the canonical count answer that reveals nothing.
func Suppressed() CountResponse { return CountResponse{View: CountSuppressed} }

// DiscloseCount applies an authorized count view to the snapshot's recorded
// count. A concrete count is disclosed only when every one of these holds:
// the caller is count-authorized, the caller asked for a concrete view, the
// snapshot's count is a VALUE, and the value is positive. Any missing
// condition - including zero members, and a count that is absent, redacted,
// unavailable or unknown - yields the suppressed response, so "denied",
// "empty" and "unknown" are the same answer on the wire.
//
// A caller who is not count-authorized but asks for a concrete count is
// refused outright: serving it would leak a number the authorization decision
// withheld.
func DiscloseCount(authorized bool, view population.CountDisclosure, count values.Presence[int]) (CountResponse, error) {
	switch view {
	case population.CountDisclosureExact, population.CountDisclosureBanded:
		if !authorized {
			return CountResponse{}, rejected("count", "unauthorized")
		}
	}
	if !authorized || !count.IsValue() {
		return Suppressed(), nil
	}
	value, _ := count.Get()
	if value <= 0 {
		return Suppressed(), nil
	}
	switch view {
	case population.CountDisclosureExact:
		return CountResponse{View: CountExact, Exact: value}, nil
	case population.CountDisclosureBanded:
		return CountResponse{View: CountBanded, Band: band(value)}, nil
	default:
		return Suppressed(), nil
	}
}

// band rounds a positive count into a 50-wide band ("0-49", "50-99",
// "100-149", ...). The band text is exact integer arithmetic with no floats,
// and it never carries the exact count.
func band(value int) string {
	lo := (value / 50) * 50
	return fmt.Sprintf("%d-%d", lo, lo+49)
}
