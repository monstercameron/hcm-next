package hashchain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EventDigest is the minimal fact a chain link folds: which event occupies a
// sequence, and the canonical digest LEDGER-002/DATA-003 already computed
// for it. It carries nothing about payload content - the chain never touches
// domain bytes, only their digest.
type EventDigest struct {
	Sequence int64
	EventID  uuid.UUID
	Digest   string
}

// ChainedLink is one durable row of the hash chain: the event it links, the
// prior chain hash it extends, and the chain hash it produces. It is what
// [Appender.Append] writes and [ReadLinks] reads back.
type ChainedLink struct {
	StreamKey  string
	Sequence   int64
	EventID    uuid.UUID
	PrevHash   string
	ChainHash  string
	Algorithm  string
	RecordedAt time.Time
}

// Head is a stream's chain head checkpoint: the chain hash reproduced by
// folding every event digest on the stream from genesis through Sequence, in
// order. Two independent replays of the same stream produce the same Head -
// that reproducibility is what DATA-004 calls "replay proves the head is
// stable."
type Head struct {
	StreamKey string
	Sequence  int64
	ChainHash string
	Algorithm string
}

// ErrChainBroken reports the exact first stream and sequence at which
// verification could not extend the chain: a gap, a reordering, a
// duplicated or substituted event, a prior-hash mismatch, or a chain hash
// that does not reproduce from the recorded event digest. Reason is a fixed,
// human-readable classification; callers that need to branch on the kind of
// break should not parse it and should instead treat any ErrChainBroken as
// "this stream's integrity could not be confirmed at or before Sequence."
type ErrChainBroken struct {
	StreamKey string
	Sequence  int64
	Reason    string
	Expected  string
	Actual    string
}

func (ErrChainBroken) Code() string { return "LEDGER_HASH_CHAIN_BROKEN" }

func (e ErrChainBroken) Error() string {
	if e.Expected == "" && e.Actual == "" {
		return fmt.Sprintf("%s: stream %s sequence %d: %s", e.Code(), e.StreamKey, e.Sequence, e.Reason)
	}
	return fmt.Sprintf("%s: stream %s sequence %d: %s (expected %s, got %s)",
		e.Code(), e.StreamKey, e.Sequence, e.Reason, e.Expected, e.Actual)
}

// ErrEmptyStream reports that a stream has no events and therefore no chain
// head to verify or report.
type ErrEmptyStream struct {
	StreamKey string
}

func (ErrEmptyStream) Code() string { return "LEDGER_HASH_CHAIN_EMPTY_STREAM" }

func (e ErrEmptyStream) Error() string {
	return fmt.Sprintf("%s: stream %s has no events", e.Code(), e.StreamKey)
}

// ErrMissingPredecessor reports that [Appender.Append] was asked to extend a
// stream at a sequence greater than one without a recorded link for the
// immediately prior sequence. It is a programmer error - callers must append
// chain links in strict sequence order, in the same transaction as the
// corresponding internal/data/ledger.Append.
type ErrMissingPredecessor struct {
	StreamKey string
	Sequence  int64
}

func (ErrMissingPredecessor) Code() string { return "LEDGER_HASH_CHAIN_MISSING_PREDECESSOR" }

func (e ErrMissingPredecessor) Error() string {
	return fmt.Sprintf("%s: stream %s has no chain link recorded for sequence %d, the predecessor of %d",
		e.Code(), e.StreamKey, e.Sequence-1, e.Sequence)
}

// ErrLinkAlreadyRecorded reports a second attempt to record a chain link for
// a (stream, sequence) that already has one. Chain links are append-only,
// exactly like the ledger events they extend: a second attempt is refused,
// never silently overwritten.
type ErrLinkAlreadyRecorded struct {
	StreamKey string
	Sequence  int64
}

func (ErrLinkAlreadyRecorded) Code() string { return "LEDGER_HASH_CHAIN_LINK_ALREADY_RECORDED" }

func (e ErrLinkAlreadyRecorded) Error() string {
	return fmt.Sprintf("%s: stream %s already has a chain link recorded at sequence %d",
		e.Code(), e.StreamKey, e.Sequence)
}
