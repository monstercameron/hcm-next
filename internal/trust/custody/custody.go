// Package custody defines the provider-neutral port for cryptographic and
// credential custody. Implementations keep key and secret material in their
// provider; callers receive handles, ciphertext, signatures, and receipts.
//
// The port deliberately has no GetSecret/GetKey operation. A lease authorizes
// a bounded provider operation, but does not expose its credential to the
// application, logs, configuration, or evidence.
package custody

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Kind string

const (
	Secret      Kind = "SECRET"
	Key         Kind = "KEY"
	Certificate Kind = "CERTIFICATE"
)

type Operation string

const (
	Encrypt        Operation = "encrypt"
	Decrypt        Operation = "decrypt"
	Sign           Operation = "sign"
	Verify         Operation = "verify"
	LeaseOperation Operation = "lease"
	Rotate         Operation = "rotate"
	Revoke         Operation = "revoke"
)

var (
	ErrInvalidHandle  = errors.New("custody: invalid handle")
	ErrInvalidContext = errors.New("custody: invalid request context")
	ErrInvalidLease   = errors.New("custody: invalid lease")
	ErrDenied         = errors.New("custody: operation denied")
	ErrExpired        = errors.New("custody: lease expired")
)

// Handle identifies a versioned custody object. It is safe to persist and
// emit in evidence; it contains neither material nor a provider locator.
type Handle struct {
	ID      string `json:"id"`
	Kind    Kind   `json:"kind"`
	Version string `json:"version"`
	Tenant  string `json:"tenant"`
	Region  string `json:"region"`
}

func (h Handle) Validate() error {
	if strings.TrimSpace(h.ID) == "" || strings.TrimSpace(h.Version) == "" || strings.TrimSpace(h.Tenant) == "" || strings.TrimSpace(h.Region) == "" {
		return fmt.Errorf("%w: id, version, tenant and region are required", ErrInvalidHandle)
	}
	switch h.Kind {
	case Secret, Key, Certificate:
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidHandle, h.Kind)
	}
	return nil
}

// RequestContext binds every operation to the authenticated workload and its
// exact semantic purpose and destination.
type RequestContext struct {
	Workload    string
	Tenant      string
	Region      string
	Purpose     string
	Destination string
}

func (c RequestContext) Validate() error {
	if strings.TrimSpace(c.Workload) == "" || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.Region) == "" || strings.TrimSpace(c.Purpose) == "" || strings.TrimSpace(c.Destination) == "" {
		return fmt.Errorf("%w: workload, tenant, region, purpose and destination are required", ErrInvalidContext)
	}
	return nil
}

// Ciphertext is provider-neutral sealed application data. Data is ciphertext,
// never a key or secret value.
type Ciphertext struct {
	Handle    Handle `json:"handle"`
	Algorithm string `json:"algorithm"`
	Data      []byte `json:"data"`
}

type Signature struct {
	Handle    Handle `json:"handle"`
	Algorithm string `json:"algorithm"`
	Data      []byte `json:"data"`
}

// Lease is a short-lived authorization and contains no credential bytes.
type Lease struct {
	ID            string    `json:"id"`
	Handle        Handle    `json:"handle"`
	Operation     Operation `json:"operation"`
	ExpiresAt     time.Time `json:"expires_at"`
	ContextDigest [32]byte  `json:"context_digest"`
}

func (l Lease) Validate(now time.Time) error {
	if strings.TrimSpace(l.ID) == "" || l.Handle.Validate() != nil || !validOperation(l.Operation) || l.ExpiresAt.IsZero() {
		return ErrInvalidLease
	}
	if l.ExpiresAt.Before(now) {
		return ErrExpired
	}
	return nil
}

func validOperation(op Operation) bool {
	switch op {
	case Encrypt, Decrypt, Sign, Verify, LeaseOperation, Rotate, Revoke:
		return true
	default:
		return false
	}
}

type Receipt struct {
	ID            string    `json:"id"`
	Handle        Handle    `json:"handle"`
	Operation     Operation `json:"operation"`
	ContextDigest [32]byte  `json:"context_digest"`
	At            time.Time `json:"at"`
}

// ContextDigest provides a stable, non-sensitive binding for evidence.
func ContextDigest(c RequestContext) [32]byte {
	return sha256.Sum256([]byte(c.Workload + "\x00" + c.Tenant + "\x00" + c.Region + "\x00" + c.Purpose + "\x00" + c.Destination))
}

// Provider is the complete custody port. Implementations must enforce tenant,
// purpose, destination, version, expiry, and revocation before provider use.
type Provider interface {
	Encrypt(ctx Context, object Handle, plaintext []byte) (Ciphertext, Receipt, error)
	Decrypt(ctx Context, object Handle, ciphertext Ciphertext) ([]byte, Receipt, error)
	Sign(ctx Context, object Handle, message []byte) (Signature, Receipt, error)
	Verify(ctx Context, object Handle, message []byte, signature Signature) (bool, Receipt, error)
	IssueLease(ctx Context, object Handle, operation Operation, ttl time.Duration) (Lease, error)
	RenewLease(ctx Context, lease Lease, ttl time.Duration) (Lease, error)
	Rotate(ctx Context, object Handle) (Handle, Receipt, error)
	Revoke(ctx Context, object Handle, reason string) (Receipt, error)
}

// Context is intentionally an alias-like wrapper so implementations cannot
// accidentally accept an unbound string or provider-specific request.
type Context struct{ RequestContext }

func (c Context) Validate() error { return c.RequestContext.Validate() }
