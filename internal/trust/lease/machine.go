package lease

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

var (
	ErrEpochStale      = errors.New("lease: credential lease revocation epoch is stale")
	ErrMachineUnknown  = errors.New("lease: unknown machine credential lease")
	ErrInvalidEpoch    = errors.New("lease: invalid revocation epoch request")
	ErrEpochRegression = errors.New("lease: revocation epoch must increase monotonically")
)

// MachineRequest is an alias of the existing scoped Request. Its Workload
// field is the canonical machine/workload identity, so callers can reuse
// existing lease request values without a second request shape.
type MachineRequest = Request

// MachineCredentialLease wraps the existing destination-scoped lease with
// the tenant revocation epoch captured at mint time. It contains no secret
// material and remains reference-only.
type MachineCredentialLease struct {
	Lease            CredentialLease
	WorkloadIdentity string
	Tenant           string
	Epoch            uint64
	RevocationEpoch  uint64
}

// EpochEvent is the append-only, digest-linked record for a tenant epoch
// bump. Digest includes the previous digest, so an event cannot be silently
// replaced without changing every subsequent digest.
type EpochEvent struct {
	Tenant         string
	PreviousEpoch  uint64
	Epoch          uint64
	PreviousDigest string
	Reason         string
	At             time.Time
	Digest         string
}

// MachineEvidence is the evidence returned by machine-lease operations. It
// intentionally contains coordinates and epoch metadata only.
type MachineEvidence struct {
	Kind             EventKind
	LeaseID          string
	Tenant           string
	WorkloadIdentity string
	Destination      string
	Operation        custody.Operation
	Epoch            uint64
	EpochDigest      string
	At               time.Time
	Outcome          string
	Reason           string
}

type machineRecord struct {
	lease MachineCredentialLease
}

type epochState struct {
	value  uint64
	digest string
}

// MachineManager composes TRUST-016's Manager and adds tenant-scoped
// revocation epochs. Existing destination, operation, expiry, single-use,
// and custody checks remain in the composed Manager.
type MachineManager struct {
	base *Manager
	now  func() time.Time

	mu          sync.Mutex
	epochs      map[string]epochState
	leases      map[string]machineRecord
	events      []MachineEvidence
	epochEvents []EpochEvent
}

// NewMachineManager creates a machine-credential manager over the same
// custody Port used by Manager.
func NewMachineManager(port Port, now func() time.Time) (*MachineManager, error) {
	base, err := NewManager(port, now)
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MachineManager{base: base, now: now, epochs: make(map[string]epochState), leases: make(map[string]machineRecord)}, nil
}

// CurrentEpoch returns the current tenant epoch. Tenants begin at epoch 1;
// an unknown tenant has no epoch until its first machine lease or bump.
func (m *MachineManager) CurrentEpoch(tenant string) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.epochs[tenant].value
}

// BumpEpoch invalidates all machine leases minted before the next epoch and
// appends a digest-linked event. The counter is monotonic and starts at one.
func (m *MachineManager) BumpEpoch(tenant, reason string) (EpochEvent, MachineEvidence, error) {
	if m == nil || strings.TrimSpace(tenant) == "" || tenant != strings.TrimSpace(tenant) || strings.TrimSpace(reason) == "" {
		return EpochEvent{}, MachineEvidence{}, ErrInvalidEpoch
	}
	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.epochs[tenant]
	next := state.value + 1
	if next <= state.value {
		return EpochEvent{}, MachineEvidence{}, ErrEpochRegression
	}
	digestInput := fmt.Sprintf("machine-epoch/v1\x00%s\x00%d\x00%d\x00%s\x00%s\x00%s", tenant, state.value, next, state.digest, reason, now.Format(time.RFC3339Nano))
	digest := sha256.Sum256([]byte(digestInput))
	event := EpochEvent{Tenant: tenant, PreviousEpoch: state.value, Epoch: next, PreviousDigest: state.digest, Reason: reason, At: now, Digest: hex.EncodeToString(digest[:])}
	m.epochs[tenant] = epochState{value: next, digest: event.Digest}
	m.epochEvents = append(m.epochEvents, event)
	ev := MachineEvidence{Kind: EventRevoke, Tenant: tenant, Epoch: next, EpochDigest: event.Digest, At: now, Outcome: outcomeGranted, Reason: "revocation_epoch_bump"}
	m.events = append(m.events, ev)
	return event, ev, nil
}

// Mint issues a short-lived machine lease at the current tenant epoch.
func (m *MachineManager) Mint(req MachineRequest) (MachineCredentialLease, MachineEvidence, error) {
	if strings.TrimSpace(req.Workload) == "" {
		return MachineCredentialLease{}, MachineEvidence{}, fmt.Errorf("%w: workload identity is required", ErrInvalidRequest)
	}
	if err := req.validate(); err != nil {
		return MachineCredentialLease{}, MachineEvidence{}, err
	}
	m.mu.Lock()
	state := m.epochs[req.Tenant]
	if state.value == 0 {
		state.value = 1
		m.epochs[req.Tenant] = state
	}
	epoch := state.value
	digest := state.digest
	m.mu.Unlock()
	cl, _, err := m.base.Mint(req)
	if err != nil {
		return MachineCredentialLease{}, MachineEvidence{}, err
	}
	result := MachineCredentialLease{Lease: cl, WorkloadIdentity: req.Workload, Tenant: req.Tenant, Epoch: epoch, RevocationEpoch: epoch}
	now := m.now().UTC()
	ev := MachineEvidence{Kind: EventMint, LeaseID: cl.ID, Tenant: cl.Tenant, WorkloadIdentity: req.Workload, Destination: cl.Destination, Operation: cl.Operation, Epoch: epoch, EpochDigest: digest, At: now, Outcome: outcomeGranted}
	m.mu.Lock()
	m.leases[cl.ID] = machineRecord{lease: result}
	m.events = append(m.events, ev)
	m.mu.Unlock()
	return result, ev, nil
}

// Use refuses a machine lease if its captured epoch is older than the
// current tenant epoch, then delegates the remaining exact-scope and
// single-use checks to TRUST-016 Manager.Use.
func (m *MachineManager) Use(presented MachineCredentialLease, destination string, operation custody.Operation) (MachineEvidence, error) {
	if m == nil {
		return MachineEvidence{}, ErrMachineUnknown
	}
	now := m.now().UTC()
	m.mu.Lock()
	rec, ok := m.leases[presented.Lease.ID]
	if !ok {
		ev := MachineEvidence{Kind: EventUse, LeaseID: presented.Lease.ID, Destination: destination, Operation: operation, At: now, Outcome: outcomeDenied, Reason: "unknown_machine_lease"}
		m.events = append(m.events, ev)
		m.mu.Unlock()
		return ev, fmt.Errorf("%w: %s", ErrMachineUnknown, presented.Lease.ID)
	}
	if rec.lease != presented {
		ev := MachineEvidence{Kind: EventUse, LeaseID: rec.lease.Lease.ID, Tenant: rec.lease.Tenant, WorkloadIdentity: rec.lease.WorkloadIdentity, Destination: destination, Operation: operation, Epoch: rec.lease.RevocationEpoch, At: now, Outcome: outcomeDenied, Reason: "tampered"}
		m.events = append(m.events, ev)
		m.mu.Unlock()
		return ev, ErrTampered
	}
	current := m.epochs[rec.lease.Tenant]
	if rec.lease.RevocationEpoch < current.value {
		ev := MachineEvidence{Kind: EventUse, LeaseID: rec.lease.Lease.ID, Tenant: rec.lease.Tenant, WorkloadIdentity: rec.lease.WorkloadIdentity, Destination: destination, Operation: operation, Epoch: rec.lease.RevocationEpoch, EpochDigest: current.digest, At: now, Outcome: outcomeDenied, Reason: "revocation_epoch"}
		m.events = append(m.events, ev)
		m.mu.Unlock()
		return ev, ErrEpochStale
	}
	currentDigest := current.digest
	m.mu.Unlock()
	_, err := m.base.Use(presented.Lease, destination, operation)
	ev := MachineEvidence{Kind: EventUse, LeaseID: presented.Lease.ID, Tenant: presented.Tenant, WorkloadIdentity: presented.WorkloadIdentity, Destination: destination, Operation: operation, Epoch: presented.RevocationEpoch, EpochDigest: currentDigest, At: now}
	if err != nil {
		ev.Outcome = outcomeDenied
		ev.Reason = err.Error()
	} else {
		ev.Outcome = outcomeGranted
	}
	m.mu.Lock()
	m.events = append(m.events, ev)
	m.mu.Unlock()
	return ev, err
}

// Revoke ends one machine lease immediately through the existing lease
// manager and appends machine evidence. Epoch bumps remain the tenant-wide
// emergency revocation mechanism.
func (m *MachineManager) Revoke(leaseID, reason string) (MachineEvidence, error) {
	if m == nil {
		return MachineEvidence{}, ErrMachineUnknown
	}
	now := m.now().UTC()
	m.mu.Lock()
	rec, ok := m.leases[leaseID]
	m.mu.Unlock()
	if !ok {
		return MachineEvidence{}, fmt.Errorf("%w: %s", ErrMachineUnknown, leaseID)
	}
	_, err := m.base.Revoke(leaseID, reason)
	ev := MachineEvidence{Kind: EventRevoke, LeaseID: leaseID, Tenant: rec.lease.Tenant, WorkloadIdentity: rec.lease.WorkloadIdentity, Destination: rec.lease.Lease.Destination, Operation: rec.lease.Lease.Operation, Epoch: rec.lease.RevocationEpoch, At: now, Outcome: outcomeGranted, Reason: reason}
	if err != nil {
		ev.Outcome = outcomeDenied
		ev.Reason = err.Error()
	}
	m.mu.Lock()
	m.events = append(m.events, ev)
	m.mu.Unlock()
	return ev, err
}

// RevokeLease is the value-oriented alias for Revoke.
func (m *MachineManager) RevokeLease(presented MachineCredentialLease, reason string) (MachineEvidence, error) {
	return m.Revoke(presented.Lease.ID, reason)
}

// EpochEvents returns a copy of all digest-linked epoch bump events.
func (m *MachineManager) EpochEvents() []EpochEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]EpochEvent(nil), m.epochEvents...)
}

// Events returns a copy of all machine mint, use, and epoch evidence.
func (m *MachineManager) Events() []MachineEvidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MachineEvidence(nil), m.events...)
}
