package dsr

import "github.com/monstercameron/hcm-next/internal/kernel/values"

// EventKind is the closed vocabulary of events [Evidence] records.
type EventKind string

// Evidence event kinds.
const (
	EventIntake              EventKind = "INTAKE"
	EventDuplicateLinked     EventKind = "DUPLICATE_LINKED"
	EventVerified            EventKind = "VERIFIED"
	EventVerificationRefused EventKind = "VERIFICATION_REFUSED"
)

// Evidence is one immutable, append-only record of something that happened
// to a [DataSubjectRequest]. [DataSubjectRequest.Trail] only ever grows by
// appending an Evidence value onto a fresh copy of the slice -- see
// [DataSubjectRequest.appendEvidence] -- never by mutating an existing
// entry.
type Evidence struct {
	// ID is derived from this event's own canonical digest.
	ID string
	// RequestDigest is the request's [DataSubjectRequest.Digest] at the
	// exact moment this event was recorded, binding the event to the exact
	// state snapshot it describes.
	RequestDigest string
	Kind          EventKind
	At            values.Instant
	// Detail is a stable reason token (e.g. an [AdvanceCode] or verify
	// refusal reason), never a reproduced claim value.
	Detail string
}

const evidenceEventPrefix = "ev:privacy:dsr:event:"

func newEvidence(kind EventKind, requestDigest string, at values.Instant, detail string) Evidence {
	dst := appendFields(nil,
		"request_digest", requestDigest,
		"kind", string(kind),
		"at", at.String(),
		"detail", detail,
	)
	return Evidence{
		ID:            evidenceEventPrefix + digestHex(dst),
		RequestDigest: requestDigest,
		Kind:          kind,
		At:            at,
		Detail:        detail,
	}
}
