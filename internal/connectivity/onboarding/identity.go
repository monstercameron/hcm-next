package onboarding

import (
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

// IdentityOutcome is the adjudicated result of resolving one external
// identity against a pinned crosswalk snapshot.
//
// There is no "best guess" value. Every call to [Adjudicator.Adjudicate]
// returns exactly one of these four: a resolution the process is willing to
// stand behind without review (EXACT), a resolution that needs a human's eyes
// before it can be trusted (CANDIDATE), an honest absence (UNMATCHED -
// operator language: UNRESOLVED), or an irreconcilable collision (CONFLICT -
// operator language: AMBIGUOUS) that must never auto-commit.
type IdentityOutcome string

// The declared outcomes.
const (
	// IdentityExact means exactly one pinned crosswalk entry the source
	// system asserted as an exact, system-issued mapping matched the
	// external id.
	IdentityExact IdentityOutcome = "EXACT"
	// IdentityCandidate means exactly one plausible match was found by a
	// weaker comparison (a mutable or fuzzy field); it may be surfaced for
	// review but never auto-committed.
	IdentityCandidate IdentityOutcome = "CANDIDATE"
	// IdentityUnmatched means the pinned crosswalk has no entry addressing
	// this external id at all. Operator language: UNRESOLVED.
	IdentityUnmatched IdentityOutcome = "UNMATCHED"
	// IdentityConflict means two or more pinned crosswalk entries disagree
	// about which canonical identity this external id resolves to. Operator
	// language: AMBIGUOUS.
	IdentityConflict IdentityOutcome = "CONFLICT"
)

// Valid reports whether o is a declared outcome.
func (o IdentityOutcome) Valid() bool {
	switch o {
	case IdentityExact, IdentityCandidate, IdentityUnmatched, IdentityConflict:
		return true
	default:
		return false
	}
}

// CrosswalkEntry is one mapping from an external identity to a canonical one,
// as of the crosswalk snapshot that carries it.
type CrosswalkEntry struct {
	Object     connectivity.ObjectKind
	ExternalID string
	// CanonicalID is the canonical identity this entry maps to.
	CanonicalID string
	// Exact states that the source of this entry asserted an unambiguous,
	// system-issued mapping. An entry with Exact false is candidate evidence
	// only: adjudication may surface it, but never auto-commits it.
	Exact bool
}

// CrosswalkSnapshot is one pinned, immutable crosswalk version an onboarding
// job adjudicates identities against.
type CrosswalkSnapshot struct {
	Name    string
	Version string
	// Digest is the lowercase hex sha256 over the snapshot's content, matched
	// against the manifest's [ReferencePin] before any adjudication runs.
	Digest  string
	Entries []CrosswalkEntry
}

// MatchesPin reports whether the snapshot is the exact version and digest the
// manifest pinned.
func (s CrosswalkSnapshot) MatchesPin(pin ReferencePin) bool {
	return s.Name == pin.Name && s.Version == pin.Version && strings.EqualFold(s.Digest, pin.Digest)
}

// Adjudication is one external identity's resolution, with the evidence a
// reviewer or a downstream governance gate needs to trust or contest it.
type Adjudication struct {
	Object     connectivity.ObjectKind
	ExternalID string
	Outcome    IdentityOutcome
	// CanonicalID is set only when Outcome is EXACT.
	CanonicalID string
	// Candidates lists the canonical ids in play, ascending, when Outcome is
	// CANDIDATE or CONFLICT.
	Candidates []string

	CrosswalkName    string
	CrosswalkVersion string
	AdjudicatedAt    time.Time

	// ReviewedBy, ReviewedAt and ReviewerCanonicalID are set by
	// [Adjudication.Resolve] once a human decides a CANDIDATE or CONFLICT
	// row. They are zero until then.
	ReviewedBy          string
	ReviewedAt          time.Time
	ReviewerCanonicalID string
}

// Resolve records a reviewer's decision on a CANDIDATE or CONFLICT
// adjudication and returns the resolved copy.
//
// It refuses on an EXACT or UNMATCHED row - those have nothing for a reviewer
// to decide - and refuses a canonical id the row never presented as a
// candidate: approving a row is not a backdoor for typing in an unrelated
// identity.
func (a Adjudication) Resolve(reviewerRef, canonicalID string, at time.Time) (Adjudication, error) {
	const op = "onboarding.Adjudication.Resolve"
	switch a.Outcome {
	case IdentityCandidate, IdentityConflict:
	default:
		return Adjudication{}, newError(op, ErrInvalidReview,
			"outcome %s has no reviewer decision to record", a.Outcome)
	}
	if strings.TrimSpace(reviewerRef) == "" {
		return Adjudication{}, newError(op, ErrInvalidReview, "reviewer decision has no reviewer")
	}
	if at.IsZero() {
		return Adjudication{}, newError(op, ErrInvalidReview, "reviewer decision has no time")
	}
	found := false
	for _, c := range a.Candidates {
		if c == canonicalID {
			found = true
			break
		}
	}
	if !found {
		return Adjudication{}, newError(op, ErrInvalidReview,
			"canonical id %q was never presented as a candidate for external id %q", canonicalID, a.ExternalID)
	}
	out := a
	out.ReviewedBy = reviewerRef
	out.ReviewedAt = at.UTC()
	out.ReviewerCanonicalID = canonicalID
	return out, nil
}

// Adjudicator resolves external identities against one pinned crosswalk
// snapshot per object. It never guesses.
type Adjudicator struct {
	// Snapshots is the loaded crosswalk snapshot per object.
	Snapshots map[connectivity.ObjectKind]CrosswalkSnapshot
	// Now supplies the adjudication timestamp. Nil uses the wall clock.
	Now func() time.Time
}

func (a Adjudicator) now() time.Time {
	if a.Now == nil {
		return time.Now().UTC()
	}
	return a.Now().UTC()
}

// Adjudicate resolves one external id for object against the loaded
// crosswalk snapshot, which must match pin exactly.
//
// Adjudicate refuses to run at all - returning [ErrStalePin] - when the
// loaded snapshot does not match the manifest's pin: adjudicating against a
// crosswalk other than the one the manifest pinned would make the pin
// meaningless.
func (a Adjudicator) Adjudicate(
	object connectivity.ObjectKind, externalID string, pin ReferencePin,
) (Adjudication, error) {
	const op = "onboarding.Adjudicator.Adjudicate"
	if err := pin.Validate(); err != nil {
		return Adjudication{}, err
	}
	if strings.TrimSpace(externalID) == "" {
		return Adjudication{}, newError(op, ErrInvalidManifest, "adjudication has no external id")
	}
	snapshot, ok := a.Snapshots[object]
	if !ok {
		return Adjudication{}, newError(op, ErrNotFound, "no crosswalk snapshot loaded for object %s", object)
	}
	if !snapshot.MatchesPin(pin) {
		return Adjudication{}, newError(op, ErrStalePin,
			"loaded crosswalk %s@%s does not match the manifest pin %s@%s",
			snapshot.Name, snapshot.Version, pin.Name, pin.Version)
	}

	var exact, candidate []string
	for _, e := range snapshot.Entries {
		if e.Object != object || e.ExternalID != externalID {
			continue
		}
		if e.Exact {
			exact = appendUnique(exact, e.CanonicalID)
		} else {
			candidate = appendUnique(candidate, e.CanonicalID)
		}
	}
	sort.Strings(exact)
	sort.Strings(candidate)

	out := Adjudication{
		Object: object, ExternalID: externalID,
		CrosswalkName: snapshot.Name, CrosswalkVersion: snapshot.Version,
		AdjudicatedAt: a.now(),
	}
	switch {
	case len(exact) == 1:
		out.Outcome = IdentityExact
		out.CanonicalID = exact[0]
	case len(exact) > 1:
		out.Outcome = IdentityConflict
		out.Candidates = exact
	case len(candidate) == 1:
		out.Outcome = IdentityCandidate
		out.Candidates = candidate
	case len(candidate) > 1:
		out.Outcome = IdentityConflict
		out.Candidates = candidate
	default:
		out.Outcome = IdentityUnmatched
	}
	return out, nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
