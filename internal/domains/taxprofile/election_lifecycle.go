package taxprofile

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ElectionValidationStatus describes the result at a requested effective
// instant. UNKNOWN is used when the requested instant cannot be evaluated.
type ElectionValidationStatus string

const (
	ElectionValidationValid      ElectionValidationStatus = "VALID"
	ElectionValidationIncomplete ElectionValidationStatus = "INCOMPLETE"
	ElectionValidationInvalid    ElectionValidationStatus = "INVALID"
	ElectionValidationExpired    ElectionValidationStatus = "EXPIRED"
	ElectionValidationUnknown    ElectionValidationStatus = "UNKNOWN"
)

type ElectionValidation struct {
	Status  ElectionValidationStatus
	Reasons []string
}

func (r ElectionValidation) Valid() bool { return r.Status == ElectionValidationValid }

// ValidateElectionAt validates the submission and then evaluates its
// effective interval. No missing form or evidence is silently treated as a
// valid election.
func ValidateElectionAt(e WithholdingElectionRevision, at values.Instant) ElectionValidation {
	if e.FormRevisionRef == "" || e.EvidenceRef == "" {
		return ElectionValidation{Status: ElectionValidationIncomplete, Reasons: []string{"form revision and evidence are required"}}
	}
	if err := e.Validate(); err != nil {
		return ElectionValidation{Status: ElectionValidationInvalid, Reasons: []string{err.Error()}}
	}
	if err := at.Validate(); err != nil {
		return ElectionValidation{Status: ElectionValidationUnknown, Reasons: []string{"evaluation instant is required"}}
	}
	inside, err := e.Effective.ContainsInstant(at)
	if err != nil {
		return ElectionValidation{Status: ElectionValidationUnknown, Reasons: []string{err.Error()}}
	}
	if !inside {
		if end, ok := e.Effective.EndInstant(); ok && !at.Before(end) {
			return ElectionValidation{Status: ElectionValidationExpired, Reasons: []string{"effective interval has ended"}}
		}
		return ElectionValidation{Status: ElectionValidationUnknown, Reasons: []string{"election is not effective at the requested instant"}}
	}
	return ElectionValidation{Status: ElectionValidationValid}
}

var (
	ErrElectionAmbiguous  = errors.New("taxprofile: multiple elections win at the same effective instant")
	ErrElectionCAS        = errors.New("taxprofile: election predecessor changed")
	ErrElectionCorrection = errors.New("taxprofile: invalid election correction")
)

// ResolveEffectiveElection selects the latest known applicable election,
// breaking equal knowledge times by effective start. Equal effective starts
// are refused rather than allowing two revisions to win the same boundary.
func ResolveEffectiveElection(elections []WithholdingElectionRevision, at values.Instant) (WithholdingElectionRevision, error) {
	if err := at.Validate(); err != nil {
		return WithholdingElectionRevision{}, fmt.Errorf("%w: evaluation instant: %v", ErrElectionCorrection, err)
	}
	chosen := make([]WithholdingElectionRevision, 0, len(elections))
	var scope WithholdingElectionRevision
	for _, e := range elections {
		if scope.ElectionID == "" {
			scope = e
		} else if e.ElectionID != scope.ElectionID || e.WorkerRef != scope.WorkerRef || e.Jurisdiction != scope.Jurisdiction {
			return WithholdingElectionRevision{}, fmt.Errorf("%w: mixed election identity scope", ErrElectionCorrection)
		}
		inside, intervalErr := e.Effective.ContainsInstant(at)
		if intervalErr != nil {
			return WithholdingElectionRevision{}, fmt.Errorf("%w: effective interval: %v", ErrElectionCorrection, intervalErr)
		}
		validation := ValidateElectionAt(e, at)
		if inside && !validation.Valid() {
			return WithholdingElectionRevision{}, fmt.Errorf("%w: applicable election is %s", ErrElectionCorrection, validation.Status)
		}
		if !inside {
			continue
		}
		chosen = append(chosen, e)
	}
	if len(chosen) == 0 {
		return WithholdingElectionRevision{}, ErrStoreNotFound
	}
	for i, candidate := range chosen {
		candidateStart, _ := candidate.Effective.StartInstant()
		for _, other := range chosen[i+1:] {
			otherStart, _ := other.Effective.StartInstant()
			if candidateStart.Compare(otherStart) == 0 {
				return WithholdingElectionRevision{}, ErrElectionAmbiguous
			}
		}
	}
	sort.SliceStable(chosen, func(i, j int) bool {
		if comparison := chosen[i].KnownAt.Compare(chosen[j].KnownAt); comparison != 0 {
			return comparison < 0
		}
		a, _ := chosen[i].Effective.StartInstant()
		b, _ := chosen[j].Effective.StartInstant()
		return a.Before(b)
	})
	return chosen[len(chosen)-1], nil
}

// ElectionCorrectionIntent records a late change without changing the
// original election. Closed payroll periods require a downstream correction.
type ElectionCorrectionIntent struct {
	OriginalDigest  string
	SuccessorDigest string
	Effective       values.EffectiveInterval
	Retroactive     bool
	PayrollAction   string
	Digest          string
}

func (i ElectionCorrectionIntent) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.taxprofile.ElectionCorrectionIntent", schemaVersion).
		String("original_digest", i.OriginalDigest).String("successor_digest", i.SuccessorDigest).
		Value("effective", i.Effective).Bool("retroactive", i.Retroactive).String("payroll_action", i.PayrollAction)
	b, _ := w.Bytes()
	return b
}

// CorrectElection creates an append-only correction intent. closedPayroll is
// supplied by the payroll boundary; this package does not invent tax rates or
// decide whether a payroll period is legally closed.
func CorrectElection(original, successor WithholdingElectionRevision, closedPayroll bool) (ElectionCorrectionIntent, error) {
	var err error
	if original.CanonicalDigest == "" {
		original, err = NewWithholdingElectionRevision(original)
	} else {
		err = original.Validate()
	}
	if err != nil {
		return ElectionCorrectionIntent{}, fmt.Errorf("%w: original: %v", ErrElectionCorrection, err)
	}
	if successor.CanonicalDigest == "" {
		successor, err = NewWithholdingElectionRevision(successor)
	} else {
		err = successor.Validate()
	}
	if err != nil {
		return ElectionCorrectionIntent{}, fmt.Errorf("%w: successor: %v", ErrElectionCorrection, err)
	}
	if original.ElectionID != successor.ElectionID || original.WorkerRef != successor.WorkerRef || original.Jurisdiction != successor.Jurisdiction {
		return ElectionCorrectionIntent{}, fmt.Errorf("%w: identity cannot change", ErrElectionCorrection)
	}
	if original.CanonicalDigest == successor.CanonicalDigest {
		return ElectionCorrectionIntent{}, fmt.Errorf("%w: successor must differ", ErrElectionCorrection)
	}
	start, _ := successor.Effective.StartInstant()
	oldStart, _ := original.Effective.StartInstant()
	intent := ElectionCorrectionIntent{OriginalDigest: original.CanonicalDigest, SuccessorDigest: successor.CanonicalDigest, Effective: successor.Effective, Retroactive: start.Before(oldStart) || closedPayroll, PayrollAction: "NONE"}
	if closedPayroll {
		intent.PayrollAction = "RETROACTIVE_PAYROLL_CORRECTION_REQUIRED"
	}
	intent.Digest = canonicalbytes.Digest(intent.Canonical())
	return intent, nil
}
