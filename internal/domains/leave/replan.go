package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Replan routes: the exact acknowledgement ladder.
const (
	ReplanNone          = "none"
	ReplanReacknowledge = "reacknowledge"
	ReplanReapproval    = "reapproval"
)

// FundingChange is one intervening availability movement in hours.
type FundingChange struct {
	SourceID     string
	OldAvailable int
	NewAvailable int
	NewRevision  string
}

// Replan is the partial leave replan: changed paid treatment and leave
// entitlement stay separate outcome dimensions, unaffected protection and
// evidence decisions survive by explicit policy, and the exact
// reacknowledgement or reapproval route gates execution.
type Replan struct {
	PriorDigest       string
	NewDigest         string
	Segments          []Segment
	PaidDelta         int
	EntitlementDelta  int
	AdditionalUnpaid  int
	RetainedSegments  []string
	RetainedDecisions []string
	Policy            string
	Route             string
	Trace             []string
}

func replanDigest(prior string, segments []Segment, paid, entitlement, unpaid int, retained []string, policy, route string) string {
	parts := []string{"leave-replan", prior, fmt.Sprint(paid, entitlement, unpaid), strings.Join(retained, ","), policy, route}
	for _, segment := range segments {
		parts = append(parts, strings.Join([]string{fmt.Sprint(segment.StartDay, segment.EndDay), segment.Kind, fmt.Sprint(segment.Hours)}, "\x01"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ReplanLeave partially replans after material drift. Shortfalls convert
// paid segments to unpaid explicitly — pay never changes silently — while
// unaffected segments and named decisions survive only under the explicit
// retention policy. Pay movement routes reapproval; untouched
// protection routes reacknowledgement; no drift routes nothing.
func ReplanLeave(prior LeaveEntitlementPlan, funding []FundingChange, retainedDecisions []string, policy string) (Replan, error) {
	if err := prior.Verify(); err != nil {
		return Replan{}, fmt.Errorf("leave: replan refuses an unsealed prior plan: %v", err)
	}
	if strings.TrimSpace(policy) == "" {
		return Replan{}, fmt.Errorf("leave: replan preserves unaffected decisions only under an explicit policy")
	}
	replan := Replan{PriorDigest: prior.Digest, Policy: policy, RetainedDecisions: append([]string(nil), retainedDecisions...)}
	chronicle := func(format string, args ...any) {
		replan.Trace = append(replan.Trace, fmt.Sprintf(format, args...))
	}
	shortfall := 0
	for _, change := range funding {
		if strings.TrimSpace(change.SourceID) == "" || strings.TrimSpace(change.NewRevision) == "" {
			return Replan{}, fmt.Errorf("leave: funding changes need a source and a revision")
		}
		if change.NewAvailable < change.OldAvailable {
			shortfall += change.OldAvailable - change.NewAvailable
			chronicle("funding %s moved %d to %d hours", change.SourceID, change.OldAvailable, change.NewAvailable)
		}
	}
	segments := append([]Segment(nil), prior.Segments...)
	remaining := shortfall
	for i := range segments {
		if remaining <= 0 {
			break
		}
		if segments[i].Kind != SegmentPaid {
			continue
		}
		convert := segments[i].Hours
		if convert > remaining {
			convert = remaining
		}
		remaining -= convert
		segments[i].Kind = SegmentUnpaid
		segments[i].Programs = nil
		replan.AdditionalUnpaid += convert
		replan.PaidDelta -= convert
		chronicle("day %d converts %d paid hours to unpaid", segments[i].StartDay, convert)
	}
	if remaining > 0 {
		return Replan{}, fmt.Errorf("leave: shortfall of %d hours exceeds convertible paid time", remaining)
	}
	for _, segment := range segments {
		if segment.Kind != SegmentUnpaid {
			replan.RetainedSegments = append(replan.RetainedSegments, fmt.Sprintf("day-%d:%s", segment.StartDay, segment.Kind))
		}
	}
	replan.Segments = segments
	switch {
	case replan.PaidDelta != 0:
		replan.Route = ReplanReapproval
	case len(funding) > 0:
		replan.Route = ReplanReacknowledge
	default:
		replan.Route = ReplanNone
	}
	chronicle("unaffected protection survives under %q; route %s", policy, replan.Route)
	replan.NewDigest = replanDigest(prior.Digest, segments, replan.PaidDelta, replan.EntitlementDelta, replan.AdditionalUnpaid, replan.RetainedSegments, policy, replan.Route)
	return replan, nil
}

// Verify recomputes the replan seal.
func (replan Replan) Verify() error {
	if replan.NewDigest == "" || replanDigest(replan.PriorDigest, replan.Segments, replan.PaidDelta, replan.EntitlementDelta, replan.AdditionalUnpaid, replan.RetainedSegments, replan.Policy, replan.Route) != replan.NewDigest {
		return fmt.Errorf("leave: replan seal is broken")
	}
	if replan.NewDigest == replan.PriorDigest && (replan.PaidDelta != 0 || len(replan.Trace) > 1) {
		return fmt.Errorf("leave: replan must advance past the prior digest")
	}
	return nil
}
