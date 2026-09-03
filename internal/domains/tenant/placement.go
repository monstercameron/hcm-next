// Package tenant owns the logical placement and residency contract.
package tenant

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrInvalidPlacement  = errors.New("tenant: invalid placement")
	ErrInvalidSignature  = errors.New("tenant: invalid placement signature")
	ErrPlacementMismatch = errors.New("PLACEMENT_MISMATCH")
)

// Placement is the signed logical location of a tenant. Epoch is a fencing
// token: a writer may use a placement only when its complete value matches
// the currently resolved value.
type Placement struct {
	Tenant           string `json:"tenant"`
	Cell             string `json:"cell"`
	Region           string `json:"region"`
	ResidencyProfile string `json:"residency_profile"`
	IsolationTier    string `json:"isolation_tier"`
	Epoch            uint64 `json:"epoch"`
	Signature        string `json:"signature,omitempty"`
}

// Validate checks the semantic shape independently of signature validity.
func (p Placement) Validate() error {
	for name, value := range map[string]string{
		"tenant": p.Tenant, "cell": p.Cell, "region": p.Region,
		"residency_profile": p.ResidencyProfile, "isolation_tier": p.IsolationTier,
	} {
		if value == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidPlacement, name)
		}
	}
	if p.Epoch == 0 {
		return fmt.Errorf("%w: epoch must be positive", ErrInvalidPlacement)
	}
	return nil
}

// canonical returns the exact bytes covered by the signature. Signature is
// intentionally excluded so signing and verification cannot be circular.
func (p Placement) canonical() ([]byte, error) {
	type unsigned Placement
	u := unsigned(p)
	u.Signature = ""
	return json.Marshal(u)
}

// Digest identifies the logical placement independent of its signature.
func (p Placement) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := p.canonical()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Sign returns a copy of p signed with the tenant placement authority key.
func Sign(p Placement, key [32]byte) (Placement, error) {
	if err := p.Validate(); err != nil {
		return Placement{}, err
	}
	b, err := p.canonical()
	if err != nil {
		return Placement{}, fmt.Errorf("%w: canonical encoding: %v", ErrInvalidPlacement, err)
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(b)
	p.Signature = hex.EncodeToString(mac.Sum(nil))
	return p, nil
}

// Verify checks both shape and authority signature. Missing or malformed
// signatures fail closed as ErrInvalidSignature.
func Verify(p Placement, key [32]byte) error {
	if err := p.Validate(); err != nil {
		return err
	}
	provided, err := hex.DecodeString(p.Signature)
	if err != nil || len(provided) != sha256.Size {
		return ErrInvalidSignature
	}
	b, err := p.canonical()
	if err != nil {
		return fmt.Errorf("%w: canonical encoding: %v", ErrInvalidSignature, err)
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(b)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return ErrInvalidSignature
	}
	return nil
}

// Matches reports whether two placements refer to the same fenced logical
// location. Signature is deliberately not compared here; callers should
// verify the candidate and authoritative placement separately.
func (p Placement) Matches(other Placement) bool {
	return p.Tenant == other.Tenant && p.Cell == other.Cell && p.Region == other.Region &&
		p.ResidencyProfile == other.ResidencyProfile && p.IsolationTier == other.IsolationTier &&
		p.Epoch == other.Epoch
}

// CheckContext verifies an incoming signed placement and rejects any context
// that differs in tenant, residency, isolation, cell, region, or epoch.
func CheckContext(authoritative, incoming Placement, key [32]byte) error {
	if err := Verify(authoritative, key); err != nil {
		return err
	}
	if err := Verify(incoming, key); err != nil {
		return err
	}
	if !authoritative.Matches(incoming) {
		return ErrPlacementMismatch
	}
	return nil
}
