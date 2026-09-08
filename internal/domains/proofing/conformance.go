package proofing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type ProviderState string

const (
	ProviderVerified  ProviderState = "VERIFIED"
	ProviderReview    ProviderState = "REVIEW_REQUIRED"
	ProviderRejected  ProviderState = "REJECTED"
	ProviderExpired   ProviderState = "EXPIRED"
	ProviderUnknown   ProviderState = "UNKNOWN"
	ProviderAmbiguous ProviderState = "AMBIGUOUS"
)

func (s ProviderState) Valid() bool {
	switch s {
	case ProviderVerified, ProviderReview, ProviderRejected, ProviderExpired, ProviderUnknown, ProviderAmbiguous:
		return true
	}
	return false
}

var (
	ErrConformanceRejected       = errors.New("proofing: work authorization conformance rejected")
	ErrAuthorizationNotEffective = errors.New("proofing: work authorization is not yet effective")
	ErrAuthorizationExpired      = errors.New("proofing: work authorization is expired")
	ErrAuthorizationUnknown      = errors.New("proofing: work authorization state is unknown")
	ErrProviderAmbiguous         = errors.New("proofing: provider observations are ambiguous")
	ErrProviderBinding           = errors.New("proofing: provider observation binding is invalid")
)

// ProviderObservation contains no provider payload. Its canonical digest binds
// tenant, subject, evidence revision, provider identity and provider version.
type ProviderObservation struct {
	Tenant                 values.TenantId
	Subject                values.EntityRef
	ProviderRef            string
	Version                string
	State                  ProviderState
	EvidenceDigest         string
	EvidenceRevisionDigest string
	CanonicalDigest        string
}

func NewProviderObservation(o ProviderObservation) (ProviderObservation, error) {
	o.CanonicalDigest = o.computedDigest()
	if err := o.Validate(); err != nil {
		return ProviderObservation{}, err
	}
	return o, nil
}

func (o ProviderObservation) Validate() error {
	if err := o.Tenant.Validate(); err != nil || o.Subject.Validate() != nil || o.Subject.Tenant != o.Tenant {
		return fmt.Errorf("%w: tenant and subject", ErrProviderBinding)
	}
	if strings.TrimSpace(o.ProviderRef) == "" || strings.TrimSpace(o.Version) == "" || !o.State.Valid() || !digestString(o.EvidenceDigest) || !digestString(o.EvidenceRevisionDigest) {
		return fmt.Errorf("%w: incomplete observation", ErrProviderBinding)
	}
	if o.CanonicalDigest == "" || o.CanonicalDigest != o.computedDigest() {
		return fmt.Errorf("%w: canonical digest mismatch", ErrProviderBinding)
	}
	return nil
}

func (o ProviderObservation) body() []byte {
	b, err := canonicalbytes.New("hcmnext.domains.proofing.ProviderObservation", schemaVersion).
		String("tenant", string(o.Tenant)).Value("subject", o.Subject).String("provider_ref", o.ProviderRef).
		String("version", o.Version).String("state", string(o.State)).String("evidence_digest", o.EvidenceDigest).
		String("evidence_revision_digest", o.EvidenceRevisionDigest).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (o ProviderObservation) computedDigest() string { return canonicalbytes.Digest(o.body()) }

type WorkAuthorizationConformanceRequest struct {
	Evidence     WorkAuthorizationEvidence
	Observations []ProviderObservation
	AsOf         values.LocalDate
	ResultID     string
}

// WorkAuthorizationConformance retains the exact provider receipt bindings
// used to derive its embedded result.
type WorkAuthorizationConformance struct {
	WorkAuthorizationResult
	ProviderState          ProviderState
	ProviderBindingDigests []string
}

func ConformWorkAuthorization(req WorkAuthorizationConformanceRequest) (WorkAuthorizationConformance, error) {
	if err := req.Evidence.Validate(); err != nil {
		return WorkAuthorizationConformance{}, err
	}
	if err := req.AsOf.Validate(); err != nil {
		return WorkAuthorizationConformance{}, fmt.Errorf("%w: as-of date", ErrConformanceRejected)
	}
	state, bindings, err := reconcileProviderObservations(req.Evidence, req.Observations)
	if err != nil && !errors.Is(err, ErrProviderAmbiguous) {
		return WorkAuthorizationConformance{}, fmt.Errorf("%w: %w", ErrConformanceRejected, err)
	}
	decision, cause := DecisionAuthorized, err
	switch {
	case state == ProviderAmbiguous:
		decision, cause = DecisionUnknown, ErrProviderAmbiguous
	case req.AsOf.Compare(req.Evidence.ValidFrom) < 0:
		decision, cause = DecisionUnknown, ErrAuthorizationNotEffective
	case req.AsOf.Compare(req.Evidence.ValidUntil) >= 0 || state == ProviderExpired:
		decision, cause = DecisionExpired, ErrAuthorizationExpired
	case state == ProviderUnknown:
		decision, cause = DecisionUnknown, ErrAuthorizationUnknown
	case state == ProviderRejected:
		decision = DecisionRejected
	case state == ProviderReview || req.AsOf.Compare(req.Evidence.ReverificationDue) >= 0:
		decision = DecisionReview
	}
	result, resultErr := NewWorkAuthorizationResult(WorkAuthorizationResult{ResultID: req.ResultID, Subject: req.Evidence.Subject, EvidenceRevisionDigest: req.Evidence.CanonicalDigest, Jurisdiction: req.Evidence.Jurisdiction, Category: req.Evidence.Category, EffectiveFrom: req.Evidence.ValidFrom, EffectiveUntil: req.Evidence.ValidUntil, Decision: decision})
	if resultErr != nil {
		return WorkAuthorizationConformance{}, resultErr
	}
	out := WorkAuthorizationConformance{WorkAuthorizationResult: result, ProviderState: state, ProviderBindingDigests: bindings}
	if cause != nil {
		return out, fmt.Errorf("%w: %w", ErrConformanceRejected, cause)
	}
	return out, nil
}

func reconcileProviderObservations(e WorkAuthorizationEvidence, observations []ProviderObservation) (ProviderState, []string, error) {
	if len(observations) == 0 {
		return ProviderUnknown, nil, nil
	}
	state := observations[0].State
	bindings := make([]string, 0, len(observations))
	for _, o := range observations {
		if err := o.Validate(); err != nil {
			return ProviderUnknown, nil, err
		}
		if o.Tenant != e.Subject.Tenant || o.Subject != e.Subject || o.EvidenceDigest != e.EvidenceDigest || o.EvidenceRevisionDigest != e.CanonicalDigest {
			return ProviderUnknown, nil, ErrProviderBinding
		}
		bindings = append(bindings, o.CanonicalDigest)
		if o.State != state || o.State == ProviderAmbiguous {
			state = ProviderAmbiguous
		}
	}
	if state == ProviderAmbiguous {
		return state, bindings, ErrProviderAmbiguous
	}
	return state, bindings, nil
}

// Renew appends a revision and requires both fresh protected evidence and a
// fresh source receipt. The receiver is never modified.
func (e WorkAuthorizationEvidence) Renew(validFrom, validUntil, due values.LocalDate, evidenceDigest, sourceRef string) (WorkAuthorizationEvidence, error) {
	if evidenceDigest == e.EvidenceDigest || sourceRef == e.SourceRef {
		return WorkAuthorizationEvidence{}, fmt.Errorf("%w: renewal requires fresh evidence and source", ErrRevisionLineage)
	}
	return e.Successor(WorkAuthorizationEvidence{EvidenceID: e.EvidenceID, Subject: e.Subject, Revision: e.Revision + 1, SupersedesRevision: e.Revision, DocumentClass: e.DocumentClass, VerificationMethod: e.VerificationMethod, Jurisdiction: e.Jurisdiction, Category: e.Category, ValidFrom: validFrom, ValidUntil: validUntil, ReverificationDue: due, EvidenceDigest: evidenceDigest, SourceRef: sourceRef})
}
