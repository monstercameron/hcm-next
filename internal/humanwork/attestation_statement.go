package humanwork

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// AttestationStatus is the lifecycle state of a statement version.
type AttestationStatus string

const (
	AttestationDraft     AttestationStatus = "DRAFT"
	AttestationPublished AttestationStatus = "PUBLISHED"
	AttestationRetired   AttestationStatus = "RETIRED"
)

// AttestationPeriod bounds the facts a respondent is asked to attest.
type AttestationPeriod struct{ From, To time.Time }

// AttestationFact identifies a fact (and its version) presented to the
// respondent. Values are deliberately references, not copied business facts.
type AttestationFact struct{ Ref, Version string }

// AttestationResponseOption is one explicitly presented answer.
type AttestationResponseOption struct{ Code, Text string }

// AttestationEvidenceRequirement states evidence that must accompany a
// response. Kind is a domain-owned evidence type; Ref is an optional pinned
// artifact or policy reference.
type AttestationEvidenceRequirement struct {
	Kind, Ref string
	Required  bool
}

// AttestationCorrectionPolicy declares how an incorrect response is corrected.
type AttestationCorrectionPolicy struct {
	Allowed   bool
	PolicyRef string
}

// AttestationRefusalPolicy declares whether refusal is an allowed outcome and
// where its handling policy is defined.
type AttestationRefusalPolicy struct {
	Allowed   bool
	PolicyRef string
}

// AttestationStatement is an immutable, versioned statement definition once
// published. Its digest covers every field, including evidence and policies.
type AttestationStatement struct {
	ID                string
	Version           string
	Status            AttestationStatus
	Text              string
	Locale            string
	SubjectRef        string
	Facts             []AttestationFact
	Period            AttestationPeriod
	Purpose           string
	RequiredAssurance trust.Assurance
	ResponseOptions   []AttestationResponseOption
	Evidence          []AttestationEvidenceRequirement
	CorrectionPolicy  AttestationCorrectionPolicy
	RefusalPolicy     AttestationRefusalPolicy
	Digest            string
}

// Validate checks the publication contract and identifies the offending field.
func (s AttestationStatement) Validate() error {
	bad := func(field, detail string) error {
		return newError("AttestationStatement.Validate", field, ErrInvalidAttestation, "%s", detail)
	}
	if strings.TrimSpace(s.ID) == "" {
		return bad("id", "statement has no id")
	}
	if strings.TrimSpace(s.Version) == "" {
		return bad("version", "statement has no version")
	}
	if strings.TrimSpace(s.Text) == "" {
		return bad("text", "statement has no exact text")
	}
	if strings.TrimSpace(s.Locale) == "" {
		return bad("locale", "statement has no locale")
	}
	if strings.TrimSpace(s.SubjectRef) == "" {
		return bad("subject_ref", "statement has no subject")
	}
	if len(s.Facts) == 0 {
		return bad("facts", "statement declares no facts")
	}
	for i, f := range s.Facts {
		if strings.TrimSpace(f.Ref) == "" || strings.TrimSpace(f.Version) == "" {
			return bad(fmt.Sprintf("facts[%d]", i), "fact requires ref and version")
		}
	}
	if s.Period.From.IsZero() || s.Period.To.IsZero() || !s.Period.From.Before(s.Period.To) {
		return bad("period", "period must have an ordered, non-zero start and end")
	}
	if strings.TrimSpace(s.Purpose) == "" {
		return bad("purpose", "statement has no purpose")
	}
	if s.RequiredAssurance == trust.AssuranceUnspecified {
		return bad("required_assurance", "required assurance is unspecified")
	}
	if len(s.ResponseOptions) < 2 {
		return bad("response_options", "at least two response options are required")
	}
	seen := make(map[string]bool, len(s.ResponseOptions))
	for i, o := range s.ResponseOptions {
		if strings.TrimSpace(o.Code) == "" || strings.TrimSpace(o.Text) == "" {
			return bad(fmt.Sprintf("response_options[%d]", i), "response option requires code and text")
		}
		if seen[o.Code] {
			return bad(fmt.Sprintf("response_options[%d].code", i), "response option code is duplicated")
		}
		seen[o.Code] = true
	}
	if len(s.Evidence) == 0 {
		return bad("evidence", "statement declares no evidence requirements")
	}
	for i, e := range s.Evidence {
		if strings.TrimSpace(e.Kind) == "" {
			return bad(fmt.Sprintf("evidence[%d].kind", i), "evidence requirement has no kind")
		}
		if e.Required && strings.TrimSpace(e.Ref) == "" {
			return bad(fmt.Sprintf("evidence[%d].ref", i), "required evidence must pin a reference")
		}
	}
	if s.CorrectionPolicy.Allowed && strings.TrimSpace(s.CorrectionPolicy.PolicyRef) == "" {
		return bad("correction_policy.policy_ref", "allowed correction has no policy reference")
	}
	if s.RefusalPolicy.Allowed && strings.TrimSpace(s.RefusalPolicy.PolicyRef) == "" {
		return bad("refusal_policy.policy_ref", "allowed refusal has no policy reference")
	}
	if s.Status != "" && s.Status != AttestationDraft && s.Status != AttestationPublished && s.Status != AttestationRetired {
		return bad("status", "unknown statement status")
	}
	return nil
}

// Publish validates and returns a published copy with its canonical digest.
func (s AttestationStatement) Publish() (AttestationStatement, error) {
	if err := s.Validate(); err != nil {
		return AttestationStatement{}, err
	}
	s.Status = AttestationPublished
	s.Digest = s.ContentDigest()
	return s, nil
}

// ContentDigest returns the SHA-256 digest of all statement content except
// lifecycle status and Digest itself. Status changes do not change meaning.
func (s AttestationStatement) ContentDigest() string {
	facts := append([]AttestationFact(nil), s.Facts...)
	sort.Slice(facts, func(i, j int) bool {
		return facts[i].Ref+"\x00"+facts[i].Version < facts[j].Ref+"\x00"+facts[j].Version
	})
	options := append([]AttestationResponseOption(nil), s.ResponseOptions...)
	sort.Slice(options, func(i, j int) bool { return options[i].Code < options[j].Code })
	evidence := append([]AttestationEvidenceRequirement(nil), s.Evidence...)
	sort.Slice(evidence, func(i, j int) bool {
		return evidence[i].Kind+"\x00"+evidence[i].Ref < evidence[j].Kind+"\x00"+evidence[j].Ref
	})
	var b strings.Builder
	w := func(v string) { fmt.Fprintf(&b, "%d:", len(v)); b.WriteString(v); b.WriteByte(';') }
	w(s.ID)
	w(s.Version)
	w(s.Text)
	w(s.Locale)
	w(s.SubjectRef)
	w(s.Purpose)
	w(s.RequiredAssurance.String())
	fmt.Fprintf(&b, "%d:%d;", s.Period.From.UTC().UnixNano(), s.Period.To.UTC().UnixNano())
	for _, f := range facts {
		w(f.Ref)
		w(f.Version)
	}
	for _, o := range options {
		w(o.Code)
		w(o.Text)
	}
	for _, e := range evidence {
		w(e.Kind)
		w(e.Ref)
		w(fmt.Sprint(e.Required))
	}
	w(fmt.Sprint(s.CorrectionPolicy.Allowed))
	w(s.CorrectionPolicy.PolicyRef)
	w(fmt.Sprint(s.RefusalPolicy.Allowed))
	w(s.RefusalPolicy.PolicyRef)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// VerifyDigest reports whether a published statement still matches its
// recorded content digest.
func (s AttestationStatement) VerifyDigest() bool {
	return s.Status == AttestationPublished && s.Digest != "" && s.Digest == s.ContentDigest()
}
