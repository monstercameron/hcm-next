package capability

import "fmt"

// Stable codes. A transport layer maps these without reading message text
// (mirrors the pattern in internal/data/ledger/errors.go).
const (
	CodeFieldRequired         = "CAPABILITY_FIELD_REQUIRED"
	CodeInvalidEffectClass    = "CAPABILITY_INVALID_EFFECT_CLASS"
	CodeImplementationUnbound = "CAPABILITY_IMPLEMENTATION_UNBOUND"
	CodeAlreadyRegistered     = "CAPABILITY_VERSION_ALREADY_REGISTERED"
	CodeUnknownCapability     = "CAPABILITY_UNKNOWN"
	CodeCapabilityDisabled    = "CAPABILITY_DISABLED"
	CodeUnauthorized          = "CAPABILITY_UNAUTHORIZED"
	CodeWriteEffectRefusedP1A = "CAPABILITY_WRITE_EFFECT_REFUSED_P1A"
	CodeHandlerFailed         = "CAPABILITY_HANDLER_FAILED"
)

// ErrDefinitionInvalid reports a definition publication rejected for a
// missing or malformed required field (CAP-001 RED clause).
type ErrDefinitionInvalid struct {
	ID      string
	Version uint32
	Field   string
	Reason  string
}

func (ErrDefinitionInvalid) Code() string { return CodeFieldRequired }

func (e ErrDefinitionInvalid) Error() string {
	return fmt.Sprintf("%s: capability %s/v%d field %s %s", CodeFieldRequired, e.ID, e.Version, e.Field, e.Reason)
}

// ErrImplementationUnbound reports a definition whose implementation binding
// does not resolve to a handler.
type ErrImplementationUnbound struct {
	ID      string
	Version uint32
}

func (ErrImplementationUnbound) Code() string { return CodeImplementationUnbound }

func (e ErrImplementationUnbound) Error() string {
	return fmt.Sprintf("%s: capability %s/v%d has no bound implementation handler", CodeImplementationUnbound, e.ID, e.Version)
}

// ErrAlreadyRegistered reports an attempt to register an (ID, Version) that
// is already published. Published capability versions are immutable.
type ErrAlreadyRegistered struct {
	ID      string
	Version uint32
}

func (ErrAlreadyRegistered) Code() string { return CodeAlreadyRegistered }

func (e ErrAlreadyRegistered) Error() string {
	return fmt.Sprintf("%s: capability %s/v%d is already published and cannot be re-registered", CodeAlreadyRegistered, e.ID, e.Version)
}

// GatewayError is the one typed error CAP-002's gateway returns. Code is a
// stable machine-readable identifier; EvidenceID names the evidence record a
// transport layer can cite back to the caller.
type GatewayError struct {
	Code       string
	Capability string
	Version    uint32
	Reason     string
	EvidenceID string
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("%s: capability %s/v%d: %s (evidence %s)", e.Code, e.Capability, e.Version, e.Reason, e.EvidenceID)
}
