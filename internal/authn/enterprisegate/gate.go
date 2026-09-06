// Package enterprisegate owns the decision and evidence gate for optional
// enterprise SAML and SCIM support. It records whether a customer-backed path
// is approved without implementing either protocol or moving identity
// authority into this package.
package enterprisegate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

var (
	ErrInvalidRecord  = errors.New("enterprisegate: invalid decision record")
	ErrDigestMismatch = errors.New("enterprisegate: decision digest mismatch")
)

type Protocol string

const (
	ProtocolSAML Protocol = "SAML"
	ProtocolSCIM Protocol = "SCIM"
)

type Outcome string

const (
	OutcomePending     Outcome = "PENDING"
	OutcomeApproved    Outcome = "APPROVED"
	OutcomeRejected    Outcome = "REJECTED"
	OutcomeNotRequired Outcome = "NOT_REQUIRED"
)

type EvidenceKind string

const (
	EvidenceRequirement     EvidenceKind = "CUSTOMER_REQUIREMENT"
	EvidenceThreatTest      EvidenceKind = "THREAT_TEST"
	EvidenceOperabilityTest EvidenceKind = "OPERABILITY_TEST"
	EvidenceMetadata        EvidenceKind = "METADATA_CONFORMANCE"
	EvidenceSignature       EvidenceKind = "SIGNATURE_CONFORMANCE"
	EvidenceDeprovision     EvidenceKind = "DEPROVISION_CONFORMANCE"
)

// EvidenceRef contains references and digests only. Customer assertions and
// test payloads stay in their governed evidence store, outside this record.
type EvidenceRef struct {
	ID          string       `json:"id"`
	TenantID    string       `json:"tenant_id"`
	Kind        EvidenceKind `json:"kind"`
	SourceRef   string       `json:"source_ref"`
	Digest      string       `json:"digest"`
	CollectedAt time.Time    `json:"collected_at"`
	ExpiresAt   time.Time    `json:"expires_at"`
}

type ConformanceEvidence struct {
	MetadataEvidenceID    string `json:"metadata_evidence_id,omitempty"`
	SignatureEvidenceID   string `json:"signature_evidence_id,omitempty"`
	DeprovisionEvidenceID string `json:"deprovision_evidence_id,omitempty"`
}

type ProtocolPath struct {
	Protocol              Protocol            `json:"protocol"`
	Outcome               Outcome             `json:"outcome"`
	Reason                string              `json:"reason,omitempty"`
	Owner                 string              `json:"owner"`
	RequirementEvidenceID string              `json:"requirement_evidence_id,omitempty"`
	ThreatEvidenceID      string              `json:"threat_evidence_id,omitempty"`
	OperabilityEvidenceID string              `json:"operability_evidence_id,omitempty"`
	Conformance           ConformanceEvidence `json:"conformance"`
}

// Record is the immutable, customer-evidence-bound decision input for
// AUTHN-008. Digest is optional while a record is being drafted and is never
// included in the bytes it authenticates.
type Record struct {
	SchemaVersion      int            `json:"schema_version"`
	DecisionID         string         `json:"decision_id"`
	TenantID           string         `json:"tenant_id"`
	PartnerManifestRef string         `json:"partner_manifest_ref"`
	Evidence           []EvidenceRef  `json:"evidence"`
	Paths              []ProtocolPath `json:"paths"`
	DecisionOwner      string         `json:"decision_owner"`
	DecisionMaker      string         `json:"decision_maker"`
	DecisionSignature  string         `json:"decision_signature"`
	DecidedAt          time.Time      `json:"decided_at"`
	Placeholder        bool           `json:"placeholder,omitempty"`
	Digest             string         `json:"digest,omitempty"`
}

type GateStatus string

const (
	GatePending     GateStatus = "PENDING_HUMAN_DECISION"
	GateBlocked     GateStatus = "BLOCKED"
	GateApproved    GateStatus = "APPROVED"
	GateNotRequired GateStatus = "NO_SUPPORT_REQUIRED"
)

type Decision struct {
	Status              GateStatus `json:"status"`
	Digest              string     `json:"digest"`
	Protocols           []Protocol `json:"protocols"`
	Missing             []string   `json:"missing,omitempty"`
	HumanInputsRequired []string   `json:"human_inputs_required,omitempty"`
	Explanation         string     `json:"explanation"`
}

type SchemaDescriptor struct {
	Name           string
	Version        int
	RequiredFields []string
	Protocols      []Protocol
	EvidenceKinds  []EvidenceKind
}

func Version() int { return SchemaVersion }

func Schema() SchemaDescriptor {
	return SchemaDescriptor{
		Name:           "AUTHN-008 enterprise federation support decision",
		Version:        SchemaVersion,
		RequiredFields: []string{"schema_version", "decision_id", "tenant_id", "partner_manifest_ref", "evidence", "paths"},
		Protocols:      []Protocol{ProtocolSAML, ProtocolSCIM},
		EvidenceKinds:  []EvidenceKind{EvidenceRequirement, EvidenceThreatTest, EvidenceOperabilityTest, EvidenceMetadata, EvidenceSignature, EvidenceDeprovision},
	}
}

func (r Record) ValidateAt(now time.Time) error {
	if r.SchemaVersion != SchemaVersion || !identifier(r.DecisionID) || !identifier(r.TenantID) || !identifier(r.PartnerManifestRef) || !strings.HasPrefix(r.PartnerManifestRef, "partner-manifest:") {
		return fmt.Errorf("%w: record identity", ErrInvalidRecord)
	}
	if r.Digest != "" && r.Digest != Digest(r) {
		return ErrDigestMismatch
	}
	if now.IsZero() {
		return fmt.Errorf("%w: evaluation time is required", ErrInvalidRecord)
	}
	if len(r.Evidence) == 0 || len(r.Paths) != 2 {
		return fmt.Errorf("%w: exactly SAML and SCIM paths plus evidence are required", ErrInvalidRecord)
	}
	seenEvidence := make(map[string]bool, len(r.Evidence))
	for _, evidence := range r.Evidence {
		if !identifier(evidence.ID) || seenEvidence[evidence.ID] || evidence.TenantID != r.TenantID || !validEvidenceKind(evidence.Kind) || strings.TrimSpace(evidence.SourceRef) == "" || !strings.HasPrefix(evidence.Digest, "sha256:") || evidence.CollectedAt.IsZero() || evidence.ExpiresAt.IsZero() || !evidence.ExpiresAt.After(evidence.CollectedAt) {
			return fmt.Errorf("%w: evidence %q", ErrInvalidRecord, evidence.ID)
		}
		seenEvidence[evidence.ID] = true
	}
	seenProtocols := make(map[Protocol]bool, 2)
	for _, path := range r.Paths {
		if !validProtocol(path.Protocol) || seenProtocols[path.Protocol] || !identifier(path.Owner) || !validOutcome(path.Outcome) {
			return fmt.Errorf("%w: protocol path %q", ErrInvalidRecord, path.Protocol)
		}
		if path.Outcome != OutcomePending && strings.TrimSpace(path.Reason) == "" {
			return fmt.Errorf("%w: protocol path %q needs a decision reason", ErrInvalidRecord, path.Protocol)
		}
		seenProtocols[path.Protocol] = true
		for _, ref := range []string{path.RequirementEvidenceID, path.ThreatEvidenceID, path.OperabilityEvidenceID, path.Conformance.MetadataEvidenceID, path.Conformance.SignatureEvidenceID, path.Conformance.DeprovisionEvidenceID} {
			if ref != "" && !identifier(ref) {
				return fmt.Errorf("%w: evidence reference %q", ErrInvalidRecord, ref)
			}
		}
	}
	if !seenProtocols[ProtocolSAML] || !seenProtocols[ProtocolSCIM] {
		return fmt.Errorf("%w: both SAML and SCIM paths are required", ErrInvalidRecord)
	}
	if r.DecidedAt.IsZero() != (r.DecisionOwner == "" && r.DecisionMaker == "" && r.DecisionSignature == "") {
		return fmt.Errorf("%w: final decision metadata must be complete or absent", ErrInvalidRecord)
	}
	if r.DecidedAt.IsZero() {
		return nil
	}
	if !identifier(r.DecisionOwner) || !identifier(r.DecisionMaker) || r.DecisionOwner == r.DecisionMaker || strings.TrimSpace(r.DecisionSignature) == "" || r.DecidedAt.After(now) {
		return fmt.Errorf("%w: decision ownership and signing", ErrInvalidRecord)
	}
	return nil
}

func (r Record) Validate() error { return r.ValidateAt(time.Now().UTC()) }

func EvaluateAt(r Record, now time.Time) (Decision, error) {
	if err := r.ValidateAt(now); err != nil {
		return Decision{}, err
	}
	digest := Digest(r)
	result := Decision{Digest: digest}
	allFinal := true
	anyApproved := false
	for _, path := range r.Paths {
		result.Protocols = append(result.Protocols, path.Protocol)
		switch path.Outcome {
		case OutcomePending:
			allFinal = false
			result.Missing = append(result.Missing, string(path.Protocol)+" decision outcome")
		case OutcomeApproved:
			anyApproved = true
			checkApprovedPath(r, path, now, &result)
		case OutcomeRejected, OutcomeNotRequired:
			if !evidencePresent(r, path.RequirementEvidenceID, EvidenceRequirement, now) {
				result.Missing = append(result.Missing, string(path.Protocol)+" customer requirement evidence")
			}
		}
	}
	sort.Slice(result.Protocols, func(i, j int) bool { return result.Protocols[i] < result.Protocols[j] })
	sort.Strings(result.Missing)
	if r.Placeholder {
		result.HumanInputsRequired = []string{"design-partner requirement and manifest reference", "SAML/SCIM outcome per protocol", "named decision owner and approver", "fresh threat and operability evidence", "metadata, signature and deprovision conformance evidence for any approved path"}
	}
	if !allFinal {
		result.Status = GatePending
	} else if len(result.Missing) > 0 || r.Placeholder {
		result.Status = GateBlocked
	} else if anyApproved {
		result.Status = GateApproved
	} else {
		result.Status = GateNotRequired
	}
	result.Explanation = explain(result)
	return result, nil
}

func Evaluate(r Record) (Decision, error) { return EvaluateAt(r, time.Now().UTC()) }

func Explain(r Record, now time.Time) (Decision, error) { return EvaluateAt(r, now) }

func (r Record) Explain(now time.Time) (Decision, error) { return Explain(r, now) }

func checkApprovedPath(r Record, path ProtocolPath, now time.Time, result *Decision) {
	checks := []struct {
		ref  string
		kind EvidenceKind
		name string
	}{
		{path.RequirementEvidenceID, EvidenceRequirement, string(path.Protocol) + " customer requirement evidence"},
		{path.ThreatEvidenceID, EvidenceThreatTest, string(path.Protocol) + " threat-test evidence"},
		{path.OperabilityEvidenceID, EvidenceOperabilityTest, string(path.Protocol) + " operability-test evidence"},
		{path.Conformance.MetadataEvidenceID, EvidenceMetadata, string(path.Protocol) + " metadata conformance evidence"},
		{path.Conformance.SignatureEvidenceID, EvidenceSignature, string(path.Protocol) + " signature conformance evidence"},
		{path.Conformance.DeprovisionEvidenceID, EvidenceDeprovision, string(path.Protocol) + " deprovision conformance evidence"},
	}
	for _, check := range checks {
		if !evidencePresent(r, check.ref, check.kind, now) {
			result.Missing = append(result.Missing, check.name)
		}
	}
}

func evidencePresent(r Record, id string, kind EvidenceKind, now time.Time) bool {
	if id == "" {
		return false
	}
	for _, evidence := range r.Evidence {
		if evidence.ID == id {
			return evidence.TenantID == r.TenantID && evidence.Kind == kind && now.Before(evidence.ExpiresAt)
		}
	}
	return false
}

func explain(result Decision) string {
	switch result.Status {
	case GateApproved:
		return "customer-evidenced enterprise support path is approved"
	case GateNotRequired:
		return "customer evidence records that SAML and SCIM support are not required"
	case GatePending:
		return "human protocol decision is still required"
	default:
		return "support remains blocked until the recorded evidence and human decision are complete"
	}
}

func Digest(r Record) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (r Record) VerifyDigest() error {
	if strings.TrimSpace(r.Digest) == "" || r.Digest != Digest(r) {
		return ErrDigestMismatch
	}
	return nil
}

func PlaceholderRecord() Record {
	return Record{
		SchemaVersion: SchemaVersion, DecisionID: "authn-008:PLACEHOLDER", TenantID: "tenant:PLACEHOLDER", PartnerManifestRef: "partner-manifest:PLACEHOLDER", Placeholder: true,
		Evidence: []EvidenceRef{{ID: "evidence:PLACEHOLDER", TenantID: "tenant:PLACEHOLDER", Kind: EvidenceRequirement, SourceRef: "fixture:PLACEHOLDER_CUSTOMER_EVIDENCE", Digest: "sha256:PLACEHOLDER", CollectedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}},
		Paths:    []ProtocolPath{{Protocol: ProtocolSAML, Outcome: OutcomePending, Owner: "owner:PLACEHOLDER"}, {Protocol: ProtocolSCIM, Outcome: OutcomePending, Owner: "owner:PLACEHOLDER"}},
	}
}

func identifier(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r > 127 || !(r == '-' || r == '_' || r == ':' || r == '/' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func validProtocol(protocol Protocol) bool {
	return protocol == ProtocolSAML || protocol == ProtocolSCIM
}

func validOutcome(outcome Outcome) bool {
	return outcome == OutcomePending || outcome == OutcomeApproved || outcome == OutcomeRejected || outcome == OutcomeNotRequired
}

func validEvidenceKind(kind EvidenceKind) bool {
	for _, allowed := range Schema().EvidenceKinds {
		if kind == allowed {
			return true
		}
	}
	return false
}
