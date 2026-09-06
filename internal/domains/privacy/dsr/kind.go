package dsr

import (
	"errors"
	"fmt"
	"slices"
)

// ErrKindInvalid is returned by [Kind.Validate] for any token outside the
// closed vocabulary this package declares.
var ErrKindInvalid = errors.New("dsr: request kind is not a declared value")

// Kind is a data-subject request kind, drawn from a closed vocabulary. It is
// never a free-form string: [AssuranceFloor] and [ClockTable] both key
// their declared tables off it, and an undeclared token must fail closed
// rather than silently fall through either table.
type Kind string

// The closed vocabulary of request kinds this package recognizes.
const (
	KindAccess        Kind = "ACCESS"
	KindRectification Kind = "RECTIFICATION"
	KindErasure       Kind = "ERASURE"
	KindRestriction   Kind = "RESTRICTION"
	KindPortability   Kind = "PORTABILITY"
	KindObjection     Kind = "OBJECTION"
)

// allKinds is the authoritative, ordered enumeration every completeness
// check ([AllKinds], the PRIV-005 CONFORMANCE test) walks.
var allKinds = []Kind{
	KindAccess,
	KindRectification,
	KindErasure,
	KindRestriction,
	KindPortability,
	KindObjection,
}

// AllKinds returns a fresh copy of the closed request-kind vocabulary, in a
// stable order.
func AllKinds() []Kind {
	return slices.Clone(allKinds)
}

// Validate reports whether k is one of the declared kinds.
func (k Kind) Validate() error {
	if slices.Contains(allKinds, k) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrKindInvalid, string(k))
}

// String returns the wire spelling of k.
func (k Kind) String() string { return string(k) }

// ErrChannelInvalid is returned by [Channel.Validate] for any token outside
// the closed vocabulary this package declares.
var ErrChannelInvalid = errors.New("dsr: intake channel is not a declared value")

// Channel is the closed vocabulary of ways a request can arrive.
type Channel string

// The closed vocabulary of intake channels this package recognizes.
const (
	ChannelPortal                   Channel = "PORTAL"
	ChannelEmail                    Channel = "EMAIL"
	ChannelPhone                    Channel = "PHONE"
	ChannelMail                     Channel = "MAIL"
	ChannelInPerson                 Channel = "IN_PERSON"
	ChannelAuthorizedRepresentative Channel = "AUTHORIZED_REPRESENTATIVE"
)

var allChannels = []Channel{
	ChannelPortal,
	ChannelEmail,
	ChannelPhone,
	ChannelMail,
	ChannelInPerson,
	ChannelAuthorizedRepresentative,
}

// Validate reports whether c is one of the declared channels.
func (c Channel) Validate() error {
	if slices.Contains(allChannels, c) {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrChannelInvalid, string(c))
}

// String returns the wire spelling of c.
func (c Channel) String() string { return string(c) }

// VerificationState is the closed vocabulary of identity-verification
// states a [DataSubjectRequest] can be in. It is always derived by
// [Intake] or [DataSubjectRequest.Verify] -- never asserted directly by a
// caller building a request from raw input.
type VerificationState string

// The closed vocabulary of verification states.
const (
	// VerificationUnverified is every request's state immediately after
	// [Intake]: a claim has been recorded, nothing has been proven yet.
	VerificationUnverified VerificationState = "UNVERIFIED"
	// VerificationVerified is set only by [DataSubjectRequest.Verify], and
	// only once identity-assurance evidence at or above the request Kind's
	// declared floor was bound to the request.
	VerificationVerified VerificationState = "VERIFIED"
	// VerificationRefused is set by [DataSubjectRequest.Verify] when the
	// supplied evidence was present but insufficient (expired, below floor,
	// wrong tenant, or an unauthorized representative). A refused request
	// remains re-verifiable: refusal is evidence, not a terminal state.
	VerificationRefused VerificationState = "REFUSED"
)

var allVerificationStates = []VerificationState{
	VerificationUnverified,
	VerificationVerified,
	VerificationRefused,
}

// Validate reports whether s is one of the declared verification states.
func (s VerificationState) Validate() error {
	if slices.Contains(allVerificationStates, s) {
		return nil
	}
	return fmt.Errorf("dsr: verification state %q is not a declared value", string(s))
}

// String returns the wire spelling of s.
func (s VerificationState) String() string { return string(s) }
