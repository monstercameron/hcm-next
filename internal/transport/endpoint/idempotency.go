package endpoint

// This file implements ENDPOINT-004: one canonical idempotency-key/request
// digest, scoped by principal/tenant/capability, that unifies the
// HTTP-header idempotency key and the message-embedded idempotency key,
// plus the expected-revision precondition that guards optimistic
// concurrency. Both mechanics are transport mechanics only; they say
// nothing about what a "material payload change" or a "revision" means for
// any particular capability — that judgment belongs to the semantic owner
// (see the todo's REFACTOR note).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// IdempotencyKeyHeader is the canonical HTTP header carrying a client
// idempotency key. http.Header.Get canonicalizes the header name and
// resolves to the first value regardless of how or in what order the
// header set was populated, so lookups through it are order independent.
const IdempotencyKeyHeader = "Idempotency-Key"

// HeaderIdempotencyKey extracts the idempotency key from an HTTP header set.
func HeaderIdempotencyKey(h http.Header) string {
	if h == nil {
		return ""
	}
	return strings.TrimSpace(h.Get(IdempotencyKeyHeader))
}

// Scope is the principal/tenant/capability boundary an idempotency key and
// request digest are computed within. It is load-bearing: the same key and
// the same payload under two different scopes must never resolve to the
// same stored result.
type Scope struct {
	Principal  string
	Tenant     string
	Capability string
}

var (
	// ErrIdempotencyKeyRequired is returned when neither the HTTP header nor
	// the message body carried an idempotency key. A missing key is refused
	// outright; it is never treated as a wildcard that matches anything
	// already stored.
	ErrIdempotencyKeyRequired = errors.New("endpoint: idempotency key is required")

	// ErrIdempotencyKeyMismatch is returned when the header-carried key and
	// the message-carried key are both present and disagree. Accepting a
	// mismatched pair (picking one silently) is the exact RED clause this
	// todo closes.
	ErrIdempotencyKeyMismatch = errors.New("endpoint: header and message idempotency keys disagree")

	// ErrScopeRequired is returned when principal, tenant or capability is
	// blank. A blank scope component is a zero value and must never be
	// treated as a valid, and therefore collidable, scope.
	ErrScopeRequired = errors.New("endpoint: principal, tenant and capability are all required")

	// ErrIdempotencyConflict is the sentinel behind PayloadConflict.
	ErrIdempotencyConflict = errors.New("endpoint: idempotency key reused with a different payload")

	// ErrRevisionMismatch is the sentinel behind RevisionConflict.
	ErrRevisionMismatch = errors.New("endpoint: expected revision does not match the current revision")

	// ErrRevisionRequired guards the zero value for revisions: a revision of
	// 0, on either side of the comparison, is never a valid precondition
	// match and must be refused rather than silently treated as equal.
	ErrRevisionRequired = errors.New("endpoint: expected and current revision must both be positive")
)

// PayloadConflict reports that an idempotency key was replayed with a
// materially different request payload. Digest is the canonical key digest
// so a caller can correlate which logical request was affected.
type PayloadConflict struct{ Digest string }

func (e *PayloadConflict) Error() string {
	return fmt.Sprintf("endpoint: idempotency key %s reused with a different payload", e.Digest)
}

// Is lets errors.Is(err, ErrIdempotencyConflict) match this concrete type.
func (e *PayloadConflict) Is(target error) bool { return target == ErrIdempotencyConflict }

// RevisionConflict reports that an expected-revision precondition failed. It
// names the resource's current revision so the caller can learn it safely,
// without the stale write landing.
type RevisionConflict struct{ Current uint64 }

func (e *RevisionConflict) Error() string {
	return fmt.Sprintf("endpoint: stale expected revision; current revision is %d", e.Current)
}

// Is lets errors.Is(err, ErrRevisionMismatch) match this concrete type.
func (e *RevisionConflict) Is(target error) bool { return target == ErrRevisionMismatch }

// CanonicalDigest folds the HTTP-header idempotency key and the
// message-body idempotency key into one identity, scoped by principal,
// tenant and capability. A header-keyed transport (HTTP) and a
// message-keyed transport (gRPC/direct) carrying the same logical retry
// must resolve to the same digest; that unification is what lets one
// idempotency store be shared safely across transports.
//
// Every field is length-prefixed before hashing so that concatenation
// cannot make two distinct tuples collide (e.g. principal="ab",tenant="c"
// must never hash the same as principal="a",tenant="bc").
func CanonicalDigest(scope Scope, headerKey, messageKey string) (string, error) {
	principal := strings.TrimSpace(scope.Principal)
	tenant := strings.TrimSpace(scope.Tenant)
	capability := strings.TrimSpace(scope.Capability)
	if principal == "" || tenant == "" || capability == "" {
		return "", ErrScopeRequired
	}

	header := strings.TrimSpace(headerKey)
	message := strings.TrimSpace(messageKey)
	var key string
	switch {
	case header == "" && message == "":
		return "", ErrIdempotencyKeyRequired
	case header == "":
		key = message
	case message == "":
		key = header
	case header != message:
		return "", ErrIdempotencyKeyMismatch
	default:
		key = header
	}

	h := sha256.New()
	for _, part := range []string{principal, tenant, capability, key} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{0})
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PayloadDigest returns a stable content digest for a request payload. Two
// byte-identical payloads always produce the same digest, and the digest is
// never the empty string, so a missing/uncomputed digest can never be
// mistaken for a real one.
func PayloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// Effect performs the durable side effect for one idempotent request. A
// Coordinator guarantees it runs at most once per canonical digest no
// matter how many times Do is called, concurrently or sequentially, or
// whether the previous attempt's result was ambiguous.
type Effect func(ctx context.Context) (Outcome, error)

// Request is one attempt to perform an idempotent, optionally
// revision-guarded, operation.
type Request struct {
	Scope      Scope
	HeaderKey  string
	MessageKey string
	Payload    []byte

	// ExpectedRevision, when non-nil, requests an optimistic-concurrency
	// precondition: the write is refused unless it equals CurrentRevision.
	// A nil pointer means "no precondition requested", which is distinct
	// from — and must never be confused with — an expected revision of 0.
	ExpectedRevision *uint64
	CurrentRevision  uint64
}

type idempotencyRecord struct {
	payloadDigest string
	ready         chan struct{}
	outcome       Outcome
	err           error
}

// Coordinator is the idempotency and revision-precondition store for one
// logical resource family. The zero value is ready to use. It is safe for
// concurrent use by multiple goroutines and multiple transports.
type Coordinator struct {
	mu      sync.Mutex
	records map[string]*idempotencyRecord
}

// NewCoordinator returns an empty idempotency coordinator.
func NewCoordinator() *Coordinator { return &Coordinator{} }

// Do resolves the canonical digest for req, then does exactly one of:
//   - refuses a payload conflict when the key was already reserved or
//     completed under a different payload digest;
//   - replays the previously stored outcome and error for an exact replay
//     (including a replay of an earlier revision conflict or an earlier
//     ambiguous effect error — never a re-execution of effect);
//   - checks the expected-revision precondition (when set) and refuses a
//     stale write without invoking effect;
//   - invokes effect exactly once, stores whatever it returns, and returns
//     that to the caller and to every future replay of this digest.
func (c *Coordinator) Do(ctx context.Context, req Request, effect Effect) (Outcome, error) {
	digest, err := CanonicalDigest(req.Scope, req.HeaderKey, req.MessageKey)
	if err != nil {
		return Outcome{}, err
	}
	payloadDigest := PayloadDigest(req.Payload)

	c.mu.Lock()
	if c.records == nil {
		c.records = make(map[string]*idempotencyRecord)
	}
	rec, exists := c.records[digest]
	if !exists {
		rec = &idempotencyRecord{payloadDigest: payloadDigest, ready: make(chan struct{})}
		c.records[digest] = rec
		c.mu.Unlock()
		return c.execute(ctx, rec, req, effect)
	}
	c.mu.Unlock()

	if rec.payloadDigest != payloadDigest {
		return Outcome{}, &PayloadConflict{Digest: digest}
	}
	<-rec.ready
	return rec.outcome, rec.err
}

// execute runs outside the coordinator's map lock (so a slow effect never
// blocks unrelated keys) but is only ever reached by the single goroutine
// that won the map-insert race for this digest; every other caller for the
// same digest instead waits on rec.ready in Do.
func (c *Coordinator) execute(ctx context.Context, rec *idempotencyRecord, req Request, effect Effect) (Outcome, error) {
	defer close(rec.ready)

	if req.ExpectedRevision != nil {
		expected := *req.ExpectedRevision
		if expected == 0 || req.CurrentRevision == 0 {
			rec.err = ErrRevisionRequired
			return rec.outcome, rec.err
		}
		if expected != req.CurrentRevision {
			rec.err = &RevisionConflict{Current: req.CurrentRevision}
			return rec.outcome, rec.err
		}
	}

	rec.outcome, rec.err = effect(ctx)
	return rec.outcome, rec.err
}
