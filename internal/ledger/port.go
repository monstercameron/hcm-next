package ledger

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Type aliases to the adapter's value types. They are plain data (a request,
// a receipt, an event record) with no behavior of their own, so aliasing
// rather than redeclaring them avoids a parallel, driftable copy while still
// letting every port method signature below live in this package.
type (
	AppendRequest  = datalogger.AppendRequest
	AppendReceipt  = datalogger.AppendReceipt
	AssertionClass = datalogger.AssertionClass
	EventRef       = datalogger.EventRef
	EventRecord    = datalogger.EventRecord
	Querier        = datalogger.Querier
)

// The five assertion classes, re-exported so a business package needs only
// this port import.
const (
	TransactionFact     = datalogger.TransactionFact
	DomainFact          = datalogger.DomainFact
	ExternalObservation = datalogger.ExternalObservation
	Claim               = datalogger.Claim
	Correction          = datalogger.Correction
)

// Appender appends assertions to a stream. internal/data/ledger.Appender
// implements it. Append runs inside the caller's own transaction and
// performs no external call of any kind (internal/data/ledger/append.go).
type Appender interface {
	Append(ctx context.Context, tx dbport.Tx, req AppendRequest) (AppendReceipt, error)
}

// Reader reads back committed ledger events for replay and verification.
// internal/data/ledger.Reader implements it.
type Reader interface {
	ReadStream(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string) ([]EventRecord, error)
	ReadEvent(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string, sequence int64) (EventRecord, error)
}

// Digester computes the canonical digest recorded on a ledger event. It
// matches internal/data/ledger.Digester's method exactly, so a [KernelDigester]
// plugs straight into internal/data/ledger.WithDigester.
type Digester interface {
	Digest(payload []byte, schemaRef string) (algorithm, digest string, length int, err error)
}
