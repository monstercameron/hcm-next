package dsr

import (
	"errors"
	"fmt"
	"strings"
)

// ErrClaimsInvalid is returned by [SubjectClaims.Validate] when a claims
// record carries nothing a downstream verifier could ever check.
var ErrClaimsInvalid = errors.New("dsr: subject identity claims are incomplete")

// SubjectClaims is what a requester asserts about their own identity at
// intake. Every field here is an unverified claim -- exactly what PRIV-005's
// RED clause guards against is treating any of this as proof. Only
// [DataSubjectRequest.Verify], fed a separately-obtained [IdentityEvidence],
// ever moves a request past [VerificationUnverified].
type SubjectClaims struct {
	// ExternalRef is an opaque reference to an existing identity record the
	// requester claims to be (e.g. a canonical
	// [github.com/monstercameron/hcm-next/internal/kernel/values.EntityRef]
	// string), when the requester names one. Preferred as the duplicate
	// matching key when present, since it is the least ambiguous claim.
	ExternalRef string
	FullName    string
	Email       string
	Phone       string
	// RepresentativeRef names an authorized representative or agent acting
	// on the subject's behalf, when the request did not come from the
	// subject directly (e.g. "AUTHORIZED_REPRESENTATIVE" channel). Empty
	// means the subject is claimed to be acting for themselves.
	// [DataSubjectRequest.Verify] refuses to advance a request naming one
	// unless the bound [IdentityEvidence] explicitly attests representative
	// authority.
	RepresentativeRef string
}

// Validate reports whether c carries at least one claim a verifier could
// check the requester against.
func (c SubjectClaims) Validate() error {
	if strings.TrimSpace(c.ExternalRef) == "" &&
		strings.TrimSpace(c.Email) == "" &&
		strings.TrimSpace(c.Phone) == "" &&
		strings.TrimSpace(c.FullName) == "" {
		return fmt.Errorf("%w: no external reference, email, phone or name claimed", ErrClaimsInvalid)
	}
	return nil
}

// Key returns the deterministic, normalized matching key [DetectDuplicate]
// groups requests by: the first non-empty claim, in the fixed preference
// order ExternalRef, Email, Phone, FullName, lower-cased and trimmed. Two
// claims records that differ only in whitespace or case on the winning
// field produce the same key.
func (c SubjectClaims) Key() string {
	for _, v := range []string{c.ExternalRef, c.Email, c.Phone, c.FullName} {
		if norm := strings.ToLower(strings.TrimSpace(v)); norm != "" {
			return norm
		}
	}
	return ""
}

// canonicalBytes appends c's deterministic, length-prefixed encoding to dst.
func (c SubjectClaims) canonicalBytes(dst []byte) []byte {
	return appendFields(dst,
		"external_ref", c.ExternalRef,
		"full_name", c.FullName,
		"email", c.Email,
		"phone", c.Phone,
		"representative_ref", c.RepresentativeRef,
	)
}
