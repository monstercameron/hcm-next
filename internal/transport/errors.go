package transport

import (
	"context"
	"errors"

	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

// OwnedError converts an error returned by a handler port into the canonical
// owned error, and stamps it with the request's correlation and authentication
// evidence references when the handler did not supply its own.
//
// Both transports call it on exactly one code path, which is why the same
// handler failure produces the same owned code, retry classification,
// correlation identifier and evidence reference on gRPC and on the HTTP edge.
func OwnedError(err error, inv *Invocation) *envelope.Error {
	if err == nil {
		return nil
	}
	owned := classify(err)
	if inv == nil {
		return owned
	}
	if owned.CorrelationID() == "" {
		owned.WithCorrelation(inv.RequestID())
	}
	if owned.EvidenceRef().ID == "" && inv.EvidenceID() != "" {
		owned.WithEvidence(envelope.Evidence{ID: inv.EvidenceID(), Kind: "authentication"})
	}
	return owned
}

// classify maps a handler error onto an owned condition. Context expiry and
// cancellation are recognized explicitly because they are transport facts, not
// unclassified failures; anything else that is not already owned becomes a
// generic condition with the original nested as an unprojected diagnostic.
func classify(err error) *envelope.Error {
	if owned, ok := envelope.As(err); ok {
		return owned
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return envelope.New(envelope.CodeDeadlineExceeded, "transport.deadline_exceeded",
			"the caller deadline expired before a result was known").WithDiagnostic(err)
	case errors.Is(err, context.Canceled):
		return envelope.New(envelope.CodeUnavailable, "transport.request_canceled",
			"the request was canceled before a result was known").WithDiagnostic(err)
	default:
		return envelope.Coerce(err)
	}
}
