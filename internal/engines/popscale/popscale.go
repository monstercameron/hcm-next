// Package popscale scales population resolution: it paginates a frozen
// population snapshot's membership exactly once, discloses counts only to
// callers whose authorization the restriction decision allowed, and keeps
// denied, empty and unknown count answers indistinguishable so no observer can
// probe membership by asking for numbers.
//
// Semantic owner: shared-engines (POP-010). The package is a read engine. It
// takes an already-frozen population.Snapshot (POP-005) and an already-decided
// caller authorization (POP-004's restriction decision) and serves pages. It
// never resolves membership itself, never decides AuthZ, privacy, organization
// or purpose policy, and never writes: serving a page is a disclosure, not an
// authoritative row, business event, outbox entry, human work item or provider
// request - and a rejected resolution persists nothing either. A Sink passed
// in with a request must be empty when the call returns, on every path.
//
// All arithmetic is exact (integer counts, integer band math); there is no
// float64 anywhere in this package.
package popscale

import (
	"errors"
	"fmt"
)

// schemaVersion tags this engine's contract; rejections cite it.
const schemaVersion = 1

// Version reports this package's contract version, part of the ARCH-GO-009
// engine package contract, not a business-facing evaluation input.
func Version() int { return schemaVersion }

// versionToken is the version text a rejection cites.
func versionToken() string { return fmt.Sprintf("popscale/v%d", schemaVersion) }

// ErrRejected is matched by every POP-010 rejection via errors.Is.
var ErrRejected = errors.New("POP_010_REJECTED")

// Rejection names the offending field, state and version of a rejected
// resolution, so an operator can tell what was wrong without a heap trace.
type Rejection struct {
	Field   string
	State   string
	Version string
}

// Error renders the rejection with the stable POP_010_REJECTED token first,
// so log prefixes and error matches stay exact.
func (r *Rejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s", ErrRejected, r.Field, r.State, r.Version)
}

// Unwrap exposes the POP_010 token, so errors.Is(err, ErrRejected) matches
// every rejection.
func (r *Rejection) Unwrap() error { return ErrRejected }

// IsRejected reports whether err is a POP-010 rejection.
func IsRejected(err error) bool { return errors.Is(err, ErrRejected) }

func rejected(field, state string) *Rejection {
	return &Rejection{Field: field, State: state, Version: versionToken()}
}

// Sink records the five classes of authoritative side effect a handler might
// emit. POP-010 is a read engine, so no path - served page or rejection - may
// record anything here; callers pass a Sink to prove it.
type Sink struct {
	AuthoritativeRows int
	BusinessEvents    int
	OutboxEntries     int
	HumanWork         int
	ProviderRequests  int
}

// Total is the sum of every recorded side effect.
func (s *Sink) Total() int {
	return s.AuthoritativeRows + s.BusinessEvents + s.OutboxEntries + s.HumanWork + s.ProviderRequests
}
