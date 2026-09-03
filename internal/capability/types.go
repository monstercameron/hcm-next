package capability

import (
	"context"
	"fmt"
)

// EffectClass mirrors the kernel side-effect classification at capability
// granularity (capability-registry-and-lifecycle.md manifest core:
// "side-effect profile").
type EffectClass string

// The five declared effect classes. There is no sixth.
const (
	EffectPure                         EffectClass = "PURE"
	EffectReadOnly                     EffectClass = "READ_ONLY"
	EffectInternalMutation             EffectClass = "INTERNAL_MUTATION"
	EffectExternalMutation             EffectClass = "EXTERNAL_MUTATION"
	EffectIrreversibleExternalMutation EffectClass = "IRREVERSIBLE_EXTERNAL_MUTATION"
)

// Valid reports whether e is one of the five declared classes.
func (e EffectClass) Valid() bool {
	switch e {
	case EffectPure, EffectReadOnly, EffectInternalMutation, EffectExternalMutation, EffectIrreversibleExternalMutation:
		return true
	default:
		return false
	}
}

// IsWrite reports whether invoking a capability declaring this effect class
// can mutate state or produce an external effect. P1A's governed gateway
// (CAP-002) refuses every write-effect capability; only PURE and READ_ONLY
// capabilities may execute.
func (e EffectClass) IsWrite() bool {
	switch e {
	case EffectInternalMutation, EffectExternalMutation, EffectIrreversibleExternalMutation:
		return true
	default:
		return false
	}
}

// Status is a capability version's lifecycle status. Under BOOTSTRAP there is
// no separate publish/activate flow, but a version can still be marked
// Deprecated (still resolvable; no longer the latest recommendation) or
// Retired (exists for history; the gateway refuses to invoke it).
type Status string

const (
	StatusActive     Status = "ACTIVE"
	StatusDeprecated Status = "DEPRECATED"
	StatusRetired    Status = "RETIRED"
)

// SchemaRef names one versioned Protobuf schema by descriptor identity. It
// mirrors hcmnext.capabilities.v1.SchemaRef.
type SchemaRef struct {
	SchemaID         string
	Version          uint32
	ProtobufFullName string
}

// Valid reports whether every component of the schema reference is present.
func (s SchemaRef) Valid() bool {
	return s.SchemaID != "" && s.Version >= 1 && s.ProtobufFullName != ""
}

// DataDomainFieldSet names the data domains, and within them the field paths,
// a capability reads or writes. It mirrors
// hcmnext.capabilities.v1.DataDomainFieldSet. An empty FieldPaths list means
// the entire named domains.
type DataDomainFieldSet struct {
	DataDomains []string
	FieldPaths  []string
}

func (d DataDomainFieldSet) clone() DataDomainFieldSet {
	return DataDomainFieldSet{
		DataDomains: append([]string(nil), d.DataDomains...),
		FieldPaths:  append([]string(nil), d.FieldPaths...),
	}
}

// Key identifies one exact capability version. Capability identity is
// (ID, Version); there is no "latest" identity.
type Key struct {
	ID      string
	Version uint32
}

func (k Key) String() string { return fmt.Sprintf("%s/v%d", k.ID, k.Version) }

// Handler executes one capability invocation. The payload and result are
// opaque to this package: the concrete request/response types belong to the
// intent and transport layers, which this package must not import.
type Handler func(ctx context.Context, payload any) (any, error)

// Definition is one immutable, versioned CapabilityDefinition
// (capability-registry-and-lifecycle.md "core", plus the governance
// references CAP-001 requires before publication: authorization scope,
// legal basis, entitlement, SLO class and a resolvable test reference).
//
// A Definition is a plain value: comparing two definitions with
// reflect.DeepEqual is meaningful, and a Registry never hands out a reference
// into its own storage.
type Definition struct {
	ID             string
	Version        uint32
	OwnerDomain    string
	RequestSchema  SchemaRef
	ResponseSchema SchemaRef
	ErrorSchema    SchemaRef
	EffectClass    EffectClass
	ReadData       DataDomainFieldSet
	WriteData      DataDomainFieldSet
	RiskClass      string
	// IdempotencyPolicyRef names the idempotency policy this capability obeys.
	IdempotencyPolicyRef string
	// AgentEligible defaults to false: an agent initiator is not eligible to
	// invoke this capability unless explicitly declared eligible.
	AgentEligible bool

	// AuthZScopeRef names the authorization scope a caller must already have
	// been granted for the gateway to invoke this capability. This package
	// never grants or checks scopes against an identity provider; it only
	// requires that the caller's already-made Authorization decision names
	// this scope (CAP-002).
	AuthZScopeRef string
	// LegalBasisRef names the legal/regulatory basis reviewed for this
	// capability's data use.
	LegalBasisRef string
	// EntitlementRef names the commercial entitlement gating this capability.
	EntitlementRef string
	// SLOClassRef names the published service-level objective class.
	SLOClassRef string
	// TestRef names the conformance test proving this capability's contract.
	TestRef string
}

// Key returns the definition's identity.
func (d Definition) Key() Key { return Key{ID: d.ID, Version: d.Version} }

// clone returns a deep copy so returned values never alias registry storage.
func (d Definition) clone() Definition {
	c := d
	c.ReadData = d.ReadData.clone()
	c.WriteData = d.WriteData.clone()
	return c
}

// Record is a definition plus the registry-owned facts about it: its
// lifecycle status and its content digest.
type Record struct {
	Definition Definition
	Status     Status
	// Digest is the canonical content digest of the definition's core
	// manifest fields (Digest computes it via internal/kernel/digest, whose
	// hcmnext.capabilities.v1.CapabilityDefinition profile is exactly that
	// core - see digest.go).
	Digest string
}
