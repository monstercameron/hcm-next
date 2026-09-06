// Package lease implements TRUST-016: destination-scoped credential leases.
//
// A [CredentialLease] authorizes exactly one bounded use of a custody object
// against exactly one destination, for exactly one custody operation, within
// a fixed time window, by exactly one workload, tenant and purpose. It is
// minted through the provider-neutral custody [Port] (see
// [internal/trust/custody]) rather than by handing out credential material
// directly - the same "resolve/lease at use time, never a bare value"
// discipline [internal/trust/secrets] applies to secret references, applied
// here to the lease itself, which [internal/trust/outbound] then presents
// alongside an outbound dispatch so an egress gateway can refuse a lease
// minted for one destination being used against another.
//
// [Manager.Mint] binds a lease to its destination, tenant, purpose and a
// single-use nonce and records mint evidence. [Manager.Use] verifies the
// presented lease matches what was minted bit-for-bit, has not expired or
// been revoked, has not already been consumed, and is being presented to the
// exact destination and for the exact operation it was minted for - then
// consumes it, so a second presentation of the same lease is refused even
// though nothing about it changed. [Manager.Revoke] ends a lease's usability
// immediately and idempotently. Every one of those three calls appends
// durable [Evidence]; nothing here ever deletes an evidence record.
package lease

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

// Errors. All are matchable with errors.Is. ErrInvalidRequest and
// ErrInvalidLease are configuration/shape failures; the rest are the refusal
// outcomes TRUST-016 requires at use time.
var (
	ErrInvalidRequest = errors.New("lease: invalid credential lease request")
	ErrInvalidLease   = errors.New("lease: invalid credential lease")
	// ErrExpired is returned when Use is presented a lease past its ExpiresAt.
	ErrExpired = errors.New("lease: credential lease expired")
	// ErrRevoked is returned when Use is presented a lease Revoke already ended.
	ErrRevoked = errors.New("lease: credential lease revoked")
	// ErrAlreadyUsed is returned when Use is presented a lease whose single-use
	// nonce was already consumed by an earlier, successful Use.
	ErrAlreadyUsed = errors.New("lease: single-use nonce already consumed")
	// ErrDestinationMismatch is returned when the destination Use is asked to
	// dispatch to does not equal the destination the lease was minted for.
	ErrDestinationMismatch = errors.New("lease: destination does not match the lease's bound destination")
	// ErrOperationNotPermitted is returned when the operation Use is asked to
	// perform does not equal the operation the lease was minted for.
	ErrOperationNotPermitted = errors.New("lease: operation is not the one this lease was minted for")
	// ErrUnknown is returned when a lease id has no record: never minted, or a
	// forged id.
	ErrUnknown = errors.New("lease: no such credential lease")
	// ErrTampered is returned when a presented lease's fields do not match the
	// record Mint produced for its id - a caller mutated a field (most likely
	// Destination or Operation) after receiving it.
	ErrTampered = errors.New("lease: presented lease does not match the minted lease")
)

// administrativeOperations are custody capabilities a credential lease may
// never be minted for: Rotate and Revoke manage the custody object itself,
// they are not something an outbound dispatch performs, and granting them
// through a destination-scoped lease would let a lease "over-broad" its way
// into custody administration.
var administrativeOperations = map[custody.Operation]bool{
	custody.Rotate: true,
	custody.Revoke: true,
}

// Port is the provider-neutral custody surface a [Manager] mints leases
// through. It is satisfied by [custody.Provider]; a [Manager] never sees any
// other custody capability.
type Port interface {
	IssueLease(ctx custody.Context, object custody.Handle, operation custody.Operation, ttl time.Duration) (custody.Lease, error)
}

// Request is what a caller asks [Manager.Mint] for: one bounded,
// destination-scoped credential lease. Every field is required; there is no
// wildcard value for Operation, Destination, Tenant, Workload or Purpose.
type Request struct {
	Handle      custody.Handle
	Workload    string
	Tenant      string
	Purpose     string
	Destination string
	Operation   custody.Operation
	TTL         time.Duration
}

func (r Request) validate() error {
	if err := r.Handle.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	for _, item := range []struct{ name, value string }{
		{"workload", r.Workload}, {"tenant", r.Tenant}, {"purpose", r.Purpose}, {"destination", r.Destination},
	} {
		if strings.TrimSpace(item.value) == "" || strings.TrimSpace(item.value) != item.value {
			return fmt.Errorf("%w: %s is empty or padded", ErrInvalidRequest, item.name)
		}
	}
	if r.Tenant != r.Handle.Tenant {
		return fmt.Errorf("%w: tenant %q does not match handle tenant %q", ErrInvalidRequest, r.Tenant, r.Handle.Tenant)
	}
	if r.Operation == "" {
		return fmt.Errorf("%w: no operation - an unscoped lease is over-broad, not narrow", ErrInvalidRequest)
	}
	if administrativeOperations[r.Operation] {
		return fmt.Errorf("%w: operation %q is a custody-administration capability, not grantable through a credential lease", ErrInvalidRequest, r.Operation)
	}
	if r.TTL <= 0 {
		return fmt.Errorf("%w: ttl must be positive", ErrInvalidRequest)
	}
	return nil
}

// CredentialLease is one minted, destination-scoped authorization. It
// carries no custody material - only the binding coordinates and a
// single-use nonce that Use requires an exact match on before it will
// consume the lease.
type CredentialLease struct {
	ID             string
	CustodyLeaseID string
	Handle         custody.Handle
	Workload       string
	Tenant         string
	Purpose        string
	Destination    string
	Operation      custody.Operation
	Nonce          string
	IssuedAt       time.Time
	ExpiresAt      time.Time
}

// matches reports whether every field of other equals l - i.e. other is
// bit-for-bit the lease Mint produced, not a caller-mutated copy that merely
// shares an ID.
func (l CredentialLease) matches(other CredentialLease) bool {
	return l.ID == other.ID && l.CustodyLeaseID == other.CustodyLeaseID && l.Handle == other.Handle &&
		l.Workload == other.Workload && l.Tenant == other.Tenant && l.Purpose == other.Purpose &&
		l.Destination == other.Destination && l.Operation == other.Operation && l.Nonce == other.Nonce &&
		l.IssuedAt.Equal(other.IssuedAt) && l.ExpiresAt.Equal(other.ExpiresAt)
}

// EventKind names which lifecycle event an [Evidence] record describes.
type EventKind string

const (
	EventMint   EventKind = "mint"
	EventUse    EventKind = "use"
	EventRevoke EventKind = "revoke"
)

// Evidence is the durable record [Manager] appends on every mint, use and
// revoke - both granted and denied. It never carries the nonce: a denial
// evidence record is safe to persist and inspect without itself becoming a
// bearer credential.
type Evidence struct {
	LeaseID     string
	Kind        EventKind
	Destination string
	Tenant      string
	Workload    string
	Purpose     string
	Operation   custody.Operation
	At          time.Time
	Outcome     string // "granted" or "denied"
	Reason      string
}

// String renders a deterministic, redaction-safe one-line summary.
func (e Evidence) String() string {
	return fmt.Sprintf("lease %s lease=%s destination=%s operation=%s outcome=%s reason=%s",
		e.Kind, e.LeaseID, e.Destination, e.Operation, e.Outcome, e.Reason)
}

const (
	outcomeGranted = "granted"
	outcomeDenied  = "denied"
)

type record struct {
	lease   CredentialLease
	used    bool
	revoked bool
}

// Manager mints, verifies-and-consumes, and revokes [CredentialLease] values,
// and accumulates their [Evidence] trail. A Manager owns the only state a
// credential lease has once minted; nothing in this package derives a
// lease's validity from wall-clock time alone the way the caller's own
// records might drift from what actually happened.
type Manager struct {
	port Port
	now  func() time.Time

	mu     sync.Mutex
	leases map[string]*record
	events []Evidence
}

// NewManager builds a Manager over a custody [Port]. now defaults to
// time.Now in UTC when nil.
func NewManager(port Port, now func() time.Time) (*Manager, error) {
	if port == nil {
		return nil, fmt.Errorf("%w: no custody port", ErrInvalidRequest)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{port: port, now: now, leases: make(map[string]*record)}, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("lease: generating random value: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Mint validates req, mints the underlying custody lease through the Port,
// and wraps it in a destination-scoped [CredentialLease] with a fresh
// single-use nonce. It records and returns mint [Evidence] on both success
// and every refusal it can attribute to a specific lease id (a request-shape
// failure that never reached the Port records no evidence, since there is no
// lease id yet to attribute it to).
func (m *Manager) Mint(req Request) (CredentialLease, Evidence, error) {
	if err := req.validate(); err != nil {
		return CredentialLease{}, Evidence{}, err
	}

	rctx := custody.RequestContext{
		Workload: req.Workload, Tenant: req.Handle.Tenant, Region: req.Handle.Region,
		Purpose: req.Purpose, Destination: req.Destination,
	}
	if err := rctx.Validate(); err != nil {
		return CredentialLease{}, Evidence{}, err
	}

	custodyLease, err := m.port.IssueLease(custody.Context{RequestContext: rctx}, req.Handle, req.Operation, req.TTL)
	if err != nil {
		return CredentialLease{}, Evidence{}, err
	}
	now := m.now().UTC()
	if err := custodyLease.Validate(now); err != nil {
		return CredentialLease{}, Evidence{}, err
	}
	if custodyLease.Handle != req.Handle || custodyLease.Operation != req.Operation {
		// Defence in depth: a provider that widened or redirected the
		// underlying lease is not trusted to have honoured the bound request.
		return CredentialLease{}, Evidence{}, fmt.Errorf("%w: custody provider returned a lease outside the requested scope", ErrInvalidLease)
	}

	id, err := randomHex(12)
	if err != nil {
		return CredentialLease{}, Evidence{}, err
	}
	nonce, err := randomHex(16)
	if err != nil {
		return CredentialLease{}, Evidence{}, err
	}

	cl := CredentialLease{
		ID:             "clx-" + id,
		CustodyLeaseID: custodyLease.ID,
		Handle:         req.Handle,
		Workload:       req.Workload,
		Tenant:         req.Tenant,
		Purpose:        req.Purpose,
		Destination:    req.Destination,
		Operation:      req.Operation,
		Nonce:          nonce,
		IssuedAt:       now,
		ExpiresAt:      custodyLease.ExpiresAt.UTC(),
	}

	m.mu.Lock()
	m.leases[cl.ID] = &record{lease: cl}
	ev := Evidence{
		LeaseID: cl.ID, Kind: EventMint, Destination: cl.Destination, Tenant: cl.Tenant,
		Workload: cl.Workload, Purpose: cl.Purpose, Operation: cl.Operation, At: now, Outcome: outcomeGranted,
	}
	m.events = append(m.events, ev)
	m.mu.Unlock()

	return cl, ev, nil
}

// deny appends a denied use [Evidence] record and returns it. Callers must
// hold m.mu.
func (m *Manager) deny(leaseID, destination string, operation custody.Operation, now time.Time, reason string) Evidence {
	ev := Evidence{LeaseID: leaseID, Kind: EventUse, Destination: destination, Operation: operation, At: now, Outcome: outcomeDenied, Reason: reason}
	m.events = append(m.events, ev)
	return ev
}

// Use verifies presented against the record Mint produced for its id, then
// - only if every check passes - consumes the lease's single-use nonce so no
// later call to Use can succeed against the same lease again. destination
// and operation are the caller's actual dispatch coordinates, checked
// against what the lease was minted for; presented itself is checked
// field-for-field against the stored record so a caller cannot widen a
// lease by mutating its own copy before presenting it.
//
// Checks run in a fixed order - tamper, revoked, expired, already-used,
// destination, operation - so the first-fired reason is deterministic; every
// outcome, granted or denied, is recorded as [Evidence] before Use returns.
func (m *Manager) Use(presented CredentialLease, destination string, operation custody.Operation) (Evidence, error) {
	now := m.now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.leases[presented.ID]
	if !ok {
		return m.deny(presented.ID, destination, operation, now, "unknown_lease"), fmt.Errorf("%w: %s", ErrUnknown, presented.ID)
	}
	if !rec.lease.matches(presented) {
		return m.deny(rec.lease.ID, destination, operation, now, "tampered"), ErrTampered
	}
	if rec.revoked {
		return m.deny(rec.lease.ID, destination, operation, now, "revoked"), ErrRevoked
	}
	if now.After(rec.lease.ExpiresAt) {
		return m.deny(rec.lease.ID, destination, operation, now, "expired"), ErrExpired
	}
	if rec.used {
		return m.deny(rec.lease.ID, destination, operation, now, "already_used"), ErrAlreadyUsed
	}
	if rec.lease.Destination != destination {
		return m.deny(rec.lease.ID, destination, operation, now, "destination_mismatch"), ErrDestinationMismatch
	}
	if rec.lease.Operation != operation {
		return m.deny(rec.lease.ID, destination, operation, now, "operation_not_permitted"), ErrOperationNotPermitted
	}

	rec.used = true
	ev := Evidence{
		LeaseID: rec.lease.ID, Kind: EventUse, Destination: destination, Tenant: rec.lease.Tenant,
		Workload: rec.lease.Workload, Purpose: rec.lease.Purpose, Operation: operation, At: now, Outcome: outcomeGranted,
	}
	m.events = append(m.events, ev)
	return ev, nil
}

// Revoke ends leaseID's usability immediately. It is idempotent: revoking an
// already-revoked lease succeeds and records another revoke [Evidence]
// entry rather than erroring, since a caller retrying a revoke after an
// uncertain earlier attempt must not have to first find out whether the
// first attempt landed.
func (m *Manager) Revoke(leaseID, reason string) (Evidence, error) {
	now := m.now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.leases[leaseID]
	if !ok {
		return Evidence{}, fmt.Errorf("%w: %s", ErrUnknown, leaseID)
	}
	rec.revoked = true
	ev := Evidence{
		LeaseID: rec.lease.ID, Kind: EventRevoke, Destination: rec.lease.Destination, Tenant: rec.lease.Tenant,
		Workload: rec.lease.Workload, Purpose: rec.lease.Purpose, Operation: rec.lease.Operation, At: now,
		Outcome: outcomeGranted, Reason: reason,
	}
	m.events = append(m.events, ev)
	return ev, nil
}

// Events returns a copy of every [Evidence] record accumulated so far, mint,
// use and revoke, granted and denied, in the order they occurred.
func (m *Manager) Events() []Evidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Evidence, len(m.events))
	copy(out, m.events)
	return out
}
