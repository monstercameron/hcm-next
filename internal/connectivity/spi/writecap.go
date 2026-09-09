package spi

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

// AuthorityAmendmentDigest identifies a signed amendment to this release's
// read-only authority boundary. It is opaque here; a later phase defines how
// one is produced and signed.
type AuthorityAmendmentDigest string

// DeclaredAmendments returns the write-authority amendments this build
// recognizes.
//
// It always returns nil in this release: P1A is sold as a paid observation,
// preflight and simulation release that leaves the incumbent byte-for-byte
// unchanged, and the forbidden-import list in
// definitions/planning/gates/p1a-manifest.yaml names
// internal/connectivity/writeadapters and
// internal/transport/clients/providerwrite as packages a P1A evidence
// package may never import. An adapter that could grant itself write here
// would make that firewall pointless, so this function - not an adapter, not
// a manifest - is the single place that could ever say yes, and it says no
// unconditionally until a signed amendment exists for it to recognize.
func DeclaredAmendments() []AuthorityAmendmentDigest { return nil }

// WriteCapabilityDeclaration is a request for an adapter to gain write
// capability over one object. It is evaluated by [DeclareWriteCapability]
// alone; no [Adapter] method ever consumes or grants one.
type WriteCapabilityDeclaration struct {
	// Object is the record family write capability is requested for.
	Object ObjectKind
	// RequestedBy names the caller making the declaration, for the refusal's
	// audit trail. It is never a secret.
	RequestedBy string
	// AuthorityAmendmentDigest is the signed amendment the declaration claims
	// authorizes it. Empty means no amendment is claimed at all.
	AuthorityAmendmentDigest AuthorityAmendmentDigest
}

// Validate reports whether the declaration is structurally well-formed. A
// structurally invalid declaration is refused before any amendment lookup:
// an empty object or requester can never be authorized by any amendment.
func (d WriteCapabilityDeclaration) Validate() error {
	const op = "spi.WriteCapabilityDeclaration.Validate"
	switch {
	case !d.Object.Valid():
		return connectivity.Fail(op, connectivity.ErrInvalid, "declaration names unknown object %q", string(d.Object))
	case strings.TrimSpace(d.RequestedBy) == "":
		return connectivity.Fail(op, connectivity.ErrInvalid, "declaration has no requester")
	}
	return nil
}

// RefusalCode names the shape of a write-capability refusal. A caller
// branches on this rather than parsing [WriteCapabilityDecision.Detail].
type RefusalCode string

// The declared refusal shapes.
const (
	// RefusalInvalidDeclaration means the declaration itself is malformed;
	// no amendment lookup was attempted.
	RefusalInvalidDeclaration RefusalCode = "INVALID_DECLARATION"
	// RefusalNoAmendment means the declaration claimed no amendment at all.
	RefusalNoAmendment RefusalCode = "NO_AUTHORITY_AMENDMENT"
	// RefusalUnknownAmendment means the declaration claimed an amendment this
	// build does not recognize.
	RefusalUnknownAmendment RefusalCode = "UNKNOWN_AUTHORITY_AMENDMENT"
)

// Valid reports whether c is one of the declared refusal shapes.
func (c RefusalCode) Valid() bool {
	switch c {
	case RefusalInvalidDeclaration, RefusalNoAmendment, RefusalUnknownAmendment:
		return true
	default:
		return false
	}
}

// WriteCapabilityDecision is the typed outcome of evaluating a
// [WriteCapabilityDeclaration]. It is returned alongside a classified error
// so a caller can branch on either the Go error chain
// (errors.Is(err, connectivity.ErrPermission)) or the structured Code,
// whichever its own error handling style prefers.
type WriteCapabilityDecision struct {
	// Object echoes the declaration's object.
	Object ObjectKind
	// Refused is true for every decision this release can produce.
	Refused bool
	// Code names the refusal's shape. It is the zero value when Refused is
	// false.
	Code RefusalCode
	// Detail is a non-secret explanation.
	Detail string
	// DecidedAt is when the decision was made.
	DecidedAt time.Time
}

// DeclareWriteCapability evaluates decl against the amendments this build
// recognizes and returns a typed refusal.
//
// now is supplied by the caller rather than read from the wall clock so the
// decision remains a pure function of its inputs: a golden vector for a
// refusal shape stays golden forever, and a test never has to tolerate a
// moving DecidedAt.
//
// Because [DeclaredAmendments] is empty in this release, every well-formed
// declaration is refused with [RefusalNoAmendment] or
// [RefusalUnknownAmendment]; the amendment-match branch below is unreachable
// today and exists only so a later phase can light it up without changing
// this function's contract or its callers.
func DeclareWriteCapability(decl WriteCapabilityDeclaration, now time.Time) (WriteCapabilityDecision, error) {
	const op = "spi.DeclareWriteCapability"

	if err := decl.Validate(); err != nil {
		return WriteCapabilityDecision{
				Object:    decl.Object,
				Refused:   true,
				Code:      RefusalInvalidDeclaration,
				Detail:    err.Error(),
				DecidedAt: now,
			},
			connectivity.Fail(op, connectivity.ErrInvalid, "write capability declaration is invalid: %v", err)
	}

	if decl.AuthorityAmendmentDigest == "" {
		return WriteCapabilityDecision{
				Object:    decl.Object,
				Refused:   true,
				Code:      RefusalNoAmendment,
				Detail:    "no authority amendment is declared; this release grants no adapter write capability",
				DecidedAt: now,
			},
			connectivity.Fail(op, connectivity.ErrPermission, "object %q requests write with no declared authority amendment", string(decl.Object))
	}

	for _, amendment := range DeclaredAmendments() {
		if amendment == decl.AuthorityAmendmentDigest {
			return WriteCapabilityDecision{
				Object:    decl.Object,
				Refused:   false,
				DecidedAt: now,
			}, nil
		}
	}

	return WriteCapabilityDecision{
			Object:    decl.Object,
			Refused:   true,
			Code:      RefusalUnknownAmendment,
			Detail:    "declared authority amendment is not recognized by this build",
			DecidedAt: now,
		},
		connectivity.Fail(op, connectivity.ErrPermission, "authority amendment %q is not recognized for object %q", string(decl.AuthorityAmendmentDigest), string(decl.Object))
}
