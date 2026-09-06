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

// Lifecycle is the metadata-only view returned by the typed custody ports.
// It is safe to persist and contains no key, secret, certificate, provider
// locator, or private material.
type Lifecycle struct {
	Handle       Handle    `json:"handle"`
	Status       string    `json:"status"`
	Algorithm    string    `json:"algorithm,omitempty"`
	PublicDigest string    `json:"public_digest,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const (
	StatusActive  = "ACTIVE"
	StatusRotated = "ROTATED"
	StatusRevoked = "REVOKED"
)

// Attestation is a provider-neutral proof about a referenced object. It
// binds only metadata and a digest; raw material never crosses this port.
type Attestation struct {
	Handle     Handle    `json:"handle"`
	Status     string    `json:"status"`
	Digest     string    `json:"digest"`
	AttestedAt time.Time `json:"attested_at"`
	ValidUntil time.Time `json:"valid_until"`
}

// MetadataCustody is the common lifecycle port for a key, secret, or
// certificate provider. Get accepts only an opaque Handle reference and
// returns metadata only. Rotate, Revoke, and Attest likewise return no raw
// material.
type MetadataCustody interface {
	Get(Context, Handle) (Lifecycle, error)
	Rotate(Context, Handle) (Lifecycle, Receipt, error)
	Revoke(Context, Handle, string) (Receipt, error)
	Attest(Context, Handle) (Attestation, error)
}

// KeyCustody is the provider-neutral key custody interface.
type KeyCustody interface{ MetadataCustody }

// SecretCustody is the provider-neutral secret custody interface.
type SecretCustody interface{ MetadataCustody }

// CertificateCustody is the provider-neutral certificate custody interface.
type CertificateCustody interface{ MetadataCustody }

// TypedProvider is an adapter that can serve all three custody domains.
type TypedProvider interface {
	KeyCustody
	SecretCustody
	CertificateCustody
}

// ProviderAdapter is an alias for adapters that implement one typed custody
// domain. It is useful in conformance helpers and documents the intended port.
type ProviderAdapter = MetadataCustody

var (
	ErrInvalidTypedRequest = errors.New("custody: invalid typed custody request")
	ErrObjectNotFound      = errors.New("custody: referenced object not found")
	ErrObjectRevoked       = errors.New("custody: referenced object is revoked")
	ErrWrongObjectKind     = errors.New("custody: handle kind does not match typed port")
)

// InMemoryFake is a metadata-only fake implementing KeyCustody,
// SecretCustody, and CertificateCustody. It intentionally has no field that
// can hold raw material; tests register opaque Handles and exercise lifecycle
// semantics through the same port an external adapter must satisfy.
type InMemoryFake struct {
	mu      sync.Mutex
	objects map[Handle]Lifecycle
	clock   func() time.Time
}

// NewInMemoryFake creates an empty metadata-only fake. now is optional and is
// useful for deterministic conformance tests.
func NewInMemoryFake(now func() time.Time) *InMemoryFake {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryFake{objects: make(map[Handle]Lifecycle), clock: now}
}

// NewFakeProvider is an alias for the in-memory provider name commonly used
// by adapter tests.
func NewFakeProvider(now func() time.Time) *InMemoryFake { return NewInMemoryFake(now) }

// Register adds one opaque reference to the fake. The reference is the only
// input needed; no value can be registered.
func (f *InMemoryFake) Register(ref Handle) error {
	if f == nil {
		return ErrInvalidTypedRequest
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	now := f.clock().UTC()
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.objects[ref]; exists {
		return fmt.Errorf("%w: %s", ErrInvalidTypedRequest, ref.ID)
	}
	f.objects[ref] = Lifecycle{Handle: ref, Status: StatusActive, UpdatedAt: now}
	return nil
}

// Get returns metadata for ref and never returns material.
func (f *InMemoryFake) Get(ctx Context, ref Handle) (Lifecycle, error) {
	if err := f.validate(ctx, ref); err != nil {
		return Lifecycle{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	lifecycle, ok := f.objects[ref]
	if !ok {
		return Lifecycle{}, fmt.Errorf("%w: %s", ErrObjectNotFound, ref.ID)
	}
	if lifecycle.Status == StatusRevoked {
		return Lifecycle{}, ErrObjectRevoked
	}
	return lifecycle, nil
}

// Rotate creates a new version of the same opaque object kind and retires
// the old reference. The version is metadata only.
func (f *InMemoryFake) Rotate(ctx Context, ref Handle) (Lifecycle, Receipt, error) {
	if err := f.validate(ctx, ref); err != nil {
		return Lifecycle{}, Receipt{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	old, ok := f.objects[ref]
	if !ok {
		return Lifecycle{}, Receipt{}, fmt.Errorf("%w: %s", ErrObjectNotFound, ref.ID)
	}
	if old.Status == StatusRevoked {
		return Lifecycle{}, Receipt{}, ErrObjectRevoked
	}
	now := f.clock().UTC()
	old.Status = StatusRotated
	old.UpdatedAt = now
	f.objects[ref] = old
	nextHandle := ref
	nextHandle.Version = nextVersion(ref.Version)
	next := Lifecycle{Handle: nextHandle, Status: StatusActive, Algorithm: old.Algorithm, PublicDigest: old.PublicDigest, UpdatedAt: now}
	f.objects[nextHandle] = next
	return next, f.receipt(ctx, nextHandle, Rotate, now), nil
}

// Revoke permanently revokes ref and returns a metadata-only receipt.
func (f *InMemoryFake) Revoke(ctx Context, ref Handle, reason string) (Receipt, error) {
	if err := f.validate(ctx, ref); err != nil {
		return Receipt{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return Receipt{}, fmt.Errorf("%w: revoke reason is required", ErrInvalidTypedRequest)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	lifecycle, ok := f.objects[ref]
	if !ok {
		return Receipt{}, fmt.Errorf("%w: %s", ErrObjectNotFound, ref.ID)
	}
	if lifecycle.Status == StatusRevoked {
		return Receipt{}, ErrObjectRevoked
	}
	now := f.clock().UTC()
	lifecycle.Status = StatusRevoked
	lifecycle.UpdatedAt = now
	f.objects[ref] = lifecycle
	return f.receipt(ctx, ref, Revoke, now), nil
}

// Attest returns a deterministic digest over metadata only.
func (f *InMemoryFake) Attest(ctx Context, ref Handle) (Attestation, error) {
	lifecycle, err := f.Get(ctx, ref)
	if err != nil {
		return Attestation{}, err
	}
	now := f.clock().UTC()
	canonical := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", lifecycle.Handle.ID, lifecycle.Handle.Version, lifecycle.Handle.Tenant, lifecycle.Handle.Region, lifecycle.Status)
	sum := sha256.Sum256([]byte(canonical))
	return Attestation{Handle: lifecycle.Handle, Status: lifecycle.Status, Digest: hex.EncodeToString(sum[:]), AttestedAt: now, ValidUntil: now.Add(time.Minute)}, nil
}

func (f *InMemoryFake) validate(ctx Context, ref Handle) error {
	if f == nil {
		return ErrInvalidTypedRequest
	}
	if err := ctx.Validate(); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	if ctx.Tenant != ref.Tenant || ctx.Region != ref.Region {
		return fmt.Errorf("%w: context scope does not match handle", ErrInvalidTypedRequest)
	}
	return nil
}

func (f *InMemoryFake) receipt(ctx Context, handle Handle, operation Operation, at time.Time) Receipt {
	return Receipt{ID: fmt.Sprintf("fake:%s:%s:%d", operation, handle.ID, at.UnixNano()), Handle: handle, Operation: operation, ContextDigest: ContextDigest(ctx.RequestContext), At: at}
}

func nextVersion(version string) string {
	var number int
	if _, err := fmt.Sscanf(version, "v%d", &number); err == nil {
		return fmt.Sprintf("v%d", number+1)
	}
	return version + ".rotated"
}

var (
	_ KeyCustody         = (*InMemoryFake)(nil)
	_ SecretCustody      = (*InMemoryFake)(nil)
	_ CertificateCustody = (*InMemoryFake)(nil)
	_ TypedProvider      = (*InMemoryFake)(nil)
)
