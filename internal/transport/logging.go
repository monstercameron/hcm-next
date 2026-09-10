package transport

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// LogRecord is the structured record emitted once per completed request. Its
// fields are the ones an operator needs to correlate a request across the two
// transports, and nothing else: there is no credential, no token digest, no
// request payload and no internal diagnostic on it.
type LogRecord struct {
	// Method is the fully qualified gRPC method name.
	Method string
	// Transport is the wire protocol the request arrived on.
	Transport Kind
	// RequestID is the correlation identifier.
	RequestID string
	// TenantID, SubjectID and Purpose are the server-derived trusted values.
	// They are empty when the request failed before authentication.
	TenantID  string
	SubjectID string
	Purpose   string
	// EvidenceID is the authentication evidence reference.
	EvidenceID string
	// Duration is the wall-clock time the request occupied.
	Duration time.Duration
	// Code is the owned outcome condition; [envelope.CodeUnspecified] means
	// the request succeeded.
	Code envelope.Code
	// ReasonRef is the owned reason identifier on a failure.
	ReasonRef string
	// ErrorType is a bounded classification, never the diagnostic message.
	ErrorType string
}

// Succeeded reports whether the record describes a successful request.
func (r LogRecord) Succeeded() bool { return r.Code == envelope.CodeUnspecified }

// Logger receives one [LogRecord] per completed request. It is a port so that
// the process's telemetry stack (OBS-*) plugs in without the transport
// depending on it.
type Logger interface {
	LogRequest(record LogRecord)
}

// LoggerFunc adapts a function to [Logger].
type LoggerFunc func(record LogRecord)

// LogRequest calls f.
func (f LoggerFunc) LogRequest(record LogRecord) { f(record) }

// NewLogRecord assembles the record for one completed request.
func NewLogRecord(method string, kind Kind, inv *Invocation, requestID string, duration time.Duration, err *envelope.Error) LogRecord {
	record := LogRecord{
		Method:    method,
		Transport: kind,
		RequestID: requestID,
		Duration:  duration,
	}
	if inv != nil {
		record.RequestID = inv.RequestID()
		record.TenantID = inv.TenantID()
		record.Purpose = inv.Purpose()
		record.EvidenceID = inv.EvidenceID()
		if p := inv.Principal(); p != nil {
			record.SubjectID = p.Subject()
		}
	}
	if err != nil {
		record.ErrorType = diagnosticType(err)
		record.Code = err.Code()
		record.ReasonRef = err.ReasonRef()
		if record.RequestID == "" {
			record.RequestID = err.CorrelationID()
		}
	}
	return record
}

type diagnosticClassificationGrant struct{}

func (diagnosticClassificationGrant) AllowsInternalDiagnostics() bool { return true }

func diagnosticType(err *envelope.Error) string {
	diagnostic, ok := err.Diagnostic(diagnosticClassificationGrant{})
	if !ok {
		return "UNCLASSIFIED"
	}
	if errors.Is(diagnostic, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(diagnostic, context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	var state interface{ SQLState() string }
	if errors.As(diagnostic, &state) {
		switch state.SQLState() {
		case "42703", "42P01":
			return "SCHEMA_MISMATCH"
		case "40001", "40P01":
			return "TRANSACTION_CONFLICT"
		case "23505":
			return "UNIQUENESS_CONFLICT"
		default:
			return "DATABASE_FAILURE"
		}
	}
	return "INTERNAL_FAILURE"
}
