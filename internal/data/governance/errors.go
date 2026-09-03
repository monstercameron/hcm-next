package governance

import (
	"errors"
	"fmt"
)

var (
	ErrCrossTenantDelegation = errors.New("governance: cross-tenant delegation rejected")
	ErrUnversionedAuthority  = errors.New("governance: unversioned authority rejected")
	ErrExpiredEvidence       = errors.New("governance: evidence expired at authorization time")
	ErrInvalidInterval       = errors.New("governance: validity interval invalid")
	ErrMissingDigest         = errors.New("governance: digest missing")
	ErrEmptyPolicySnapshot   = errors.New("governance: policy_snapshot_ids empty")
	ErrValidityExpired       = errors.New("governance: validity interval expired")
	ErrSameTenantViolation   = errors.New("governance: same-tenant check violated")
	ErrNilTenant             = errors.New("governance: tenant id is nil")
	ErrDigestMismatch        = errors.New("governance: digest mismatch")
)

type ErrCrossTenant struct {
	TenantID        string
	DelegatorTenant string
	DelegateTenant  string
}

func (e ErrCrossTenant) Error() string {
	return fmt.Sprintf("governance: cross-tenant delegation tenant=%s delegator=%s delegate=%s: %v", e.TenantID, e.DelegatorTenant, e.DelegateTenant, ErrCrossTenantDelegation)
}

func (e ErrCrossTenant) Is(target error) bool { return errors.Is(target, ErrCrossTenantDelegation) }

type ErrInvalidIntervalDetail struct {
	Reason string
}

func (e ErrInvalidIntervalDetail) Error() string {
	return fmt.Sprintf("%v: %s", ErrInvalidInterval, e.Reason)
}

func (e ErrInvalidIntervalDetail) Is(target error) bool { return errors.Is(target, ErrInvalidInterval) }

type ErrUnversionedDetail struct {
	Field string
}

func (e ErrUnversionedDetail) Error() string {
	return fmt.Sprintf("%v: %s", ErrUnversionedAuthority, e.Field)
}

func (e ErrUnversionedDetail) Is(target error) bool {
	return errors.Is(target, ErrUnversionedAuthority)
}
