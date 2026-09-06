package attestationstore

import (
	"errors"
	"fmt"
)

// Code classifies adapter failures for callers that need a stable decision
// without matching PostgreSQL error strings.
type Code string

const (
	CodeInvalid             Code = "INVALID"
	CodeNotFound            Code = "NOT_FOUND"
	CodeDuplicateRevision   Code = "DUPLICATE_REVISION"
	CodeDuplicateEvent      Code = "DUPLICATE_EVENT"
	CodeVersionConflict     Code = "VERSION_CONFLICT"
	CodeIdempotencyConflict Code = "IDEMPOTENCY_CONFLICT"
	CodeDatabase            Code = "DATABASE"
)

// Error is a typed persistence failure. Cause remains available through
// errors.Is for the domain's semantic sentinels.
type Error struct {
	Code  Code
	Table string
	Key   string
	Cause error
}

func (e *Error) Error() string {
	msg := string(e.Code)
	if e.Table != "" {
		msg += " " + e.Table
	}
	if e.Key != "" {
		msg += " " + e.Key
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Cause }

// CodeOf returns the stable code carried by the nearest adapter Error.
func CodeOf(err error) Code {
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func failure(code Code, table, key string, cause error) error {
	return &Error{Code: code, Table: table, Key: key, Cause: cause}
}

func invalid(table, key, detail string) error {
	return failure(CodeInvalid, table, key, fmt.Errorf("%s", detail))
}
