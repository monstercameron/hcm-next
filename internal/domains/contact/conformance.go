package contact

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ReissueContactChallenge atomically describes the two state transitions a
// store must commit together: the former challenge becomes unusable and the
// replacement is a new, independently scoped challenge. No token is copied
// between challenges.
func ReissueContactChallenge(previous, replacement ContactVerificationChallenge, now time.Time) (ContactVerificationChallenge, ContactVerificationChallenge, error) {
	if err := previous.Validate(); err != nil {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, err
	}
	if err := replacement.Validate(); err != nil {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, err
	}
	if previous.Status != ContactChallengeIssued || replacement.Status != ContactChallengeIssued {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, fmt.Errorf("%w: challenges must be issued", ErrInvalidReissue)
	}
	if previous.ChallengeID == replacement.ChallengeID || previous.TokenDigest == replacement.TokenDigest {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, fmt.Errorf("%w: replacement must have a new id and token", ErrInvalidReissue)
	}
	if previous.Subject != replacement.Subject || previous.EndpointID != replacement.EndpointID || previous.Purpose != replacement.Purpose {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, fmt.Errorf("%w: challenge scope changed", ErrInvalidReissue)
	}
	if now.IsZero() || now.Before(replacement.IssuedAt) || !now.Before(previous.ExpiresAt) || !replacement.IssuedAt.After(previous.IssuedAt) || !now.Before(replacement.ExpiresAt) {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, fmt.Errorf("%w: reissue time is invalid", ErrInvalidReissue)
	}
	old := previous.appendEvent(ChallengeEventRevoked, now, previous.Attempts, "")
	old.Status = ContactChallengeRevoked
	old.CanonicalDigest = old.computedDigest()
	if err := old.Validate(); err != nil {
		return ContactVerificationChallenge{}, ContactVerificationChallenge{}, err
	}
	return old, replacement, nil
}

// ContactObservation is provider-supplied evidence. It is deliberately a
// digest and never a raw address; an observation can request repair but cannot
// overwrite the Person-owned endpoint revision.
type ContactObservation struct {
	Subject               values.EntityRef
	EndpointID            string
	Purpose               string
	NormalizedValueDigest string
	Source                string
	ObservedAt            time.Time
}

func (o ContactObservation) Validate() error {
	if err := o.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrExternalContactMismatch, err)
	}
	if strings.TrimSpace(o.EndpointID) == "" || strings.TrimSpace(o.Purpose) == "" || !digestString(o.NormalizedValueDigest) || strings.TrimSpace(o.Source) == "" || o.ObservedAt.IsZero() {
		return fmt.Errorf("%w: observation is incomplete", ErrExternalContactMismatch)
	}
	return nil
}

type ContactReconciliationStatus string

const (
	ContactReconciled     ContactReconciliationStatus = "RECONCILED"
	ContactRepairRequired ContactReconciliationStatus = "REPAIR_REQUIRED"
)

// ContactRepair is a bounded proposal. Applying it requires the normal
// correction/evidence workflow; this package does not mutate authoritative
// contact truth from an external observation.
type ContactRepair struct {
	Subject                values.EntityRef
	EndpointID             string
	Purpose                string
	ExpectedValueDigest    string
	ExpectedRevisionDigest string
	ObservedValueDigest    string
	ObservationSource      string
}

type ContactReconciliation struct {
	Status ContactReconciliationStatus
	Repair *ContactRepair
}

func ReconcileExternalContact(current ContactEndpointRevision, observation ContactObservation) (ContactReconciliation, error) {
	if err := current.Validate(); err != nil {
		return ContactReconciliation{}, err
	}
	if err := observation.Validate(); err != nil {
		return ContactReconciliation{}, err
	}
	if current.Subject != observation.Subject || current.EndpointID != observation.EndpointID || current.Purpose != observation.Purpose {
		return ContactReconciliation{}, fmt.Errorf("%w: observation is outside authoritative endpoint scope", ErrExternalContactMismatch)
	}
	if current.NormalizedValueDigest == observation.NormalizedValueDigest {
		return ContactReconciliation{Status: ContactReconciled}, nil
	}
	return ContactReconciliation{Status: ContactRepairRequired, Repair: &ContactRepair{Subject: current.Subject, EndpointID: current.EndpointID, Purpose: current.Purpose, ExpectedValueDigest: current.NormalizedValueDigest, ExpectedRevisionDigest: current.CanonicalDigest, ObservedValueDigest: observation.NormalizedValueDigest, ObservationSource: observation.Source}}, nil
}
