package signal

import (
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// LogEntry is one immutable, inspectable record of an Accept evaluation. It
// retains the signal's payload/taint classification exactly as received
// (REFACTOR: payload retains taint/classification) so an accepted, duplicate
// or refused delivery can be reviewed without re-deriving it.
type LogEntry struct {
	SubscriptionDigest string
	Signal             Signal
	Status             Status
	Continuation       bool
	Reason             string
	RecordedAt         values.Instant
	Digest             string
}

// RecordedAtText returns the canonical text of RecordedAt, or "" when unset.
func (e LogEntry) RecordedAtText() string { return instantText(e.RecordedAt) }

func newLogEntry(sub SignalSubscription, sig Signal, result Result, now values.Instant) LogEntry {
	e := LogEntry{
		SubscriptionDigest: sub.Digest(),
		Signal:             sig.immutable(),
		Status:             result.Status,
		Continuation:       result.Continuation,
		Reason:             result.Reason,
		RecordedAt:         now,
	}
	e.Digest = computeLogEntryDigest(e)
	return e
}

// SignalLog is a small in-memory, concurrency-safe append log of every Accept
// evaluation for a set of subscriptions. It holds no policy of its own: it
// exists so accepted and duplicate signals "stay inspectable" without a
// caller having to thread a growing slice through every call by hand. The
// decision logic itself lives in the pure [Accept] function; SignalLog.Accept
// is a thin, stateful wrapper around it.
type SignalLog struct {
	mu      sync.Mutex
	entries []LogEntry
}

// NewSignalLog returns an empty log.
func NewSignalLog() *SignalLog { return &SignalLog{} }

// Accept evaluates sig against sub using every entry this log already holds
// for sub's digest, appends the resulting LogEntry, and returns the Result.
func (l *SignalLog) Accept(sub SignalSubscription, sig Signal, verify Verifier, now values.Instant) (Result, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	digest := sub.Digest()
	prior := make([]LogEntry, 0, len(l.entries))
	for _, e := range l.entries {
		if e.SubscriptionDigest == digest {
			prior = append(prior, e)
		}
	}

	result, err := Accept(sub, sig, prior, verify, now)
	if err != nil {
		return Result{}, err
	}
	l.entries = append(l.entries, newLogEntry(sub, sig, result, now))
	return result, nil
}

// Entries returns a copy of every entry this log holds, in append order.
// Mutating the returned slice or its elements never affects the log.
func (l *SignalLog) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LogEntry, len(l.entries))
	copy(out, l.entries)
	for i := range out {
		out[i].Signal = out[i].Signal.immutable()
	}
	return out
}

// For returns a copy of every entry recorded against the subscription whose
// digest is subscriptionDigest, in append order.
func (l *SignalLog) For(subscriptionDigest string) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []LogEntry
	for _, e := range l.entries {
		if e.SubscriptionDigest == subscriptionDigest {
			e.Signal = e.Signal.immutable()
			out = append(out, e)
		}
	}
	return out
}

// Len returns the number of entries recorded.
func (l *SignalLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
