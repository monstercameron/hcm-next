package commercial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ContractStatus string

const (
	StatusActive    ContractStatus = "ACTIVE"
	StatusSuspended ContractStatus = "SUSPENDED"
	StatusRevoked   ContractStatus = "REVOKED"
)

// Contract is a tenant-scoped, versioned commercial entitlement snapshot.
// It only grants named capabilities; it never grants execution authority.
type Contract struct {
	TenantID      string         `json:"tenant_id"`
	ContractID    string         `json:"contract_id"`
	Version       uint64         `json:"version"`
	EffectiveFrom time.Time      `json:"effective_from"`
	EffectiveTo   time.Time      `json:"effective_to"`
	Status        ContractStatus `json:"status"`
	Capabilities  []string       `json:"capabilities"`
	PriceCents    int64          `json:"price_cents"`
	Currency      string         `json:"currency"`
}

var ErrInvalidContract = fmt.Errorf("commercial: invalid contract")

func (c Contract) Validate() error {
	if c.TenantID == "" || c.ContractID == "" || c.Version == 0 || c.EffectiveFrom.IsZero() || c.Status == "" || c.PriceCents < 0 || strings.TrimSpace(c.Currency) == "" {
		return fmt.Errorf("%w: required fields", ErrInvalidContract)
	}
	if c.EffectiveTo.IsZero() || !c.EffectiveTo.After(c.EffectiveFrom) {
		return fmt.Errorf("%w: effective interval", ErrInvalidContract)
	}
	if c.Status != StatusActive && c.Status != StatusSuspended && c.Status != StatusRevoked {
		return fmt.Errorf("%w: status", ErrInvalidContract)
	}
	if len(c.Capabilities) == 0 {
		return fmt.Errorf("%w: capabilities", ErrInvalidContract)
	}
	seen := map[string]bool{}
	for _, cap := range c.Capabilities {
		if strings.TrimSpace(cap) == "" || seen[cap] {
			return fmt.Errorf("%w: capability %q", ErrInvalidContract, cap)
		}
		seen[cap] = true
	}
	return nil
}

func (c Contract) clone() Contract {
	c.Capabilities = append([]string(nil), c.Capabilities...)
	return c
}

func (c Contract) Fingerprint() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidContract, err)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
