package reconcile

import (
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

// Status is a reconciliation job's own lifecycle state. It is a closed set of
// eight values; nothing in this package produces a ninth.
type Status string

// The declared statuses.
const (
	// StatusPending is a freshly triggered job that has not yet been polled.
	StatusPending Status = "PENDING"
	// StatusObserving is a resting, non-terminal state: the most recent
	// observation did not meet the job's required freshness, so the job stays
	// due rather than settling on an inconclusive look.
	StatusObserving Status = "OBSERVING"
	// StatusPass is a terminal comparison verdict: the canonical state matches
	// what was intended.
	StatusPass Status = "PASS"
	// StatusMismatch is a terminal comparison verdict: the canonical state
	// contradicts what was intended.
	StatusMismatch Status = "MISMATCH"
	// StatusPartial is a terminal comparison verdict: the canonical state
	// partially matches what was intended.
	StatusPartial Status = "PARTIAL"
	// StatusUnknown is a resting, non-terminal state a [Comparer] verdict may
	// report: even a fresh, complete observation could not be classified with
	// confidence. Like StatusObserving it stays due rather than being treated
	// as a final answer; only deadline exhaustion or a later determinate
	// verdict closes it.
	StatusUnknown Status = "UNKNOWN"
	// StatusExpired is a terminal state: the job reached its deadline with no
	// determinate verdict, and the committed effect declared no repair route.
	StatusExpired Status = "EXPIRED"
	// StatusRepairRequired is a terminal state: the job reached its deadline
	// with no determinate verdict, and the committed effect declared a repair
	// route that must now be engaged.
	StatusRepairRequired Status = "REPAIR_REQUIRED"
)

var declaredStatuses = map[Status]bool{
	StatusPending: true, StatusObserving: true, StatusPass: true, StatusMismatch: true,
	StatusPartial: true, StatusUnknown: true, StatusExpired: true, StatusRepairRequired: true,
}

// Valid reports whether s is one of the eight declared statuses.
func (s Status) Valid() bool { return declaredStatuses[s] }

// Terminal reports whether s is a closed, final state that a job never polls
// out of again. StatusPending, StatusObserving and StatusUnknown are the
// three resting states that remain due; every other declared status is
// terminal.
func (s Status) Terminal() bool {
	switch s {
	case StatusPass, StatusMismatch, StatusPartial, StatusExpired, StatusRepairRequired:
		return true
	default:
		return false
	}
}

// noRepairPolicy is the sentinel internal/effectgraph uses for "no repair
// route declared" (see effectgraph.Compile's IRREVERSIBLE_NEEDS_REPAIR
// check). A job carrying it settles to StatusExpired rather than
// StatusRepairRequired at exhaustion.
const noRepairPolicy = "NONE"

// exhaustStatus is the terminal status an unsettled job takes at its
// deadline: REPAIR_REQUIRED when the committed effect declared a repair
// route, EXPIRED when it declared none. Either way the row settles rather
// than disappearing.
func exhaustStatus(repairPolicy string) Status {
	if repairPolicy == "" || repairPolicy == noRepairPolicy {
		return StatusExpired
	}
	return StatusRepairRequired
}

// validVerdictStatus reports whether s is one of the four statuses a
// [Comparer] is allowed to report. A comparer that names any other status --
// including the job-lifecycle-only PENDING, OBSERVING, EXPIRED or
// REPAIR_REQUIRED -- is refused rather than trusted.
func validVerdictStatus(s Status) bool {
	switch s {
	case StatusPass, StatusMismatch, StatusPartial, StatusUnknown:
		return true
	default:
		return false
	}
}

// freshnessRank orders observe.Freshness from most to least reliable, mirroring
// internal/connectivity/observe's own (unexported) ranking, so that "does this
// observation meet the job's requirement" is comparable rather than an
// enumerated table of cases.
var freshnessRank = map[observe.Freshness]int{
	observe.FreshnessFresh:       0,
	observe.FreshnessStale:       1,
	observe.FreshnessUnknown:     2,
	observe.FreshnessPartial:     3,
	observe.FreshnessUnavailable: 4,
}

// meetsFreshness reports whether an observation's freshness is at least as
// reliable as a job requires. An unrecognised freshness value ranks below
// every declared one, so it never meets any requirement.
func meetsFreshness(got, required observe.Freshness) bool {
	gotRank, ok := freshnessRank[got]
	if !ok {
		return false
	}
	requiredRank, ok := freshnessRank[required]
	if !ok {
		return false
	}
	return gotRank <= requiredRank
}

// Job is one reconciliation-job row: the durable promise that a committed
// mandatory effect's external result will be checked against what was
// intended, under one comparison policy.
type Job struct {
	TenantID uuid.UUID
	JobID    uuid.UUID

	// EffectRef is the committed effect's own durable idempotency key
	// (internal/effectgraph.EffectNode.IdempotencyKey). EffectID is the
	// compiled graph's local node id, carried for evidence only: two graphs
	// can assign the same node id to different effects, so EffectRef, not
	// EffectID, is what makes a job unique to one effect.
	EffectRef string
	EffectID  string
	// PolicyRef names the comparison policy this job evaluates under.
	// RECON-002 owns what a policy reference resolves to; this package treats
	// it as an opaque identity.
	PolicyRef string

	// IntendedRef is what was intended: a reference into the proposal or
	// decision the effect committed. CanonicalRef is what an observation has
	// most recently named as the external system's own account, absent
	// (empty) until the first observation supplies one.
	IntendedRef  string
	CanonicalRef string

	// RequiredFreshness is the least reliable observe.Freshness this job will
	// accept before asking its Comparer for a verdict.
	RequiredFreshness observe.Freshness

	ObservationAttempts int
	// NextCheckAt is the next instant a caller should poll this job.
	// Deadline is the instant beyond which an unsettled job is exhausted.
	NextCheckAt time.Time
	Deadline    time.Time

	// Owner is the holder_id (internal/workflow/lease.Identity.HolderID) of
	// whichever caller most recently advanced this job under a verified
	// fence. SLARef names the service-level commitment this job is measured
	// against.
	Owner  string
	SLARef string

	// RepairPolicy is carried from the committed effect at trigger time, so
	// an exhausted job can settle to EXPIRED or REPAIR_REQUIRED without
	// re-reading the compiled graph.
	RepairPolicy string

	Status  Status
	Version uint64

	CreatedAt time.Time
	UpdatedAt time.Time
}
