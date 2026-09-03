package bitemporal

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Mode selects one of the effective-date debugger's five query shapes
// (specs/hris-admin-dataops.md "Effective-Date Debugger").
type Mode string

const (
	// ModeCurrent resolves the fact effective now, using everything known now.
	ModeCurrent Mode = "CURRENT"
	// ModeEffectiveAsOf resolves the fact effective at Request.EffectiveAt,
	// using everything known now (or at Request.KnownAt, if given). This is
	// AsOf: "what was true on this business date".
	ModeEffectiveAsOf Mode = "EFFECTIVE_AS_OF"
	// ModeKnownAsOf resolves what was known at Request.KnownAt about the fact
	// effective now (or at Request.EffectiveAt, if given). This is KnownAt:
	// "what had the ledger recorded by this instant".
	ModeKnownAsOf Mode = "KNOWN_AS_OF"
	// ModeBetween returns every fact whose effective_at falls in the
	// half-open window [EffectiveFrom, EffectiveTo), visible as of KnownAt.
	ModeBetween Mode = "BETWEEN"
	// ModeHistory returns every fact visible as of KnownAt, with no
	// effective-time bound - the full evidence trail for a subject/field.
	ModeHistory Mode = "HISTORY"
)

// Valid reports whether m is one of the five declared modes.
func (m Mode) Valid() bool {
	switch m {
	case ModeCurrent, ModeEffectiveAsOf, ModeKnownAsOf, ModeBetween, ModeHistory:
		return true
	default:
		return false
	}
}

// MaxPageSize bounds one page. A query that pages without a bound is a query
// that can be made to allocate without limit.
const MaxPageSize = 1000

// DefaultPageSize is used when a Request does not set Limit.
const DefaultPageSize = 200

// Request describes one bitemporal fact query.
type Request struct {
	// Tenant must equal the Decision's tenant; see ErrTenantMismatch.
	Tenant uuid.UUID
	Mode   Mode

	// Subject restricts the query to one ledger stream (e.g. "worker:1").
	// Empty means every subject the Decision authorizes.
	Subject string
	// Field restricts the query to one schema_ref. Empty means every field
	// the Decision authorizes. See Decision's doc for why schema_ref is what
	// "field" means at this layer.
	Field string

	// EffectiveAt is the business-time point for CURRENT, EFFECTIVE_AS_OF and
	// KNOWN_AS_OF. Zero defaults to "now" for CURRENT and KNOWN_AS_OF, and is
	// required for EFFECTIVE_AS_OF.
	EffectiveAt time.Time
	// EffectiveFrom/EffectiveTo bound BETWEEN's half-open window
	// [EffectiveFrom, EffectiveTo). EffectiveFrom is required; a zero
	// EffectiveTo leaves the window open-ended, matching the shared
	// EffectiveTimeRange contract (canonical-envelope-and-digest.md).
	EffectiveFrom time.Time
	EffectiveTo   time.Time

	// KnownAt is the recorded-time knowledge horizon. Zero defaults to "now"
	// for every mode except KNOWN_AS_OF, where it is required.
	KnownAt time.Time

	// Cursor resumes a prior page; empty starts at the first page. A cursor
	// minted for a different tenant, mode, subject or field is refused
	// (ErrCursorInvalid) rather than silently reinterpreted.
	Cursor string
	// Limit bounds the page size. Zero uses DefaultPageSize; anything over
	// MaxPageSize is clamped down to it.
	Limit int
}

// Validate reports whether the request is well formed for its mode. It does
// not consult a Decision; tenant/decision agreement is checked by Query.
func (r Request) Validate() error {
	if r.Tenant == uuid.Nil {
		return ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if !r.Mode.Valid() {
		return ErrRequestInvalid{Field: "Mode", Reason: fmt.Sprintf("%q is not one of CURRENT, EFFECTIVE_AS_OF, KNOWN_AS_OF, BETWEEN, HISTORY", string(r.Mode))}
	}
	switch r.Mode {
	case ModeEffectiveAsOf:
		if r.EffectiveAt.IsZero() {
			return ErrRequestInvalid{Field: "EffectiveAt", Reason: "is required for EFFECTIVE_AS_OF"}
		}
	case ModeKnownAsOf:
		if r.KnownAt.IsZero() {
			return ErrRequestInvalid{Field: "KnownAt", Reason: "is required for KNOWN_AS_OF"}
		}
	case ModeBetween:
		if r.EffectiveFrom.IsZero() {
			return ErrRequestInvalid{Field: "EffectiveFrom", Reason: "is required for BETWEEN"}
		}
		if !r.EffectiveTo.IsZero() && !r.EffectiveFrom.Before(r.EffectiveTo) {
			return ErrRequestInvalid{Field: "EffectiveTo", Reason: "must be strictly after EffectiveFrom; the window is half-open [from, to)"}
		}
	}
	if r.Limit < 0 {
		return ErrRequestInvalid{Field: "Limit", Reason: "cannot be negative"}
	}
	return nil
}

// pageSize returns the effective, bounded page size.
func (r Request) pageSize() int {
	switch {
	case r.Limit <= 0:
		return DefaultPageSize
	case r.Limit > MaxPageSize:
		return MaxPageSize
	default:
		return r.Limit
	}
}

// CorrectionKind labels why a fact appears at its position in the timeline.
// See doc.go for the exact rule that distinguishes CORRECTION from
// SUPERSESSION.
type CorrectionKind string

const (
	// KindOriginal is any non-CORRECTION assertion class: the fact as first
	// recorded, not itself correcting anything.
	KindOriginal CorrectionKind = "ORIGINAL"
	// KindCorrection is a CORRECTION whose effective_at equals its target's:
	// a retroactive fix of what was already asserted true at that instant.
	KindCorrection CorrectionKind = "CORRECTION"
	// KindSupersession is a CORRECTION whose effective_at differs from its
	// target's: a new business-time boundary that also formally invalidates
	// the fact it names.
	KindSupersession CorrectionKind = "SUPERSESSION"
)

// Fact is one authorized ledger event, labeled with its chronology so a
// caller never has to infer "what happened" from "what is now believed".
type Fact struct {
	Tenant          uuid.UUID
	StreamKey       string
	Sequence        int64
	EventID         uuid.UUID
	SchemaRef       string
	AssertionClass  ledger.AssertionClass
	CorrectionKind  CorrectionKind
	Authority       string
	SourceRef       string
	Payload         []byte
	ArtifactRef     string
	Digest          string
	DigestAlgorithm string
	OccurredAt      time.Time
	EffectiveAt     time.Time
	RecordedAt      time.Time
	CorrelationID   uuid.UUID
	CausationID     uuid.UUID
	IdempotencyKey  string
	// Corrects names the assertion this fact corrects or supersedes, nil for
	// KindOriginal.
	Corrects *ledger.EventRef
}

// Result is one page of authorized facts.
type Result struct {
	Facts []Fact
	// NextCursor resumes after the last fact in Facts. Empty when the
	// authorized set is exhausted.
	NextCursor string
}

// cursorPayload is the opaque, versioned position a page cursor encodes.
// Tampering with it can move the resume point but can never widen what is
// authorized: every reissued query re-applies the caller's Decision, and a
// cursor minted under a different tenant/mode/subject/field is rejected
// outright.
type cursorPayload struct {
	V         int    `json:"v"`
	Tenant    string `json:"tenant"`
	Mode      Mode   `json:"mode"`
	Subject   string `json:"subject"`
	Field     string `json:"field"`
	Effective int64  `json:"effective_ns"`
	Recorded  int64  `json:"recorded_ns"`
	Stream    string `json:"stream"`
	Schema    string `json:"schema"`
	Sequence  int64  `json:"sequence"`
}

const cursorVersion = 1

func encodeCursor(req Request, effectiveAt, recordedAt time.Time, streamKey, schemaRef string, sequence int64) string {
	p := cursorPayload{
		V:         cursorVersion,
		Tenant:    req.Tenant.String(),
		Mode:      req.Mode,
		Subject:   req.Subject,
		Field:     req.Field,
		Effective: effectiveAt.UnixNano(),
		Recorded:  recordedAt.UnixNano(),
		Stream:    streamKey,
		Schema:    schemaRef,
		Sequence:  sequence,
	}
	raw, err := json.Marshal(p)
	if err != nil {
		// json.Marshal on this fixed, all-scalar struct cannot fail.
		panic(fmt.Sprintf("bitemporal: encode cursor: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(req Request, cursor string) (cursorPayload, error) {
	var p cursorPayload
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return cursorPayload{}, ErrCursorInvalid{Reason: "not valid base64"}
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return cursorPayload{}, ErrCursorInvalid{Reason: "not a valid cursor payload"}
	}
	if p.V != cursorVersion {
		return cursorPayload{}, ErrCursorInvalid{Reason: fmt.Sprintf("cursor version %d is not supported", p.V)}
	}
	if p.Tenant != req.Tenant.String() {
		return cursorPayload{}, ErrCursorInvalid{Reason: "cursor was minted for a different tenant"}
	}
	if p.Mode != req.Mode {
		return cursorPayload{}, ErrCursorInvalid{Reason: "cursor was minted for a different mode"}
	}
	if p.Subject != req.Subject || p.Field != req.Field {
		return cursorPayload{}, ErrCursorInvalid{Reason: "cursor was minted for a different subject or field scope"}
	}
	return p, nil
}
