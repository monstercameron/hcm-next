package custody

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ProviderClass identifies the class of provider that owns a key handle. It
// is a classification only; it is never a provider locator.
type ProviderClass string

const (
	ProviderKMS  ProviderClass = "KMS"
	ProviderHSM  ProviderClass = "HSM"
	ProviderBYOK ProviderClass = "BYOK"

	// Short aliases make the closed set convenient to use without changing
	// the serialized provider-neutral spelling.
	KMS  = ProviderKMS
	HSM  = ProviderHSM
	BYOK = ProviderBYOK
)

// KeyState is the state of a key handle, not of key material.
type KeyState string

const (
	StatePending   KeyState = "PENDING"
	StateActive    KeyState = "ACTIVE"
	StateRotating  KeyState = "ROTATING"
	StateDisabled  KeyState = "DISABLED"
	StateDestroyed KeyState = "DESTROYED"

	Pending   = StatePending
	Active    = StateActive
	Rotating  = StateRotating
	Disabled  = StateDisabled
	Destroyed = StateDestroyed
)

type KeyHandleState = KeyState

// KeyHandle is an opaque, provider-neutral reference for a key lifecycle.
// It contains the provider class, tenant, semantic purpose, and version, but
// no provider locator or key material.
type KeyHandle struct {
	ID            string        `json:"id"`
	ProviderClass ProviderClass `json:"provider_class"`
	Tenant        string        `json:"tenant"`
	Purpose       string        `json:"purpose"`
	Version       string        `json:"version"`
}

func (h KeyHandle) Validate() error {
	if strings.TrimSpace(h.ID) == "" || strings.TrimSpace(h.Tenant) == "" || strings.TrimSpace(h.Purpose) == "" || strings.TrimSpace(h.Version) == "" {
		return fmt.Errorf("%w: id, provider class, tenant, purpose and version are required", ErrInvalidKeyHandle)
	}
	switch h.ProviderClass {
	case ProviderKMS, ProviderHSM, ProviderBYOK:
		return nil
	default:
		return fmt.Errorf("%w: unknown provider class %q", ErrInvalidKeyHandle, h.ProviderClass)
	}
}

type LifecycleOperation string

const (
	LifecycleCreate   LifecycleOperation = "create"
	LifecycleImport   LifecycleOperation = "import"
	LifecycleActivate LifecycleOperation = "activate"
	LifecycleRotate   LifecycleOperation = "rotate"
	LifecycleDisable  LifecycleOperation = "disable"
	LifecycleDestroy  LifecycleOperation = "destroy"
)

// KeyLifecycleEvent is a digest-only audit event. EvidenceDigest is a caller
// supplied digest reference; neither field carries secret or key material.
type KeyLifecycleEvent struct {
	Handle          KeyHandle          `json:"handle"`
	Operation       LifecycleOperation `json:"operation"`
	From            KeyState           `json:"from,omitempty"`
	To              KeyState           `json:"to"`
	EvidenceDigest  string             `json:"evidence_digest,omitempty"`
	AuthorityDigest string             `json:"authority_digest,omitempty"`
	Digest          string             `json:"digest"`
	At              time.Time          `json:"at"`
}

type KeyLifecycle struct {
	Handle    KeyHandle           `json:"handle"`
	State     KeyState            `json:"state"`
	Events    []KeyLifecycleEvent `json:"events"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// BYOKAttestation is an attestation reference for an imported key. The
// attestation is intentionally a digest and expiry, never an attested key.
type BYOKAttestation struct {
	Handle         KeyHandle `json:"handle"`
	EvidenceDigest string    `json:"evidence_digest"`
	AttestedAt     time.Time `json:"attested_at"`
	ValidUntil     time.Time `json:"valid_until"`
}

type BYOKImportRequest struct {
	Handle              KeyHandle       `json:"handle"`
	WrappingProofDigest string          `json:"wrapping_proof_digest"`
	Attestation         BYOKAttestation `json:"attestation"`
}

// RetentionHoldCheck records the declared result of the retention check that
// must precede destruction. A held key cannot be destroyed.
type RetentionHoldCheck struct {
	Declared       bool      `json:"declared"`
	OnHold         bool      `json:"on_hold"`
	EvidenceDigest string    `json:"evidence_digest"`
	CheckedAt      time.Time `json:"checked_at"`
}

type DestroyRequest struct {
	Handle         KeyHandle          `json:"handle"`
	RequestedBy    string             `json:"requested_by"`
	Approver       string             `json:"approver"`
	RetentionCheck RetentionHoldCheck `json:"retention_check"`
}

var (
	ErrInvalidKeyHandle          = errors.New("custody: invalid key handle")
	ErrKeyHandleNotFound         = errors.New("custody: key handle not found")
	ErrKeyHandleExists           = errors.New("custody: key handle already exists")
	ErrInvalidLifecycle          = errors.New("custody: invalid key lifecycle request")
	ErrInvalidTransition         = errors.New("custody: invalid key lifecycle transition")
	ErrKeyHandleDisabled         = errors.New("custody: key handle is disabled")
	ErrKeyHandleDestroyed        = errors.New("custody: key handle is destroyed")
	ErrDistinctApprover          = errors.New("custody: distinct approver is required")
	ErrRetentionHold             = errors.New("custody: retention hold prevents destruction")
	ErrLifecycleEvidenceRequired = errors.New("custody: lifecycle evidence digest is required")
	ErrBYOKProofRequired         = errors.New("custody: BYOK wrapping proof is required")
	ErrBYOKAttestationInvalid    = errors.New("custody: BYOK attestation is invalid")
)

// KeyLifecycleProvider is the additive provider-neutral lifecycle port. It
// receives only opaque references and digest evidence.
type KeyLifecycleProvider interface {
	CreateKeyHandle(Context, KeyHandle) (KeyLifecycle, error)
	ImportBYOK(Context, BYOKImportRequest) (KeyLifecycle, error)
	ActivateKeyHandle(Context, KeyHandle, string) (KeyLifecycle, error)
	RotateKeyHandle(Context, KeyHandle, string) (KeyHandle, error)
	DisableKeyHandle(Context, KeyHandle, string) (KeyLifecycle, error)
	DestroyKeyHandle(Context, DestroyRequest) (KeyLifecycle, error)
	KeyLifecycle(Context, KeyHandle) (KeyLifecycle, error)
}

// LifecycleProvider is a concise alias for adapter declarations.
type LifecycleProvider = KeyLifecycleProvider

type lifecycleRecord struct {
	state  KeyState
	events []KeyLifecycleEvent
}

type lifecycleStore struct {
	mu      sync.Mutex
	records map[KeyHandle]lifecycleRecord
}

var lifecycleStores sync.Map // map[*InMemoryFake]*lifecycleStore

func storeFor(fake *InMemoryFake) *lifecycleStore {
	if value, ok := lifecycleStores.Load(fake); ok {
		return value.(*lifecycleStore)
	}
	created := &lifecycleStore{records: make(map[KeyHandle]lifecycleRecord)}
	actual, _ := lifecycleStores.LoadOrStore(fake, created)
	return actual.(*lifecycleStore)
}

func (f *InMemoryFake) CreateKeyHandle(ctx Context, handle KeyHandle) (KeyLifecycle, error) {
	if err := validateKeyRequest(ctx, handle); err != nil {
		return KeyLifecycle{}, err
	}
	store := storeFor(f)
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.records[handle]; exists {
		return KeyLifecycle{}, fmt.Errorf("%w: %s", ErrKeyHandleExists, handle.ID)
	}
	now := f.clock().UTC()
	event := lifecycleEvent(handle, LifecycleCreate, "", StatePending, "", now)
	store.records[handle] = lifecycleRecord{state: StatePending, events: []KeyLifecycleEvent{event}}
	return lifecycleView(handle, store.records[handle], now), nil
}

func (f *InMemoryFake) ImportBYOK(ctx Context, request BYOKImportRequest) (KeyLifecycle, error) {
	if request.Handle.ProviderClass != ProviderBYOK {
		return KeyLifecycle{}, fmt.Errorf("%w: provider class must be BYOK", ErrInvalidLifecycle)
	}
	if err := validateKeyRequest(ctx, request.Handle); err != nil {
		return KeyLifecycle{}, err
	}
	if strings.TrimSpace(request.WrappingProofDigest) == "" {
		return KeyLifecycle{}, ErrBYOKProofRequired
	}
	if request.Attestation.Handle != request.Handle || strings.TrimSpace(request.Attestation.EvidenceDigest) == "" || request.Attestation.ValidUntil.Before(f.clock().UTC()) {
		return KeyLifecycle{}, ErrBYOKAttestationInvalid
	}
	store := storeFor(f)
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[request.Handle]
	if !ok {
		return KeyLifecycle{}, fmt.Errorf("%w: %s", ErrKeyHandleNotFound, request.Handle.ID)
	}
	if record.state != StatePending {
		return KeyLifecycle{}, fmt.Errorf("%w: import requires PENDING", ErrInvalidTransition)
	}
	now := f.clock().UTC()
	record.events = append(record.events, lifecycleEvent(request.Handle, LifecycleImport, StatePending, StatePending, request.Attestation.EvidenceDigest, now))
	store.records[request.Handle] = record
	return lifecycleView(request.Handle, record, now), nil
}

func (f *InMemoryFake) ActivateKeyHandle(ctx Context, handle KeyHandle, evidenceDigest string) (KeyLifecycle, error) {
	return f.transitionKey(ctx, handle, StateActive, evidenceDigest, LifecycleActivate, StatePending, StateRotating)
}

// RotateKeyHandle marks the current version ROTATING and creates the next
// opaque version in PENDING. Activation is explicit and separately evidenced.
func (f *InMemoryFake) RotateKeyHandle(ctx Context, handle KeyHandle, evidenceDigest string) (KeyHandle, error) {
	if err := validateKeyRequest(ctx, handle); err != nil {
		return KeyHandle{}, err
	}
	if strings.TrimSpace(evidenceDigest) == "" {
		return KeyHandle{}, ErrLifecycleEvidenceRequired
	}
	store := storeFor(f)
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[handle]
	if !ok {
		return KeyHandle{}, fmt.Errorf("%w: %s", ErrKeyHandleNotFound, handle.ID)
	}
	if record.state != StateActive {
		return KeyHandle{}, transitionError(record.state)
	}
	now := f.clock().UTC()
	record.state = StateRotating
	record.events = append(record.events, lifecycleEvent(handle, LifecycleRotate, StateActive, StateRotating, evidenceDigest, now))
	store.records[handle] = record
	next := handle
	next.Version = nextVersion(handle.Version)
	nextRecord := lifecycleRecord{state: StatePending, events: []KeyLifecycleEvent{lifecycleEvent(next, LifecycleRotate, StateRotating, StatePending, evidenceDigest, now)}}
	store.records[next] = nextRecord
	return next, nil
}

func (f *InMemoryFake) DisableKeyHandle(ctx Context, handle KeyHandle, evidenceDigest string) (KeyLifecycle, error) {
	return f.transitionKey(ctx, handle, StateDisabled, evidenceDigest, LifecycleDisable, StateActive, StateRotating)
}

func (f *InMemoryFake) DestroyKeyHandle(ctx Context, request DestroyRequest) (KeyLifecycle, error) {
	if err := validateKeyRequest(ctx, request.Handle); err != nil {
		return KeyLifecycle{}, err
	}
	if strings.TrimSpace(request.RequestedBy) == "" || strings.TrimSpace(request.Approver) == "" || request.RequestedBy == request.Approver {
		return KeyLifecycle{}, ErrDistinctApprover
	}
	check := request.RetentionCheck
	if !check.Declared || check.OnHold || strings.TrimSpace(check.EvidenceDigest) == "" || check.CheckedAt.IsZero() {
		if check.OnHold {
			return KeyLifecycle{}, ErrRetentionHold
		}
		return KeyLifecycle{}, fmt.Errorf("%w: declared, clear, evidenced retention check required", ErrInvalidLifecycle)
	}
	store := storeFor(f)
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[request.Handle]
	if !ok {
		return KeyLifecycle{}, fmt.Errorf("%w: %s", ErrKeyHandleNotFound, request.Handle.ID)
	}
	if record.state == StateDestroyed {
		return KeyLifecycle{}, ErrKeyHandleDestroyed
	}
	now := f.clock().UTC()
	from := record.state
	record.state = StateDestroyed
	authority := sha256.Sum256([]byte(request.RequestedBy + "\x00" + request.Approver))
	record.events = append(record.events, lifecycleEvent(request.Handle, LifecycleDestroy, from, StateDestroyed, check.EvidenceDigest, now, hex.EncodeToString(authority[:])))
	store.records[request.Handle] = record
	return lifecycleView(request.Handle, record, now), nil
}

func (f *InMemoryFake) KeyLifecycle(ctx Context, handle KeyHandle) (KeyLifecycle, error) {
	if err := validateKeyRequest(ctx, handle); err != nil {
		return KeyLifecycle{}, err
	}
	store := storeFor(f)
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[handle]
	if !ok {
		return KeyLifecycle{}, fmt.Errorf("%w: %s", ErrKeyHandleNotFound, handle.ID)
	}
	return lifecycleView(handle, record, f.clock().UTC()), nil
}

func (f *InMemoryFake) transitionKey(ctx Context, handle KeyHandle, to KeyState, evidence string, operation LifecycleOperation, allowed ...KeyState) (KeyLifecycle, error) {
	if err := validateKeyRequest(ctx, handle); err != nil {
		return KeyLifecycle{}, err
	}
	if strings.TrimSpace(evidence) == "" {
		return KeyLifecycle{}, ErrLifecycleEvidenceRequired
	}
	store := storeFor(f)
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[handle]
	if !ok {
		return KeyLifecycle{}, fmt.Errorf("%w: %s", ErrKeyHandleNotFound, handle.ID)
	}
	for _, state := range allowed {
		if record.state == state {
			now := f.clock().UTC()
			from := record.state
			record.state = to
			record.events = append(record.events, lifecycleEvent(handle, operation, from, to, evidence, now))
			store.records[handle] = record
			return lifecycleView(handle, record, now), nil
		}
	}
	return KeyLifecycle{}, transitionError(record.state)
}

func validateKeyRequest(ctx Context, handle KeyHandle) error {
	if err := ctx.Validate(); err != nil {
		return err
	}
	if err := handle.Validate(); err != nil {
		return err
	}
	if ctx.Tenant != handle.Tenant || ctx.Purpose != handle.Purpose {
		return fmt.Errorf("%w: context tenant or purpose does not match handle", ErrInvalidLifecycle)
	}
	return nil
}

func transitionError(state KeyState) error {
	switch state {
	case StateDisabled:
		return ErrKeyHandleDisabled
	case StateDestroyed:
		return ErrKeyHandleDestroyed
	default:
		return fmt.Errorf("%w: current state %s", ErrInvalidTransition, state)
	}
}

func lifecycleView(handle KeyHandle, record lifecycleRecord, now time.Time) KeyLifecycle {
	events := append([]KeyLifecycleEvent(nil), record.events...)
	return KeyLifecycle{Handle: handle, State: record.state, Events: events, UpdatedAt: now}
}

func lifecycleEvent(handle KeyHandle, operation LifecycleOperation, from, to KeyState, evidence string, at time.Time, authority ...string) KeyLifecycleEvent {
	authorityDigest := ""
	if len(authority) != 0 {
		authorityDigest = authority[0]
	}
	canonical := strings.Join([]string{handle.ID, string(handle.ProviderClass), handle.Tenant, handle.Purpose, handle.Version, string(operation), string(from), string(to), evidence, authorityDigest, at.UTC().Format(time.RFC3339Nano)}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return KeyLifecycleEvent{Handle: handle, Operation: operation, From: from, To: to, EvidenceDigest: evidence, AuthorityDigest: authorityDigest, Digest: hex.EncodeToString(digest[:]), At: at.UTC()}
}

// Version identifies the additive lifecycle contract version.
func Version() int { return 1 }

// Explain describes the digest-only key-handle lifecycle boundary.
func Explain() string {
	return "Provider-neutral KMS/HSM/BYOK key handles move through digested PENDING, ACTIVE, ROTATING, DISABLED, and DESTROYED events without exporting key material."
}
