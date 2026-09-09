// Package attest verifies attestation statements the way ATTEST-003 requires:
// the attestor's identity, the attestor's authority for the claim's mode, and
// the assurance the assertor actually reached are all checked against the
// trusted attestor record, and the decision that results carries both the
// statement proof and the current AuthZ context. Nothing in this package
// accepts a statement on the strength of a header, a caller assertion or an
// implicit mode: proxy and delegated claims fail unless the requirement
// explicitly lists the mode, a revoked or expired attestor fails even when the
// signature verifies, and an insufficient-assurance claim fails even when
// everything else is in order.
package attest

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Mode is how an attestor stands to the subject it attests.
type Mode uint8

// Attestation modes. ModeUnspecified is never accepted: a statement without a
// mode is not a direct claim by default, so silence fails closed.
const (
	ModeUnspecified Mode = iota
	ModeDirect
	ModeProxy
	ModeDelegated
)

var modeWire = map[Mode]string{
	ModeDirect:    "direct",
	ModeProxy:     "proxy",
	ModeDelegated: "delegated",
}

// String returns the canonical wire spelling.
func (m Mode) String() string {
	if w, ok := modeWire[m]; ok {
		return w
	}
	return "mode_unspecified"
}

// ParseMode maps a wire token back onto a mode.
func ParseMode(wire string) (Mode, error) {
	for m, w := range modeWire {
		if w == wire {
			return m, nil
		}
	}
	return ModeUnspecified, fmt.Errorf("attest: unknown mode %q", wire)
}

// Status is an attestor's lifecycle state in the trusted directory.
type Status uint8

// Attestor lifecycle states.
const (
	StatusUnspecified Status = iota
	StatusActive
	StatusRevoked
)

// Attestor is one trusted attestor record: who it is, which claims modes its
// authority covers, the floor of assurance it is empowered to assert, and the
// key its statements are signed with. The record is the server's view of the
// attestor; a statement never redefines it.
type Attestor struct {
	ID           string
	Tenant       values.TenantId
	Authority    []Mode
	MinAssurance trust.Assurance
	SigningKey   [32]byte
	Status       Status
	EvidenceID   string
}

// Directory is an immutable, keyed registry of trusted attestor records.
type Directory struct {
	byID map[string]Attestor
}

// NewDirectory validates and freezes the attestor records. Duplicate IDs,
// missing fields and non-active status values that are neither Active nor
// Revoked are rejected at construction time.
func NewDirectory(records []Attestor) (*Directory, error) {
	d := &Directory{byID: make(map[string]Attestor, len(records))}
	for _, r := range records {
		if r.ID == "" {
			return nil, errors.New("attest: attestor record has no id")
		}
		if r.Tenant == "" {
			return nil, fmt.Errorf("attest: attestor %s has no tenant", r.ID)
		}
		if len(r.Authority) == 0 {
			return nil, fmt.Errorf("attest: attestor %s has no authority modes", r.ID)
		}
		if r.SigningKey == [32]byte{} {
			return nil, fmt.Errorf("attest: attestor %s has no signing key", r.ID)
		}
		if r.Status != StatusActive && r.Status != StatusRevoked {
			return nil, fmt.Errorf("attest: attestor %s has an invalid status", r.ID)
		}
		if r.EvidenceID == "" {
			return nil, fmt.Errorf("attest: attestor %s has no evidence id", r.ID)
		}
		if _, dup := d.byID[r.ID]; dup {
			return nil, fmt.Errorf("attest: attestor %s is recorded twice", r.ID)
		}
		d.byID[r.ID] = r
	}
	return d, nil
}

// LookUp returns the attestor record for id and whether it is present.
func (d *Directory) LookUp(id string) (Attestor, bool) {
	r, ok := d.byID[id]
	return r, ok
}

// Statement is the attestation claim to verify. Every field is part of the
// signed digest except Signature itself; a statement is only as valid as the
// signed whole.
type Statement struct {
	AttestorID string
	Tenant     values.TenantId
	Subject    string
	SessionRef string
	Mode       Mode
	Assurance  trust.Assurance
	Purposes   []string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	Signature  []byte
}

// canonical returns the framed, length-prefixed serialization of every signed
// field, so that no two distinct statements share a digest by field-boundary
// ambiguity and no unsigned field can shift the signed content.
func (s Statement) canonical() string {
	purposes := append([]string(nil), s.Purposes...)
	sort.Strings(purposes)
	var b strings.Builder
	write := func(label, v string) {
		b.WriteString(label)
		b.WriteString("=")
		b.WriteString(fmt.Sprintf("%d:", len(v)))
		b.WriteString(v)
		b.WriteString(";")
	}
	write("attestor", s.AttestorID)
	write("tenant", s.Tenant.String())
	write("subject", s.Subject)
	write("session", s.SessionRef)
	write("mode", s.Mode.String())
	write("assurance", s.Assurance.String())
	for _, p := range purposes {
		b.WriteString("purpose=")
		b.WriteString(fmt.Sprintf("%d:", len(p)))
		b.WriteString(p)
		b.WriteString(";")
	}
	b.WriteString("iat=")
	b.WriteString(fmt.Sprintf("%d", s.IssuedAt.UTC().UnixNano()))
	b.WriteString(";exp=")
	b.WriteString(fmt.Sprintf("%d", s.ExpiresAt.UTC().UnixNano()))
	return b.String()
}

// Digest returns the hex-encoded SHA-256 of the statement's canonical form.
func (s Statement) Digest() string {
	sum := sha256.Sum256([]byte(s.canonical()))
	return hex.EncodeToString(sum[:])
}

// Sign fills Signature with the HMAC-SHA256 of the digest under key. A
// statement signed under the wrong key cannot verify against the directory.
func (s *Statement) Sign(key [32]byte) {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(s.Digest()))
	s.Signature = mac.Sum(nil)
}

// Requirement is the claim policy a caller verifies a statement against: how
// much assurance the decision needs, and exactly which modes are permitted.
// An empty PermittedModes permits nothing: the modes a statement may use are
// always listed explicitly, never implied.
type Requirement struct {
	MinAssurance   trust.Assurance
	PermittedModes []Mode
}

// Proof binds the verified statement to its attestor and to the instant of the
// decision. It is what a downstream [Decision] carries so that the acceptance
// can be re-checked against the record without re-trusting the wire.
type Proof struct {
	StatementDigest    string
	AttestorID         string
	AttestorEvidenceID string
	Mode               Mode
	Assurance          trust.Assurance
	PrincipalEvidence  string
}

// Reason tokens a Decision carries. They are stable policy tokens, matchable
// with ==, never display strings.
const (
	ReasonAccepted         = "accepted"
	ReasonAttestorUnknown  = "attestor_unknown"
	ReasonAttestorRevoked  = "attestor_revoked"
	ReasonTenantMismatch   = "tenant_mismatch"
	ReasonSignatureInvalid = "signature_invalid"
	ReasonNotValidYet      = "statement_not_valid_yet"
	ReasonStatementExpired = "statement_expired"
	ReasonModeNotPermitted = "mode_not_permitted"
	ReasonAssuranceLow     = "assurance_insufficient"
	ReasonCurrentAssurance = "current_assurance_insufficient"
	ReasonSubjectMismatch  = "subject_mismatch"
	ReasonSessionMismatch  = "session_mismatch"
)

// Decision is the recorded result of verifying one statement. An accepted
// decision always carries the statement proof and the current AuthZ context
// (the verifying principal's evidence id and fingerprint); a refused decision
// carries the refusing reason and nothing sensitive about the claimant.
type Decision struct {
	Accepted    bool
	Reason      string
	Proof       *Proof
	Principal   string
	PrincipalFP string
	EvidenceID  string
	At          time.Time
}

// Verify checks statement under requirement for principal at time now against
// the trusted directory and returns the decision. It never returns a nil
// decision for a non-nil statement check: every failure is a refused decision
// with a typed reason, so the caller has one result type to record. The error
// return is non-nil only for a structurally invalid verification request (a
// nil directory, principal or an unparseable requirement), not for a refused
// claim.
func Verify(dir *Directory, st Statement, req Requirement, principal *trust.Principal, now time.Time) (Decision, error) {
	if dir == nil {
		return Decision{}, errors.New("attest: nil attestor directory")
	}
	if principal == nil {
		return Decision{}, errors.New("attest: nil principal")
	}
	if req.MinAssurance == trust.AssuranceUnspecified {
		return Decision{}, errors.New("attest: requirement has no minimum assurance")
	}
	if len(req.PermittedModes) == 0 {
		return Decision{}, errors.New("attest: requirement permits no modes")
	}
	now = now.UTC()
	refuse := func(reason string) Decision {
		return Decision{
			Accepted:    false,
			Reason:      reason,
			Principal:   principal.EvidenceID(),
			PrincipalFP: principal.Fingerprint(),
			At:          now,
		}
	}

	record, ok := dir.LookUp(st.AttestorID)
	if !ok {
		return refuse(ReasonAttestorUnknown), nil
	}
	if record.Status != StatusActive {
		return refuse(ReasonAttestorRevoked), nil
	}
	if st.Tenant != record.Tenant || st.Tenant != principal.Tenant() {
		return refuse(ReasonTenantMismatch), nil
	}
	authorizedMode := false
	for _, mode := range record.Authority {
		if mode == st.Mode {
			authorizedMode = true
			break
		}
	}
	if !authorizedMode {
		return refuse(ReasonModeNotPermitted), nil
	}
	if !st.Assurance.AtLeast(record.MinAssurance) {
		return refuse(ReasonAssuranceLow), nil
	}

	mac := hmac.New(sha256.New, record.SigningKey[:])
	mac.Write([]byte(st.Digest()))
	if subtle.ConstantTimeCompare(st.Signature, mac.Sum(nil)) != 1 {
		return refuse(ReasonSignatureInvalid), nil
	}
	if now.Before(st.IssuedAt.UTC()) {
		return refuse(ReasonNotValidYet), nil
	}
	if !now.Before(st.ExpiresAt.UTC()) {
		return refuse(ReasonStatementExpired), nil
	}

	permitted := false
	for _, m := range req.PermittedModes {
		if m == st.Mode {
			permitted = true
			break
		}
	}
	if !permitted {
		return refuse(ReasonModeNotPermitted), nil
	}

	if !st.Assurance.AtLeast(req.MinAssurance) {
		return refuse(ReasonAssuranceLow), nil
	}
	if !principal.Assurance().AtLeast(req.MinAssurance) {
		return refuse(ReasonCurrentAssurance), nil
	}
	if st.Subject != principal.Subject() {
		return refuse(ReasonSubjectMismatch), nil
	}
	if st.SessionRef != principal.SessionRef() {
		return refuse(ReasonSessionMismatch), nil
	}

	proof := &Proof{
		StatementDigest:    st.Digest(),
		AttestorID:         st.AttestorID,
		AttestorEvidenceID: record.EvidenceID,
		Mode:               st.Mode,
		Assurance:          st.Assurance,
		PrincipalEvidence:  principal.EvidenceID(),
	}
	sum := sha256.Sum256([]byte("attest:decision:" + st.Digest() + ":" + principal.EvidenceID() + ":" + now.UTC().Format(time.RFC3339Nano)))
	return Decision{
		Accepted:    true,
		Reason:      ReasonAccepted,
		Proof:       proof,
		Principal:   principal.EvidenceID(),
		PrincipalFP: principal.Fingerprint(),
		EvidenceID:  "ev:attest:" + hex.EncodeToString(sum[:])[:32],
		At:          now,
	}, nil
}
