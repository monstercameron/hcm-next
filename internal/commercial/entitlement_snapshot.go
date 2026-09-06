package commercial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// EntitlementBound is the fixed population limit carried by a pilot
// entitlement. Exactly one of Seats or Population must be set. Population is
// an opaque, tenant-owned reference; this package never expands it into
// workforce data.
type EntitlementBound struct {
	Seats      uint64 `json:"seats,omitempty"`
	Population string `json:"population,omitempty"`
}

// Valid reports whether the bound selects exactly one fixed population shape.
func (b EntitlementBound) Valid() bool {
	hasSeats := b.Seats > 0
	hasPopulation := strings.TrimSpace(b.Population) != ""
	return hasSeats != hasPopulation
}

// BoundKind identifies which fixed population shape applies.
type BoundKind string

const (
	BoundBySeats      BoundKind = "SEATS"
	BoundByPopulation BoundKind = "POPULATION"
)

func (b EntitlementBound) kind() BoundKind {
	if b.Seats > 0 {
		return BoundBySeats
	}
	return BoundByPopulation
}

var (
	// ErrInvalidEntitlementSnapshot means the revision cannot be made into a
	// reproducible fixed-price entitlement.
	ErrInvalidEntitlementSnapshot = errors.New("commercial: invalid entitlement snapshot")
	// ErrInvalidEntitlementAmendment means an amendment would rewrite the
	// identity or history of an existing contract instead of creating a new
	// revision.
	ErrInvalidEntitlementAmendment = errors.New("commercial: invalid entitlement amendment")
)

// FixedPricePilotContract is one customer-facing contract revision. It has no
// usage meter, rating rule, invoice calculation, or mutable status: the
// revision is the complete fixed-price entitlement input for P1A.
type FixedPricePilotContract struct {
	TenantID      string    `json:"tenant_id"`
	ContractID    string    `json:"contract_id"`
	Revision      uint64    `json:"revision"`
	EffectiveFrom time.Time `json:"effective_from"`
	EffectiveTo   time.Time `json:"effective_to"`
	// Status is part of the revision identity. An omitted status is treated as
	// ACTIVE for compatibility with the first P1A contract fixtures; a frozen
	// snapshot always stores the explicit value.
	Status       ContractStatus   `json:"status"`
	Capabilities []string         `json:"capabilities"`
	Bound        EntitlementBound `json:"bound"`
	PriceCents   int64            `json:"price_cents"`
	Currency     string           `json:"currency"`
}

// ContractRevision is a descriptive compatibility name for a fixed-price
// pilot contract revision.
type ContractRevision = FixedPricePilotContract

func (c FixedPricePilotContract) Validate() error {
	for name, value := range map[string]string{
		"tenant_id":   c.TenantID,
		"contract_id": c.ContractID,
		"currency":    c.Currency,
	} {
		if !validReference(value) {
			return fmt.Errorf("%w: %s is required and must be a safe reference", ErrInvalidEntitlementSnapshot, name)
		}
	}
	if c.Revision == 0 {
		return fmt.Errorf("%w: revision must be positive", ErrInvalidEntitlementSnapshot)
	}
	if c.EffectiveFrom.IsZero() || c.EffectiveTo.IsZero() || !c.EffectiveTo.After(c.EffectiveFrom) {
		return fmt.Errorf("%w: effective window must be non-empty", ErrInvalidEntitlementSnapshot)
	}
	if len(c.Capabilities) == 0 {
		return fmt.Errorf("%w: capabilities are required", ErrInvalidEntitlementSnapshot)
	}
	seen := make(map[string]struct{}, len(c.Capabilities))
	for _, capability := range c.Capabilities {
		if !validReference(capability) {
			return fmt.Errorf("%w: capability is required and must be a safe reference", ErrInvalidEntitlementSnapshot)
		}
		if _, exists := seen[capability]; exists {
			return fmt.Errorf("%w: duplicate capability %q", ErrInvalidEntitlementSnapshot, capability)
		}
		seen[capability] = struct{}{}
	}
	if !c.Bound.Valid() {
		return fmt.Errorf("%w: exactly one positive seat or population bound is required", ErrInvalidEntitlementSnapshot)
	}
	if c.PriceCents <= 0 {
		return fmt.Errorf("%w: fixed price must be positive", ErrInvalidEntitlementSnapshot)
	}
	if c.Status != "" && c.Status != StatusActive && c.Status != StatusSuspended && c.Status != StatusRevoked {
		return fmt.Errorf("%w: invalid status", ErrInvalidEntitlementSnapshot)
	}
	return nil
}

func (c FixedPricePilotContract) clone() FixedPricePilotContract {
	c.Capabilities = append([]string(nil), c.Capabilities...)
	return c
}

type canonicalEntitlementRevision struct {
	Schema        string           `json:"schema"`
	TenantID      string           `json:"tenant_id"`
	ContractID    string           `json:"contract_id"`
	Revision      uint64           `json:"revision"`
	EffectiveFrom string           `json:"effective_from"`
	EffectiveTo   string           `json:"effective_to"`
	Status        ContractStatus   `json:"status"`
	Capabilities  []string         `json:"capabilities"`
	Bound         EntitlementBound `json:"bound"`
	PriceCents    int64            `json:"price_cents"`
	Currency      string           `json:"currency"`
}

func (c FixedPricePilotContract) canonical() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	capabilities := append([]string(nil), c.Capabilities...)
	sort.Strings(capabilities)
	return json.Marshal(canonicalEntitlementRevision{
		Schema:        "hcmnext.commercial.entitlement_snapshot/v1",
		TenantID:      c.TenantID,
		ContractID:    c.ContractID,
		Revision:      c.Revision,
		EffectiveFrom: c.EffectiveFrom.UTC().Format(time.RFC3339Nano),
		EffectiveTo:   c.EffectiveTo.UTC().Format(time.RFC3339Nano),
		Status:        normalizedStatus(c.Status),
		Capabilities:  capabilities,
		Bound:         c.Bound,
		PriceCents:    c.PriceCents,
		Currency:      c.Currency,
	})
}

// EntitlementSnapshot is an immutable, tenant-scoped entitlement revision.
// Contract data is private and all accessors return values or defensive
// copies, so creating an amendment cannot change an execution already pinned
// to this snapshot.
type EntitlementSnapshot struct {
	contract    FixedPricePilotContract
	fingerprint string
}

// NewEntitlementSnapshot validates and freezes one fixed-price pilot contract
// revision. The fingerprint is the SHA-256 digest of its canonical bytes.
func NewEntitlementSnapshot(contract FixedPricePilotContract) (EntitlementSnapshot, error) {
	canonical, err := contract.canonical()
	if err != nil {
		return EntitlementSnapshot{}, err
	}
	digest := sha256.Sum256(canonical)
	contract.Status = normalizedStatus(contract.Status)
	return EntitlementSnapshot{
		contract:    contract.clone(),
		fingerprint: hex.EncodeToString(digest[:]),
	}, nil
}

// Contract returns a defensive copy of the frozen contract revision.
func (s EntitlementSnapshot) Contract() FixedPricePilotContract { return s.contract.clone() }

// TenantID returns the tenant to which the snapshot is bound.
func (s EntitlementSnapshot) TenantID() string { return s.contract.TenantID }

// ContractID returns the stable contract identity.
func (s EntitlementSnapshot) ContractID() string { return s.contract.ContractID }

// Revision returns the immutable contract revision number.
func (s EntitlementSnapshot) Revision() uint64 { return s.contract.Revision }

// Status returns the frozen lifecycle status. A suspended or revoked status
// is a typed refusal, never a capability removal from the historical record.
func (s EntitlementSnapshot) Status() ContractStatus { return s.contract.Status }

// EffectiveWindow returns the half-open [from, to) window of the snapshot.
func (s EntitlementSnapshot) EffectiveWindow() (time.Time, time.Time) {
	return s.contract.EffectiveFrom, s.contract.EffectiveTo
}

// Capabilities returns a defensive copy of the entitled capability set.
func (s EntitlementSnapshot) Capabilities() []string {
	return append([]string(nil), s.contract.Capabilities...)
}

// Bound returns the fixed seat or population bound.
func (s EntitlementSnapshot) Bound() EntitlementBound { return s.contract.Bound }

// Fingerprint returns the canonical identity of this snapshot.
func (s EntitlementSnapshot) Fingerprint() string { return s.fingerprint }

// Validate re-computes the frozen snapshot identity and checks its internal
// contract. It is useful at boundaries that receive a snapshot by value.
func (s EntitlementSnapshot) Validate() error {
	canonical, err := s.contract.canonical()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	if s.fingerprint != hex.EncodeToString(digest[:]) {
		return fmt.Errorf("%w: fingerprint mismatch", ErrInvalidEntitlementSnapshot)
	}
	return nil
}

// Amend creates a new snapshot revision. The receiver remains unchanged, so
// executions holding its fingerprint continue to resolve against its old
// effective window and capability set.
func (s EntitlementSnapshot) Amend(next FixedPricePilotContract) (EntitlementSnapshot, error) {
	if err := s.Validate(); err != nil {
		return EntitlementSnapshot{}, err
	}
	if next.TenantID != s.contract.TenantID || next.ContractID != s.contract.ContractID {
		return EntitlementSnapshot{}, fmt.Errorf("%w: tenant and contract identity cannot change", ErrInvalidEntitlementAmendment)
	}
	if next.Revision <= s.contract.Revision {
		return EntitlementSnapshot{}, fmt.Errorf("%w: revision must increase", ErrInvalidEntitlementAmendment)
	}
	return NewEntitlementSnapshot(next)
}

// EntitlementRequest is the channel-neutral input to the single entitlement
// resolver. Channel is metadata only and cannot alter the answer.
type EntitlementRequest struct {
	TenantID   string
	Capability string
	At         time.Time
	Channel    Channel
}

// EntitlementCode is the closed, non-disclosing result vocabulary for pilot
// entitlement resolution. It aliases the commercial package's original
// DecisionCode so both the stopped lane's Contract API and this stricter
// snapshot API can be composed without converting codes.
type EntitlementCode = DecisionCode

const CodeAllowed DecisionCode = "ALLOWED"

// EntitlementDecision is the channel-neutral decision and pinned snapshot
// identity. A channel may project it, but may not replace its code.
type EntitlementDecision struct {
	Code             EntitlementCode
	Fingerprint      string
	TenantID         string
	Capability       string
	ContractID       string
	ContractRevision uint64
}

// Allowed reports whether the decision permits the requested capability.
func (d EntitlementDecision) Allowed() bool { return d.Code == CodeAllowed }

// Resolve answers one tenant/capability/instant question against this exact
// snapshot. The interval is half-open: effective at EffectiveFrom and
// expired at EffectiveTo. A tenant mismatch fails closed as
// CAPABILITY_OUT_OF_SCOPE so the resolver does not disclose another tenant's
// contract existence.
func (s EntitlementSnapshot) Resolve(request EntitlementRequest) EntitlementDecision {
	d := EntitlementDecision{
		Fingerprint:      s.fingerprint,
		TenantID:         request.TenantID,
		Capability:       request.Capability,
		ContractID:       s.contract.ContractID,
		ContractRevision: s.contract.Revision,
	}
	switch {
	case request.TenantID != s.contract.TenantID:
		d.Code = CodeCapabilityOutOfScope
	case request.At.Before(s.contract.EffectiveFrom):
		d.Code = CodeContractNotYetEffective
	case !request.At.Before(s.contract.EffectiveTo):
		d.Code = CodeContractExpired
	case s.contract.Status == StatusSuspended:
		d.Code = CodeContractSuspended
	case s.contract.Status == StatusRevoked:
		d.Code = CodeContractRevoked
	case !contains(s.contract.Capabilities, request.Capability):
		d.Code = CodeCapabilityOutOfScope
	default:
		d.Code = CodeAllowed
	}
	return d
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Explain returns an audit-safe summary. It includes stable references and
// counts, never the opaque population reference or a caller-supplied value.
func (s EntitlementSnapshot) Explain() string {
	from, to := s.EffectiveWindow()
	return fmt.Sprintf("entitlement snapshot tenant=%s contract=%s revision=%d fingerprint=%s effective=[%s,%s) capabilities=%d bound=%s fixed_price=true",
		s.contract.TenantID, s.contract.ContractID, s.contract.Revision, s.fingerprint,
		from.UTC().Format(time.RFC3339Nano), to.UTC().Format(time.RFC3339Nano), len(s.contract.Capabilities), s.contract.Bound.kind())
}

// Explain renders an entitlement snapshot through the package-level symbol
// used by audit and conformance tooling.
func Explain(s EntitlementSnapshot) string { return s.Explain() }

// EntitlementResolver is the one channel-neutral resolver contract. All
// channel adapters below accept only this interface and delegate to it.
type EntitlementResolver interface {
	Resolve(EntitlementRequest) EntitlementDecision
}

type entitlementChannelFake struct{ resolver EntitlementResolver }

func (f entitlementChannelFake) resolve(request EntitlementRequest) EntitlementDecision {
	if f.resolver == nil {
		return EntitlementDecision{Code: CodeCapabilityOutOfScope}
	}
	return f.resolver.Resolve(request)
}

// UIFake is the UI channel adapter used to prove channel parity.
type UIFake struct{ entitlementChannelFake }

func NewUIFake(resolver EntitlementResolver) UIFake {
	return UIFake{entitlementChannelFake{resolver: resolver}}
}
func (f UIFake) Resolve(request EntitlementRequest) EntitlementDecision { return f.resolve(request) }

// HTTPFake is the HTTP channel adapter used to prove channel parity.
type HTTPFake struct{ entitlementChannelFake }

func NewHTTPFake(resolver EntitlementResolver) HTTPFake {
	return HTTPFake{entitlementChannelFake{resolver: resolver}}
}
func (f HTTPFake) Resolve(request EntitlementRequest) EntitlementDecision { return f.resolve(request) }

// GRPCFake is the gRPC channel adapter used to prove channel parity.
type GRPCFake struct{ entitlementChannelFake }

func NewGRPCFake(resolver EntitlementResolver) GRPCFake {
	return GRPCFake{entitlementChannelFake{resolver: resolver}}
}
func (f GRPCFake) Resolve(request EntitlementRequest) EntitlementDecision { return f.resolve(request) }

// WorkflowFake is the workflow channel adapter used to prove channel parity.
type WorkflowFake struct{ entitlementChannelFake }

func NewWorkflowFake(resolver EntitlementResolver) WorkflowFake {
	return WorkflowFake{entitlementChannelFake{resolver: resolver}}
}
func (f WorkflowFake) Resolve(request EntitlementRequest) EntitlementDecision {
	return f.resolve(request)
}

// ConnectorFake is the connector channel adapter used to prove channel
// parity.
type ConnectorFake struct{ entitlementChannelFake }

func NewConnectorFake(resolver EntitlementResolver) ConnectorFake {
	return ConnectorFake{entitlementChannelFake{resolver: resolver}}
}
func (f ConnectorFake) Resolve(request EntitlementRequest) EntitlementDecision {
	return f.resolve(request)
}

func validReference(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func normalizedStatus(status ContractStatus) ContractStatus {
	if status == "" {
		return StatusActive
	}
	return status
}
