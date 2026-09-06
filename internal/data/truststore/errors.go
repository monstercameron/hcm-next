package truststore

import (
	"errors"
	"fmt"
)

// Code is the stable classification of a persistence failure.
type Code string

const (
	CodeInvalid           Code = "INVALID"
	CodeNotFound          Code = "NOT_FOUND"
	CodeDuplicate         Code = "DUPLICATE"
	CodeDuplicateRevision Code = "DUPLICATE_REVISION"
	CodeDuplicateEvent    Code = "DUPLICATE_EVENT"
	CodeVersionConflict   Code = "VERSION_CONFLICT"
	CodeDatabase          Code = "DATABASE"
	CodeTenantRequired    Code = "TENANT_REQUIRED"
)

// Error is a typed adapter failure. Callers can branch on CodeOf without
// depending on PostgreSQL driver error strings, while Cause remains available
// to errors.Is for the underlying semantic error.
type Error struct {
	Code  Code
	Table string
	Key   string
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return "truststore: nil error"
	}
	message := string(e.Code)
	if e.Table != "" {
		message += " " + e.Table
	}
	if e.Key != "" {
		message += " " + e.Key
	}
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	return message
}

func (e *Error) Unwrap() error { return e.Cause }

// CodeOf returns the nearest truststore error code.
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
