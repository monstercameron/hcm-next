package values

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Canonical type tags for dataset-versioned timer values.
const (
	tagTimerReference byte = 0x0a
	tagTimerEvidence  byte = 0x0b
)

// Timer and dataset-versioning errors. All are matchable with errors.Is.
var (
	ErrReferenceUpdatePolicyRequired = errors.New("values: a future timer must declare a reference-update policy")
	ErrReferenceUpdatePolicy         = errors.New("values: unknown reference-update policy")
	ErrDatasetVersionRequired        = errors.New("values: dataset versions are required")
	ErrResolverRequired              = errors.New("values: replay needs a resolver")
)

// ReferenceUpdatePolicy declares what happens to an already-computed deadline
// when the timezone or business-calendar dataset that produced it is
// republished. There is no default, because the safe answer differs per timer:
// a statutory filing deadline is pinned, a rolling SLA recalculates, and a
// negotiated deadline needs a human.
type ReferenceUpdatePolicy uint8

// Reference-update policies.
const (
	// ReferenceUpdateUnspecified is the zero value and is never legal.
	ReferenceUpdateUnspecified ReferenceUpdatePolicy = iota
	// ReferenceUpdatePin keeps the deadline the historical dataset produced.
	// Replay recomputes with the historical dataset, not the current one.
	ReferenceUpdatePin
	// ReferenceUpdateRecalculate adopts the deadline the current dataset
	// produces. This is the only policy that may move a deadline.
	ReferenceUpdateRecalculate
	// ReferenceUpdateReviewRequired keeps the existing deadline and flags the
	// timer for human review when the current dataset would move it.
	ReferenceUpdateReviewRequired
)

var referenceUpdateWire = map[ReferenceUpdatePolicy]string{
	ReferenceUpdatePin:            "PIN",
	ReferenceUpdateRecalculate:    "RECALCULATE",
	ReferenceUpdateReviewRequired: "REVIEW_REQUIRED",
}

// AllReferenceUpdatePolicies returns every legal policy in canonical order.
func AllReferenceUpdatePolicies() []ReferenceUpdatePolicy {
	return []ReferenceUpdatePolicy{
		ReferenceUpdatePin,
		ReferenceUpdateRecalculate,
		ReferenceUpdateReviewRequired,
	}
}

// String returns the stable wire token.
func (p ReferenceUpdatePolicy) String() string {
	if w, ok := referenceUpdateWire[p]; ok {
		return w
	}
	return "REFERENCE_UPDATE_UNSPECIFIED"
}

// Valid reports whether p is a declared policy.
func (p ReferenceUpdatePolicy) Valid() bool {
	_, ok := referenceUpdateWire[p]
	return ok
}

// ParseReferenceUpdatePolicy decodes a wire token. The unspecified token is
// rejected.
func ParseReferenceUpdatePolicy(token string) (ReferenceUpdatePolicy, error) {
	for policy, wire := range referenceUpdateWire {
		if wire == token {
			return policy, nil
		}
	}
	return ReferenceUpdateUnspecified, fmt.Errorf("%w: %q", ErrReferenceUpdatePolicy, token)
}

// DatasetVersions names the timezone and business-calendar dataset releases a
// calculation ran against. Both halves are mandatory, because a deadline that
// cannot name its inputs cannot be replayed or defended.
type DatasetVersions struct {
	TzdbVersion     string
	CalendarVersion string
}

// Validate reports whether both dataset versions are present.
func (d DatasetVersions) Validate() error {
	if d.TzdbVersion == "" {
		return fmt.Errorf("%w: tzdb version is empty", ErrDatasetVersionRequired)
	}
	if d.CalendarVersion == "" {
		return fmt.Errorf("%w: calendar version is empty", ErrDatasetVersionRequired)
	}
	return nil
}

// String returns "tzdb=<v>,calendar=<v>".
func (d DatasetVersions) String() string {
	return "tzdb=" + d.TzdbVersion + ",calendar=" + d.CalendarVersion
}

// TimerReference is the dataset context a future timer must carry. A deadline
// computed without a zone, a tzdb version, a calendar version and an update
// policy cannot be replayed, so the platform refuses to schedule one.
type TimerReference struct {
	Zone     ZoneRef
	Calendar CalendarRef
	Policy   ReferenceUpdatePolicy
}

// Validate reports whether the reference is complete enough to schedule a
// future timer.
func (r TimerReference) Validate() error {
	if err := r.Zone.Validate(); err != nil {
		return err
	}
	if err := r.Calendar.Validate(); err != nil {
		return err
	}
	if r.Policy == ReferenceUpdateUnspecified {
		return ErrReferenceUpdatePolicyRequired
	}
	if !r.Policy.Valid() {
		return fmt.Errorf("%w: %d", ErrReferenceUpdatePolicy, uint8(r.Policy))
	}
	return nil
}

// HistoricalDataset returns the dataset versions the reference was created
// against. These are the versions a PIN replay uses.
func (r TimerReference) HistoricalDataset() DatasetVersions {
	return DatasetVersions{TzdbVersion: r.Zone.TzdbVersion, CalendarVersion: r.Calendar.Version}
}

// String returns "<zone>|<calendar>|<policy>".
func (r TimerReference) String() string {
	if r.Validate() != nil {
		return ""
	}
	return r.Zone.String() + "|" + r.Calendar.String() + "|" + r.Policy.String()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r TimerReference) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	out := []byte{tagTimerReference}
	out = appendLengthPrefixed(out, r.Zone.ID)
	out = appendLengthPrefixed(out, r.Zone.TzdbVersion)
	out = appendLengthPrefixed(out, r.Calendar.Ref)
	out = appendLengthPrefixed(out, r.Calendar.Version)
	return append(out, byte(r.Policy))
}

// TimerResolver recomputes a timer's due instant against a named dataset. The
// caller supplies it because the due expression itself is domain logic; this
// package owns only the replay protocol around it.
type TimerResolver func(DatasetVersions) (Instant, error)

// TimerReplayEvidence records everything needed to defend a replay decision:
// which datasets were used, what each produced, and what actually applies.
type TimerReplayEvidence struct {
	Zone              ZoneRef
	Calendar          CalendarRef
	Policy            ReferenceUpdatePolicy
	HistoricalDataset DatasetVersions
	CurrentDataset    DatasetVersions
	Pinned            Instant
	Recomputed        Instant
	RecomputeError    string
	Effective         Instant
	DatasetChanged    bool
	WouldChange       bool
	ReviewRequired    bool
}

// Canonical returns the canonical byte encoding of the evidence.
func (e TimerReplayEvidence) Canonical() []byte {
	out := []byte{tagTimerEvidence}
	out = appendLengthPrefixed(out, e.Zone.ID)
	out = appendLengthPrefixed(out, e.Zone.TzdbVersion)
	out = appendLengthPrefixed(out, e.Calendar.Ref)
	out = appendLengthPrefixed(out, e.Calendar.Version)
	out = append(out, byte(e.Policy))
	out = appendLengthPrefixed(out, e.HistoricalDataset.TzdbVersion)
	out = appendLengthPrefixed(out, e.HistoricalDataset.CalendarVersion)
	out = appendLengthPrefixed(out, e.CurrentDataset.TzdbVersion)
	out = appendLengthPrefixed(out, e.CurrentDataset.CalendarVersion)
	out = appendOptionalInstant(out, e.Pinned)
	out = appendOptionalInstant(out, e.Recomputed)
	out = appendOptionalInstant(out, e.Effective)
	out = appendLengthPrefixed(out, e.RecomputeError)
	return append(out, boolByte(e.DatasetChanged), boolByte(e.WouldChange), boolByte(e.ReviewRequired))
}

func appendOptionalInstant(dst []byte, i Instant) []byte {
	canon := i.Canonical()
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(canon)))
	return append(dst, canon...)
}

func boolByte(b bool) byte {
	if b {
		return 0x01
	}
	return 0x00
}

// TimerReplayOutcome is the decision a replay reached.
type TimerReplayOutcome struct {
	// Policy is the policy that produced the decision.
	Policy ReferenceUpdatePolicy
	// Effective is the instant that actually applies after the replay.
	Effective Instant
	// DatasetChanged reports whether the current dataset differs from the one
	// the timer was created against.
	DatasetChanged bool
	// WouldChange reports whether recomputing under the current dataset
	// produces a different instant. It is false when recomputation failed.
	WouldChange bool
	// ReviewRequired reports whether a human must look at this timer.
	ReviewRequired bool
	// Evidence is the full record of the replay.
	Evidence TimerReplayEvidence
}

// ReplayTimer decides what a timer's deadline is after a dataset refresh.
//
// Under PIN it recomputes with the historical dataset that the reference names,
// so a tzdb or calendar republish cannot move the deadline; the recomputation
// under the current dataset is still attempted and recorded as evidence, and a
// failure there is recorded rather than fatal. Under RECALCULATE it adopts the
// current dataset's answer, and a failure to compute one is fatal because there
// is no answer to adopt. Under REVIEW_REQUIRED the existing deadline stands and
// the timer is flagged whenever the current dataset would move it, or cannot be
// evaluated at all.
//
// No policy ever rewrites a deadline silently: a moved deadline only comes from
// RECALCULATE, and every outcome carries the evidence for what happened.
func ReplayTimer(ref TimerReference, pinned Instant, current DatasetVersions, resolve TimerResolver) (TimerReplayOutcome, error) {
	if err := ref.Validate(); err != nil {
		return TimerReplayOutcome{}, err
	}
	if err := pinned.Validate(); err != nil {
		return TimerReplayOutcome{}, err
	}
	if err := current.Validate(); err != nil {
		return TimerReplayOutcome{}, err
	}
	if resolve == nil {
		return TimerReplayOutcome{}, ErrResolverRequired
	}

	historical := ref.HistoricalDataset()
	if err := historical.Validate(); err != nil {
		return TimerReplayOutcome{}, err
	}

	evidence := TimerReplayEvidence{
		Zone:              ref.Zone,
		Calendar:          ref.Calendar,
		Policy:            ref.Policy,
		HistoricalDataset: historical,
		CurrentDataset:    current,
		Pinned:            pinned,
		DatasetChanged:    historical != current,
	}

	effective := pinned
	switch ref.Policy {
	case ReferenceUpdatePin:
		// The historical answer is the authority. If it cannot be reproduced,
		// the timer's evidence chain is broken and that is fatal.
		historicalDue, err := resolve(historical)
		if err != nil {
			return TimerReplayOutcome{}, fmt.Errorf("values: replay with the historical dataset %s: %w", historical, err)
		}
		if err := historicalDue.Validate(); err != nil {
			return TimerReplayOutcome{}, fmt.Errorf("values: historical replay produced no instant: %w", err)
		}
		effective = historicalDue
		recomputed, err := resolve(current)
		if err != nil {
			evidence.RecomputeError = err.Error()
		} else {
			evidence.Recomputed = recomputed
			evidence.WouldChange = recomputed.Compare(pinned) != 0
		}

	case ReferenceUpdateRecalculate:
		recomputed, err := resolve(current)
		if err != nil {
			return TimerReplayOutcome{}, fmt.Errorf("values: replay with the current dataset %s: %w", current, err)
		}
		if err := recomputed.Validate(); err != nil {
			return TimerReplayOutcome{}, fmt.Errorf("values: current replay produced no instant: %w", err)
		}
		evidence.Recomputed = recomputed
		evidence.WouldChange = recomputed.Compare(pinned) != 0
		effective = recomputed

	default: // ReferenceUpdateReviewRequired
		recomputed, err := resolve(current)
		switch {
		case err != nil:
			// A dataset that can no longer evaluate the timer is exactly the
			// case a human has to look at.
			evidence.RecomputeError = err.Error()
			evidence.ReviewRequired = true
		default:
			evidence.Recomputed = recomputed
			evidence.WouldChange = recomputed.Compare(pinned) != 0
			evidence.ReviewRequired = evidence.WouldChange
		}
	}

	evidence.Effective = effective
	return TimerReplayOutcome{
		Policy:         ref.Policy,
		Effective:      effective,
		DatasetChanged: evidence.DatasetChanged,
		WouldChange:    evidence.WouldChange,
		ReviewRequired: evidence.ReviewRequired,
		Evidence:       evidence,
	}, nil
}
