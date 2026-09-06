package issuerregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/stepup"
)

// AssuranceContract is the immutable assurance floor attached to one Issuer
// revision. TableVersion identifies the stepup credential-assurance table
// against which the floor was published.
type AssuranceContract struct {
	TableVersion int
	IAL          stepup.IAL
	AAL          stepup.AAL
	FAL          stepup.FAL
	RequiredIAL  stepup.IAL
	RequiredAAL  stepup.AAL
	RequiredFAL  stepup.FAL
}

func (c AssuranceContract) tier() stepup.AssuranceTier {
	ial, aal, fal := c.IAL, c.AAL, c.FAL
	if ial == stepup.IALUnspecified {
		ial = c.RequiredIAL
	}
	if aal == stepup.AALUnspecified {
		aal = c.RequiredAAL
	}
	if fal == stepup.FALUnspecified {
		fal = c.RequiredFAL
	}
	return stepup.AssuranceTier{IAL: ial, AAL: aal, FAL: fal}
}

// Configured reports whether a revision carries a government assurance
// contract rather than the zero value used by ordinary tenants.
func (c AssuranceContract) Configured() bool {
	return c.TableVersion > 0 || c.tier().Valid()
}

func (c AssuranceContract) validate() error {
	if !c.Configured() {
		return nil
	}
	if c.TableVersion <= 0 {
		return fmt.Errorf("%w: assurance contract field table_version must be positive", ErrInvalidIssuer)
	}
	if !c.tier().Valid() {
		return fmt.Errorf("%w: assurance contract field ial/aal/fal is invalid", ErrInvalidIssuer)
	}
	return nil
}

// ResolveAssurance validates that this revision's immutable floor is resolved
// against the exact versioned table it names.
func (i Issuer) ResolveAssurance(table *stepup.CredentialAssuranceTable) (stepup.AssuranceTier, error) {
	if err := i.AssuranceContract.validate(); err != nil {
		return stepup.AssuranceTier{}, err
	}
	if !i.AssuranceContract.Configured() {
		return stepup.AssuranceTier{}, fmt.Errorf("%w: assurance contract field is missing", ErrAssuranceContract)
	}
	if table == nil {
		return stepup.AssuranceTier{}, fmt.Errorf("%w: assurance table field is missing", ErrAssuranceContract)
	}
	if table.Version != i.AssuranceContract.TableVersion {
		return stepup.AssuranceTier{}, fmt.Errorf("%w: assurance table field version %d does not match contract version %d", ErrAssuranceContract, table.Version, i.AssuranceContract.TableVersion)
	}
	return i.AssuranceContract.tier(), nil
}

// AssuranceContractTier returns the contract's value without resolving it.
// Callers enforcing a revision should use [Issuer.ResolveAssurance] first.
func (i Issuer) AssuranceContractTier() stepup.AssuranceTier {
	return i.AssuranceContract.tier()
}

var (
	// ErrAssuranceContract is returned when a government assurance contract
	// is absent, malformed, or resolved against the wrong table revision.
	ErrAssuranceContract = errors.New("issuerregistry: assurance contract is invalid")
	ErrAccessRevoked     = errors.New("issuerregistry: access is revoked by assurance state")
)

// AccessRequest contains only verified assurance state and policy inputs.
// Deprovisioned represents an external IdP deprovision event; a claim
// downgrade is represented by Presented being below the issuer floor.
type AccessRequest struct {
	Issuer        Issuer
	Table         *stepup.CredentialAssuranceTable
	Presented     stepup.AssuranceTier
	SensitiveWork bool
	Deprovisioned bool
	At            time.Time
}

// AccessDecision is immutable, digest-backed evidence for an assurance
// decision. It never contains a subject, issuer URL, account number, or raw
// claim.
type AccessDecision struct {
	TableVersion           int
	Required               stepup.AssuranceTier
	Presented              stepup.AssuranceTier
	Allowed                bool
	Revoked                bool
	ReauthenticationNeeded bool
	Reason                 string
	At                     time.Time
	Digest                 string
	EvidenceID             string
}

const (
	ReasonAccessAllowed             = "access_allowed"
	ReasonExternalDeprovisioned     = "external_deprovisioned"
	ReasonAssuranceDowngrade        = "assurance_claim_downgrade"
	ReasonSensitiveReauthentication = "sensitive_work_requires_reauthentication"
)

// AccessDecisionSink is the I/O port for durable access-state evidence.
type AccessDecisionSink interface {
	RecordAccessDecision(AccessDecision) error
}

// MemoryAccessEvidenceStore is a concurrency-safe evidence adapter for tests
// and kernel-pure integrations.
type MemoryAccessEvidenceStore struct {
	mu        sync.Mutex
	decisions []AccessDecision
}

// NewMemoryAccessEvidenceStore returns an empty access evidence adapter.
func NewMemoryAccessEvidenceStore() *MemoryAccessEvidenceStore { return &MemoryAccessEvidenceStore{} }

// RecordAccessDecision appends one immutable decision.
func (s *MemoryAccessEvidenceStore) RecordAccessDecision(d AccessDecision) error {
	if s == nil || d.EvidenceID == "" || d.Digest == "" {
		return errors.New("issuerregistry: access evidence field decision is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decisions = append(s.decisions, d)
	return nil
}

// Decisions returns a copy of decisions oldest first.
func (s *MemoryAccessEvidenceStore) Decisions() []AccessDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AccessDecision(nil), s.decisions...)
}

func digestAccessDecision(req AccessRequest, required stepup.AssuranceTier, reason string, allowed, revoked, reauth bool) AccessDecision {
	d := AccessDecision{TableVersion: req.Table.Version, Required: required, Presented: req.Presented, Allowed: allowed, Revoked: revoked, ReauthenticationNeeded: reauth, Reason: reason, At: req.At.UTC()}
	h := sha256.New()
	fmt.Fprintf(h, "table=%d;required=%d/%d/%d;presented=%d/%d/%d;allowed=%t;revoked=%t;reauth=%t;reason=%s;at=%d;",
		d.TableVersion, required.IAL, required.AAL, required.FAL, req.Presented.IAL, req.Presented.AAL, req.Presented.FAL,
		allowed, revoked, reauth, reason, d.At.UnixNano())
	d.Digest = hex.EncodeToString(h.Sum(nil))
	d.EvidenceID = "ev:issuer-assurance:" + d.Digest[:32]
	return d
}

// EvaluateAccess resolves the issuer revision's assurance floor and durably
// records the result. External deprovision always revokes. A claim downgrade
// revokes mapped access, and sensitive work additionally requires a fresh
// authentication event before it can be attempted again.
func EvaluateAccess(req AccessRequest, sink AccessDecisionSink) (AccessDecision, error) {
	if sink == nil {
		return AccessDecision{}, errors.New("issuerregistry: access decision field evidence sink is required")
	}
	if req.At.IsZero() {
		return AccessDecision{}, errors.New("issuerregistry: access request field at is required")
	}
	if req.Table == nil {
		return AccessDecision{}, fmt.Errorf("%w: assurance table field is missing", ErrAssuranceContract)
	}
	required, err := req.Issuer.ResolveAssurance(req.Table)
	if err != nil {
		return AccessDecision{}, err
	}
	reason := ReasonAccessAllowed
	allowed, revoked, reauth := true, false, false
	switch {
	case req.Deprovisioned:
		allowed, revoked, reason = false, true, ReasonExternalDeprovisioned
	case !req.Presented.Valid() || !req.Presented.AtLeast(required):
		allowed, revoked, reason = false, true, ReasonAssuranceDowngrade
		reauth = req.SensitiveWork
		if reauth {
			reason = ReasonSensitiveReauthentication
		}
	}
	d := digestAccessDecision(req, required, reason, allowed, revoked, reauth)
	if err := sink.RecordAccessDecision(d); err != nil {
		return d, fmt.Errorf("issuerregistry: record access decision: %w", err)
	}
	return d, nil
}

// Explain returns a redaction-safe access-policy summary.
func (d AccessDecision) Explain() string {
	return fmt.Sprintf("issuer assurance access allowed=%t revoked=%t reauthentication=%t reason=%s table=%d", d.Allowed, d.Revoked, d.ReauthenticationNeeded, d.Reason, d.TableVersion)
}

// Validate ensures a decision is suitable for durable storage.
func (d AccessDecision) Validate() error {
	if d.TableVersion <= 0 {
		return errors.New("issuerregistry: access decision field table_version is missing")
	}
	if strings.TrimSpace(d.Reason) == "" || d.At.IsZero() || d.Digest == "" || d.EvidenceID == "" {
		return errors.New("issuerregistry: access decision field evidence is incomplete")
	}
	return nil
}
