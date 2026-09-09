package dsr

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrIntakeInvalid is returned by [Intake] when the spec it was given
// cannot become a valid [DataSubjectRequest].
var ErrIntakeInvalid = errors.New("dsr: intake spec is invalid")

// IntakeSpec is everything [Intake] needs to record one claimed request. It
// carries no verification evidence: that is deliberately a separate,
// later call to [DataSubjectRequest.Verify].
type IntakeSpec struct {
	ID           string
	Tenant       values.TenantId
	Kind         Kind
	Claims       SubjectClaims
	Jurisdiction legal.Jurisdiction
	ReceivedAt   values.Instant
	Channel      Channel
}

// DetectDuplicate scans existing for the earliest request that shares
// candidate's tenant, [SubjectClaims.Key] and [Kind], with a received-at
// instant within window of candidate's. It never matches across tenants,
// even when every other field lines up -- PRIV-005's RED clause on
// cross-tenant subjects applies to duplicate linkage as much as to
// verification. Ties (equal ReceivedAt) are broken by the lowest ID, so the
// result is deterministic regardless of existing's order.
func DetectDuplicate(existing []DataSubjectRequest, candidate DataSubjectRequest, window time.Duration) (DataSubjectRequest, bool) {
	key := candidate.Claims.Key()
	if key == "" {
		return DataSubjectRequest{}, false
	}

	var best DataSubjectRequest
	found := false
	for _, other := range existing {
		if other.Tenant != candidate.Tenant {
			continue
		}
		if other.Kind != candidate.Kind {
			continue
		}
		if other.Claims.Key() != key {
			continue
		}
		delta := candidate.ReceivedAt.Time().Sub(other.ReceivedAt.Time())
		if delta < 0 {
			delta = -delta
		}
		if delta > window {
			continue
		}
		if !found {
			best, found = other, true
			continue
		}
		if other.ReceivedAt.Before(best.ReceivedAt) {
			best = other
			continue
		}
		if other.ReceivedAt.Compare(best.ReceivedAt) == 0 && other.ID < best.ID {
			best = other
		}
	}
	return best, found
}

// Intake validates spec, resolves spec.Kind's statutory response deadline
// from clock (see [ClockTable.Deadline]), checks spec against existing for
// a duplicate within duplicateWindow (see [DetectDuplicate]), and returns
// the resulting immutable [DataSubjectRequest] with its initial
// [Evidence] trail entry (or two, when a duplicate was linked).
//
// Intake never refuses a duplicate submission outright: it always records
// the request, linking it via [DataSubjectRequest.DuplicateOf] rather than
// creating an independent, separately-executable second request for the
// same subject/kind/window. The returned request's
// [DataSubjectRequest.VerificationState] is always [VerificationUnverified]
// -- Intake performs no identity verification.
func Intake(spec IntakeSpec, clock ClockTable, existing []DataSubjectRequest, duplicateWindow time.Duration) (DataSubjectRequest, error) {
	if spec.ID == "" {
		return DataSubjectRequest{}, fmt.Errorf("%w: no id", ErrIntakeInvalid)
	}
	if err := spec.Tenant.Validate(); err != nil {
		return DataSubjectRequest{}, fmt.Errorf("%w: %v", ErrIntakeInvalid, err)
	}
	if err := spec.Kind.Validate(); err != nil {
		return DataSubjectRequest{}, fmt.Errorf("%w: %v", ErrIntakeInvalid, err)
	}
	if err := spec.Claims.Validate(); err != nil {
		return DataSubjectRequest{}, fmt.Errorf("%w: %v", ErrIntakeInvalid, err)
	}
	if err := spec.Jurisdiction.Validate(); err != nil {
		return DataSubjectRequest{}, fmt.Errorf("%w: jurisdiction %v", ErrIntakeInvalid, err)
	}
	if !spec.ReceivedAt.IsSet() {
		return DataSubjectRequest{}, fmt.Errorf("%w: no received_at", ErrIntakeInvalid)
	}
	if err := spec.Channel.Validate(); err != nil {
		return DataSubjectRequest{}, fmt.Errorf("%w: %v", ErrIntakeInvalid, err)
	}
	if duplicateWindow < 0 {
		return DataSubjectRequest{}, fmt.Errorf("%w: negative duplicate window", ErrIntakeInvalid)
	}

	deadline, err := clock.Deadline(spec.Jurisdiction, spec.Kind, spec.ReceivedAt)
	if err != nil {
		return DataSubjectRequest{}, fmt.Errorf("%w: %v", ErrIntakeInvalid, err)
	}

	req := DataSubjectRequest{
		ID:                spec.ID,
		Tenant:            spec.Tenant,
		Kind:              spec.Kind,
		Claims:            spec.Claims,
		Jurisdiction:      spec.Jurisdiction,
		ReceivedAt:        spec.ReceivedAt,
		Channel:           spec.Channel,
		Deadline:          deadline,
		VerificationState: VerificationUnverified,
	}

	if dup, ok := DetectDuplicate(existing, req, duplicateWindow); ok {
		req.DuplicateOf = dup.ID
	}

	// DuplicateOf is already set above, so a single digest/evidence-id
	// computation here covers it; appendEvidence never changes Digest (see
	// its doc comment), so no further recomputation is needed below.
	req = req.withEvidenceID()
	req = req.appendEvidence(EventIntake, spec.ReceivedAt, string(spec.Kind))
	if req.DuplicateOf != "" {
		req = req.appendEvidence(EventDuplicateLinked, spec.ReceivedAt, req.DuplicateOf)
	}

	if err := req.Validate(); err != nil {
		return DataSubjectRequest{}, err
	}
	return req, nil
}
