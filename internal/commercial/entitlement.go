package commercial

import (
	"fmt"
	"time"
)

type DecisionCode string

const (
	CodeAllow                   DecisionCode = "ALLOW"
	CodeContractNotYetEffective DecisionCode = "CONTRACT_NOT_YET_EFFECTIVE"
	CodeContractExpired         DecisionCode = "CONTRACT_EXPIRED"
	CodeContractSuspended       DecisionCode = "CONTRACT_SUSPENDED"
	CodeContractRevoked         DecisionCode = "CONTRACT_REVOKED"
	CodeCapabilityOutOfScope    DecisionCode = "CAPABILITY_OUT_OF_SCOPE"
	CodeTenantMismatch          DecisionCode = "TENANT_MISMATCH"
)

// Channel is informational only. Every channel uses the same resolver and
// therefore receives the same code and fingerprint.
type Channel string

const (
	ChannelUI        Channel = "UI"
	ChannelHTTP      Channel = "HTTP"
	ChannelGRPC      Channel = "GRPC"
	ChannelWorkflow  Channel = "WORKFLOW"
	ChannelConnector Channel = "CONNECTOR"
)

type Request struct {
	TenantID   string
	Capability string
	At         time.Time
	Channel    Channel
}

type Decision struct {
	Code            DecisionCode
	TenantID        string
	Capability      string
	ContractID      string
	ContractVersion uint64
	Fingerprint     string
}

func (d Decision) Allowed() bool { return d.Code == CodeAllow }

// Snapshot is a defensive, immutable copy of the contract selected at
// resolution time. Later contract amendments cannot alter it.
type Snapshot struct {
	Contract    Contract
	Fingerprint string
}

func NewSnapshot(c Contract) (Snapshot, error) {
	fp, err := c.Fingerprint()
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Contract: c.clone(), Fingerprint: fp}, nil
}

func (s Snapshot) Resolve(r Request) Decision {
	c := s.Contract
	d := Decision{TenantID: r.TenantID, Capability: r.Capability, ContractID: c.ContractID, ContractVersion: c.Version, Fingerprint: s.Fingerprint}
	if r.TenantID != c.TenantID {
		d.Code = CodeTenantMismatch
		return d
	}
	if r.At.Before(c.EffectiveFrom) {
		d.Code = CodeContractNotYetEffective
		return d
	}
	if !c.EffectiveTo.IsZero() && !r.At.Before(c.EffectiveTo) {
		d.Code = CodeContractExpired
		return d
	}
	if c.Status == StatusSuspended {
		d.Code = CodeContractSuspended
		return d
	}
	if c.Status == StatusRevoked {
		d.Code = CodeContractRevoked
		return d
	}
	for _, capability := range c.Capabilities {
		if capability == r.Capability {
			d.Code = CodeAllow
			return d
		}
	}
	d.Code = CodeCapabilityOutOfScope
	return d
}

func (s Snapshot) Validate() error {
	fp, err := s.Contract.Fingerprint()
	if err != nil {
		return err
	}
	if s.Fingerprint == "" || s.Fingerprint != fp {
		return fmt.Errorf("%w: fingerprint mismatch", ErrInvalidContract)
	}
	return nil
}
