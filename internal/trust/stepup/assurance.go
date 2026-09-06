package stepup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/hcm-next/internal/trust"
)

// EvidenceKind is the closed vocabulary of credential evidence understood by
// the assurance table. The value is evidence metadata, never a credential.
type EvidenceKind string

const (
	EvidencePasskey      EvidenceKind = "PASSKEY"
	EvidenceHardwareKey  EvidenceKind = "HARDWARE_KEY"
	EvidenceTOTP         EvidenceKind = "TOTP"
	EvidenceSMSOTP       EvidenceKind = "SMS_OTP"
	EvidenceRecoveryCode EvidenceKind = "RECOVERY_CODE"
	EvidencePassword     EvidenceKind = "PASSWORD"

	// Descriptive aliases keep callers close to the vocabulary in the
	// security control while retaining one canonical value on the wire.
	CredentialPasskey      = EvidencePasskey
	CredentialHardwareKey  = EvidenceHardwareKey
	CredentialTOTP         = EvidenceTOTP
	CredentialSMSOTP       = EvidenceSMSOTP
	CredentialRecoveryCode = EvidenceRecoveryCode
	CredentialPassword     = EvidencePassword
)

func (k EvidenceKind) valid() bool {
	switch k {
	case EvidencePasskey, EvidenceHardwareKey, EvidenceTOTP, EvidenceSMSOTP,
		EvidenceRecoveryCode, EvidencePassword:
		return true
	default:
		return false
	}
}

// IAL, AAL and FAL are the NIST SP 800-63-4 assurance dimensions.
type IAL uint8
type AAL uint8
type FAL uint8

const (
	IALUnspecified IAL = iota
	IAL1
	IAL2
	IAL3
)

const (
	AALUnspecified AAL = iota
	AAL1
	AAL2
	AAL3
)

const (
	FALUnspecified FAL = iota
	FAL1
	FAL2
	FAL3
)

func (v IAL) valid() bool { return v >= IAL1 && v <= IAL3 }
func (v AAL) valid() bool { return v >= AAL1 && v <= AAL3 }
func (v FAL) valid() bool { return v >= FAL1 && v <= FAL3 }

// AssuranceTier is the three-dimensional assurance floor used by a tenant or
// operation. All dimensions must meet the floor; no dimension is substituted
// for another.
type AssuranceTier struct {
	IAL IAL
	AAL AAL
	FAL FAL
}

func (t AssuranceTier) valid() bool { return t.IAL.valid() && t.AAL.valid() && t.FAL.valid() }

func (t AssuranceTier) atLeast(required AssuranceTier) bool {
	return t.valid() && required.valid() && t.IAL >= required.IAL && t.AAL >= required.AAL && t.FAL >= required.FAL
}

// Valid reports whether every assurance dimension is a concrete NIST tier.
func (t AssuranceTier) Valid() bool { return t.valid() }

// AtLeast reports whether every dimension meets required.
func (t AssuranceTier) AtLeast(required AssuranceTier) bool { return t.atLeast(required) }

// AssuranceMapping maps one evidence kind to a NIST assurance tier.
type AssuranceMapping struct {
	Kind         EvidenceKind
	EvidenceKind EvidenceKind
	IAL          IAL
	AAL          AAL
	FAL          FAL
}

// CredentialAssuranceMapping is a readable alias for AssuranceMapping.
type CredentialAssuranceMapping = AssuranceMapping

func (m AssuranceMapping) validate() error {
	if m.Kind == "" {
		m.Kind = m.EvidenceKind
	}
	if m.EvidenceKind != "" && m.Kind != m.EvidenceKind {
		return fmt.Errorf("stepup: assurance mapping field kind is inconsistent")
	}
	if !m.Kind.valid() {
		return fmt.Errorf("stepup: assurance mapping field kind is invalid: %q", m.Kind)
	}
	if !m.IAL.valid() {
		return fmt.Errorf("stepup: assurance mapping field ial is invalid")
	}
	if !m.AAL.valid() {
		return fmt.Errorf("stepup: assurance mapping field aal is invalid")
	}
	if !m.FAL.valid() {
		return fmt.Errorf("stepup: assurance mapping field fal is invalid")
	}
	return nil
}

func (m AssuranceMapping) canonicalKind() EvidenceKind {
	if m.Kind != "" {
		return m.Kind
	}
	return m.EvidenceKind
}

// CredentialAssuranceTable is an immutable, versioned mapping from evidence
// kind to IAL/AAL/FAL. Its digest identifies the exact table used in a
// decision and is safe to place in an audit record.
type CredentialAssuranceTable struct {
	Version  int
	Mappings []AssuranceMapping
	Digest   string
}

// NewCredentialAssuranceTable validates and freezes a versioned mapping.
func NewCredentialAssuranceTable(version int, mappings []AssuranceMapping) (*CredentialAssuranceTable, error) {
	if version <= 0 {
		return nil, fmt.Errorf("stepup: assurance table field version must be positive")
	}
	if len(mappings) == 0 {
		return nil, fmt.Errorf("stepup: assurance table field mappings is empty")
	}
	out := append([]AssuranceMapping(nil), mappings...)
	seen := make(map[EvidenceKind]bool, len(out))
	for i, m := range out {
		if err := m.validate(); err != nil {
			return nil, err
		}
		kind := m.canonicalKind()
		m.Kind, m.EvidenceKind = kind, kind
		out[i] = m
		if seen[kind] {
			return nil, fmt.Errorf("stepup: assurance table field kind is duplicated: %q", kind)
		}
		seen[m.Kind] = true
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	h := sha256.New()
	fmt.Fprintf(h, "version=%d;", version)
	for _, m := range out {
		fmt.Fprintf(h, "kind=%d:%s;ial=%d;aal=%d;fal=%d;", len(m.Kind), m.Kind, m.IAL, m.AAL, m.FAL)
	}
	return &CredentialAssuranceTable{Version: version, Mappings: out, Digest: "ca:" + hex.EncodeToString(h.Sum(nil))}, nil
}

// DefaultCredentialAssuranceTable is the conservative v1 baseline. A
// credential proves authentication assurance only; it does not silently
// upgrade identity proofing or federation assurance.
func DefaultCredentialAssuranceTable() *CredentialAssuranceTable {
	t, err := NewCredentialAssuranceTable(1, []AssuranceMapping{
		{Kind: EvidencePasskey, IAL: IAL2, AAL: AAL2, FAL: FAL2},
		{Kind: EvidenceHardwareKey, IAL: IAL2, AAL: AAL3, FAL: FAL2},
		{Kind: EvidenceTOTP, IAL: IAL1, AAL: AAL2, FAL: FAL1},
		{Kind: EvidenceSMSOTP, IAL: IAL1, AAL: AAL1, FAL: FAL1},
		{Kind: EvidenceRecoveryCode, IAL: IAL1, AAL: AAL1, FAL: FAL1},
		{Kind: EvidencePassword, IAL: IAL1, AAL: AAL1, FAL: FAL1},
	})
	if err != nil {
		panic("stepup: default credential assurance table is invalid: " + err.Error())
	}
	return t
}

// Lookup resolves one evidence kind without exposing credential material.
func (t CredentialAssuranceTable) Lookup(kind EvidenceKind) (AssuranceTier, bool) {
	for _, m := range t.Mappings {
		if m.Kind == kind {
			return AssuranceTier{IAL: m.IAL, AAL: m.AAL, FAL: m.FAL}, true
		}
	}
	return AssuranceTier{}, false
}

// CredentialEvidence describes the verified evidence used for a mapping
// decision. A recovery flag is not enough to authorize a sensitive operation:
// a non-empty NewProofDigest is required after recovery.
type CredentialEvidence struct {
	Kind           EvidenceKind
	Recovered      bool
	NewProofDigest string
}

// MappingDecision is the immutable, digest-backed audit fact for one table
// lookup and comparison. It contains no subject, account number, issuer URL,
// or raw credential.
type MappingDecision struct {
	TableVersion int
	TableDigest  string
	EvidenceKind EvidenceKind
	Mapped       AssuranceTier
	Required     AssuranceTier
	Accepted     bool
	Reason       string
	Digest       string
	EvidenceID   string
}

const (
	ReasonAssuranceMapped        = "assurance_mapped"
	ReasonAssuranceInsufficient  = "assurance_insufficient"
	ReasonUnknownEvidenceKind    = "evidence_kind_unknown"
	ReasonRecoveryProofRequired  = "recovery_new_proof_required"
	ReasonAssurancePolicyMissing = "assurance_policy_missing"
)

// Explain returns an audit-safe summary without identifiers or secrets.
func (d MappingDecision) Explain() string {
	return fmt.Sprintf("credential assurance table=%d evidence_kind=%s accepted=%t reason=%s", d.TableVersion, d.EvidenceKind, d.Accepted, d.Reason)
}

// AssuranceEvidenceSink is the I/O port for durable mapping evidence.
type AssuranceEvidenceSink interface {
	RecordAssuranceDecision(MappingDecision) error
}

// MemoryAssuranceEvidenceStore is a concurrency-safe test adapter for the
// durable evidence port. Production adapters can persist the same value.
type MemoryAssuranceEvidenceStore struct {
	mu        sync.Mutex
	decisions []MappingDecision
}

// NewMemoryAssuranceEvidenceStore returns an empty evidence adapter.
func NewMemoryAssuranceEvidenceStore() *MemoryAssuranceEvidenceStore {
	return &MemoryAssuranceEvidenceStore{}
}

// RecordAssuranceDecision appends one immutable decision.
func (s *MemoryAssuranceEvidenceStore) RecordAssuranceDecision(d MappingDecision) error {
	if s == nil || d.EvidenceID == "" || d.Digest == "" {
		return errors.New("stepup: assurance evidence field decision is incomplete")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.decisions = append(s.decisions, d)
	return nil
}

// Decisions returns a copy of all recorded decisions, oldest first.
func (s *MemoryAssuranceEvidenceStore) Decisions() []MappingDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MappingDecision(nil), s.decisions...)
}

func requiredTier(a trust.Assurance) AssuranceTier {
	switch a {
	case trust.AssuranceLow:
		return AssuranceTier{IAL: IAL1, AAL: AAL1, FAL: FAL1}
	case trust.AssuranceSubstantial:
		return AssuranceTier{IAL: IAL1, AAL: AAL2, FAL: FAL1}
	case trust.AssuranceHigh:
		return AssuranceTier{IAL: IAL2, AAL: AAL3, FAL: FAL2}
	default:
		return AssuranceTier{}
	}
}

func makeMappingDecision(table CredentialAssuranceTable, evidence CredentialEvidence, required AssuranceTier) MappingDecision {
	d := MappingDecision{TableVersion: table.Version, TableDigest: table.Digest, EvidenceKind: evidence.Kind, Required: required}
	mapped, found := table.Lookup(evidence.Kind)
	d.Mapped = mapped
	switch {
	case !found:
		d.Reason = ReasonUnknownEvidenceKind
	case evidence.Recovered && strings.TrimSpace(evidence.NewProofDigest) == "":
		d.Reason = ReasonRecoveryProofRequired
	case !mapped.atLeast(required):
		d.Reason = ReasonAssuranceInsufficient
	default:
		d.Accepted = true
		d.Reason = ReasonAssuranceMapped
	}
	h := sha256.New()
	fmt.Fprintf(h, "table=%d:%s;kind=%d:%s;mapped=%d/%d/%d;required=%d/%d/%d;accepted=%t;reason=%s;recovered=%t;proof=%d:%s;",
		table.Version, table.Digest, len(evidence.Kind), evidence.Kind,
		mapped.IAL, mapped.AAL, mapped.FAL, required.IAL, required.AAL, required.FAL,
		d.Accepted, d.Reason, evidence.Recovered, len(evidence.NewProofDigest), evidence.NewProofDigest)
	d.Digest = hex.EncodeToString(h.Sum(nil))
	d.EvidenceID = "ev:assurance:" + d.Digest[:32]
	return d
}

// NewAssuranceObligationPolicy builds the existing obligation policy with a
// credential-assurance table and mandatory durable-decision port.
func NewAssuranceObligationPolicy(table *CredentialAssuranceTable, evidence AssuranceEvidenceSink, rules ...ObligationRule) (*ObligationPolicy, error) {
	if table == nil {
		return nil, errors.New("stepup: assurance policy field table is required")
	}
	if evidence == nil {
		return nil, errors.New("stepup: assurance policy field evidence sink is required")
	}
	p, err := NewObligationPolicy(rules...)
	if err != nil {
		return nil, err
	}
	p.assuranceTable = table
	p.assuranceEvidence = evidence
	return p, nil
}

// ResolveAssurance resolves and durably records the evidence mapping for the
// strongest matching obligation. A missing policy or malformed operation is
// refused with a typed, field-naming error.
func (p *ObligationPolicy) ResolveAssurance(capability, purpose string, risk Risk, evidence CredentialEvidence) (MappingDecision, error) {
	if p == nil || p.assuranceTable == nil {
		return MappingDecision{Reason: ReasonAssurancePolicyMissing}, errors.New("stepup: assurance policy field table is missing")
	}
	requirement, _, needed := p.Select(capability, purpose, risk)
	if !needed {
		requirement.MinAssurance = trust.AssuranceLow
	}
	d := makeMappingDecision(*p.assuranceTable, evidence, requiredTier(requirement.MinAssurance))
	if err := p.assuranceEvidence.RecordAssuranceDecision(d); err != nil {
		return d, fmt.Errorf("stepup: record assurance decision: %w", err)
	}
	return d, nil
}

// EvaluateObligationWithAssurance performs the ordinary pure obligation
// evaluation and adds the versioned credential mapping and recovery guard.
func EvaluateObligationWithAssurance(pol *ObligationPolicy, req ObligationRequest, p *trust.Principal, proof *Proof, evidence CredentialEvidence) Obligation {
	ob := EvaluateObligation(pol, req, p, proof)
	if pol == nil || pol.assuranceTable == nil {
		return ob
	}
	d, err := pol.ResolveAssurance(req.Operation.Capability, req.Operation.Purpose, req.Operation.Risk, evidence)
	if err != nil || !d.Accepted {
		ob.Code, ob.Required, ob.Satisfied = CodeStepUpRequired, true, false
		ob.Reason = d.Reason
	}
	return ob
}

// Explain describes the assurance-aware step-up contract without values that
// could identify a subject or protected record.
func Explain() string {
	return "stepup: versioned credential evidence maps to NIST IAL/AAL/FAL floors; sensitive obligations refuse insufficient or freshly recovered assurance without a new proof and record digested decisions"
}
