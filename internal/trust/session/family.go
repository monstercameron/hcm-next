package session

import (
	"context"
	"time"
)

// Family is the AUTHN-004 semantic facade over the TRUST-003 session
// manager. A session record is itself a refresh-token family: every rotation
// advances that family, and replay revokes the family rather than one token.
// Keeping this facade thin makes the authentication owner depend on the
// already-proven trust/session state machine instead of duplicating it.
type Family struct {
	manager *Manager
}

// NewFamily constructs a rotating session-family authority.
func NewFamily(cfg ManagerConfig) (*Family, error) {
	m, err := NewManager(cfg)
	if err != nil {
		return nil, err
	}
	return &Family{manager: m}, nil
}

// Manager exposes the underlying trust manager for adapters that already use
// the TRUST-003 API. It returns the owned manager, whose methods are safe for
// concurrent use.
func (f *Family) Manager() *Manager {
	if f == nil {
		return nil
	}
	return f.manager
}

// Create opens one active family and returns its current refresh credential.
func (f *Family) Create(ctx context.Context, spec CreateSpec) (Record, RefreshToken, error) {
	if f == nil || f.manager == nil {
		return Record{}, "", ErrInvalidCreateSpec
	}
	return f.manager.Create(ctx, spec)
}

// Rotate retires presented and returns a successor. A retired credential is
// a replay and revokes the complete family.
func (f *Family) Rotate(ctx context.Context, presented RefreshToken) (Record, RefreshToken, error) {
	if f == nil || f.manager == nil {
		return Record{}, "", ErrInvalidCreateSpec
	}
	return f.manager.Refresh(ctx, presented)
}

// Validate checks the current family state and the asserted tenant and
// assurance. It never trusts a previously returned Record as current state.
func (f *Family) Validate(ctx context.Context, id ID, claim Claim) (Record, error) {
	if f == nil || f.manager == nil {
		return Record{}, ErrInvalidCreateSpec
	}
	return f.manager.Validate(ctx, id, claim)
}

// CheckRevocation checks the family at the supplied boundary instant.
func (f *Family) CheckRevocation(ctx context.Context, id ID, at time.Time) error {
	if f == nil || f.manager == nil {
		return ErrInvalidCreateSpec
	}
	return f.manager.CheckRevocation(ctx, string(id), at)
}

// Revoke immediately fences the family and its current refresh credential.
func (f *Family) Revoke(ctx context.Context, id ID, reason string) (Record, error) {
	if f == nil || f.manager == nil {
		return Record{}, ErrInvalidCreateSpec
	}
	return f.manager.Revoke(ctx, id, reason)
}

// Version is the contract version for the AUTHN-004 facade.
func Version() int { return 1 }

// Explain returns a redaction-safe summary of the family contract.
func Explain() string {
	return "session families rotate single-use refresh credentials, enforce idle and absolute expiry, and revoke on replay or explicit lifecycle fencing."
}

// Explain returns the package contract without exposing family or token data.
func (f *Family) Explain() string { return Explain() }
