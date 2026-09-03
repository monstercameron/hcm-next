package bitemporal

import (
	"fmt"

	"github.com/google/uuid"
)

// Stable error codes. A transport layer can switch on Code() without parsing
// the message text.
const (
	CodeRequestInvalid  = "BITEMPORAL_REQUEST_INVALID"
	CodeTenantMismatch  = "BITEMPORAL_TENANT_MISMATCH"
	CodeCursorInvalid   = "BITEMPORAL_CURSOR_INVALID"
	CodeDecisionInvalid = "BITEMPORAL_DECISION_INVALID"
)

// ErrRequestInvalid reports a missing or malformed field on a Request.
type ErrRequestInvalid struct {
	Field  string
	Reason string
}

// Code returns the stable validation identifier.
func (ErrRequestInvalid) Code() string { return CodeRequestInvalid }

func (e ErrRequestInvalid) Error() string {
	return fmt.Sprintf("%s: %s %s", CodeRequestInvalid, e.Field, e.Reason)
}

// ErrTenantMismatch reports a Request naming a tenant other than the one the
// Decision authorizes. A query is refused before any SQL is built rather
// than executed and filtered - a Decision never widens a query's tenant.
type ErrTenantMismatch struct {
	RequestTenant  uuid.UUID
	DecisionTenant uuid.UUID
}

// Code returns the stable validation identifier.
func (ErrTenantMismatch) Code() string { return CodeTenantMismatch }

func (e ErrTenantMismatch) Error() string {
	return fmt.Sprintf("%s: request tenant %s does not match the authorized decision tenant %s",
		CodeTenantMismatch, e.RequestTenant, e.DecisionTenant)
}

// ErrCursorInvalid reports a page cursor that does not decode, or that was
// minted for a different tenant, mode, subject or field than the request now
// carries. A cursor from one authorized query is never honored inside a
// differently scoped one.
type ErrCursorInvalid struct {
	Reason string
}

// Code returns the stable validation identifier.
func (ErrCursorInvalid) Code() string { return CodeCursorInvalid }

func (e ErrCursorInvalid) Error() string {
	return fmt.Sprintf("%s: %s", CodeCursorInvalid, e.Reason)
}

// ErrDecisionInvalid reports a Decision that cannot be evaluated, such as a
// missing tenant.
type ErrDecisionInvalid struct {
	Reason string
}

// Code returns the stable validation identifier.
func (ErrDecisionInvalid) Code() string { return CodeDecisionInvalid }

func (e ErrDecisionInvalid) Error() string {
	return fmt.Sprintf("%s: %s", CodeDecisionInvalid, e.Reason)
}
