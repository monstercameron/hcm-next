package replay

import (
	"context"
	"sync"
	"time"
)

// EffectAttempt names one thing an adapter may be asked to do during a run.
//
// It is a closed vocabulary rather than a free-form string because a replay's
// whole guarantee is "none of these, ever", and an attempt a reader could not
// classify would be an attempt an auditor could not rule on.
type EffectAttempt string

// The declared attempts.
const (
	// AttemptGovernedRead is a read of governed material. In a replay it is
	// answered from the record and never from the world.
	AttemptGovernedRead EffectAttempt = "GOVERNED_READ"
	// AttemptDomainWrite commits domain truth.
	AttemptDomainWrite EffectAttempt = "DOMAIN_WRITE"
	// AttemptExternalCall reaches a destination outside the system.
	AttemptExternalCall EffectAttempt = "EXTERNAL_CALL"
	// AttemptMessageSend delivers a message to a person.
	AttemptMessageSend EffectAttempt = "MESSAGE_SEND"
	// AttemptApprovalConsumption spends a live human approval.
	AttemptApprovalConsumption EffectAttempt = "APPROVAL_CONSUMPTION"
)

// EffectAttempts returns the five declared attempts, in a fixed order.
func EffectAttempts() []EffectAttempt {
	return []EffectAttempt{
		AttemptGovernedRead, AttemptDomainWrite, AttemptExternalCall,
		AttemptMessageSend, AttemptApprovalConsumption,
	}
}

// NodeAdapter is any adapter a caller wires behind a node -- a capability
// client, an outbox, a notifier.
//
// A [Replayer] accepts one so that the composition under replay is the same
// composition that ran, rather than a second wiring a test could get wrong.
// It never calls it: every attempt is refused by the [Recorder] first, and the
// adapter is invoked zero times. That is the SECURITY property, and it is
// structural -- there is no code path from [Replayer.Replay] to Invoke.
type NodeAdapter interface {
	Invoke(ctx context.Context, nodeID string, attempt EffectAttempt) error
}

// Refusal is one recorded attempt a [Recorder] turned away.
type Refusal struct {
	NodeID  string        `json:"node_id"`
	Attempt EffectAttempt `json:"attempt"`
	Code    string        `json:"code"`
}

// Recorder is the adapter set a replay binds in place of the live one.
//
// It answers governed reads out of the [Record] and refuses everything else
// with [CodeEffectForbidden], naming the node that tried. The refusal is the
// point: a replay that suppressed an effect silently would look identical to
// one that never attempted it, and only one of those is a correct replay.
//
// It is safe for concurrent use so a caller may drive several nodes' adapters
// from one recorder; the replay itself is serial.
type Recorder struct {
	mu       sync.Mutex
	rec      Record
	refusals []Refusal
	draws    map[string]int
}

// NewRecorder returns a recorder answering from a copy of rec.
func NewRecorder(rec Record) *Recorder {
	return &Recorder{rec: rec.Clone(), draws: map[string]int{}}
}

// Attempt decides one adapter attempt at one node.
//
// A governed read is admitted, because reading recorded material is the whole
// of what a replay does. Everything else is [CodeEffectForbidden] and is
// recorded in [Recorder.Refusals].
func (r *Recorder) Attempt(_ context.Context, nodeID string, attempt EffectAttempt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if attempt == AttemptGovernedRead {
		return nil
	}
	r.refusals = append(r.refusals, Refusal{NodeID: nodeID, Attempt: attempt, Code: CodeEffectForbidden})
	return refuse(CodeEffectForbidden, nodeID,
		"REPLAY re-derives history and reaches nothing; %s was attempted", attempt)
}

// Refusals returns every attempt this recorder turned away, in attempt order.
func (r *Recorder) Refusals() []Refusal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Refusal(nil), r.refusals...)
}

// Output returns the recorded attempt for one node, or
// [CodeArtifactUnavailable] when the record does not hold it.
//
// A node may hold more than one record for the same attempt number -- an
// APPROVAL that awaits and is later resumed is two entries at attempt 1 -- and
// this returns the first in record order. A caller that needs one exact entry
// addresses it by [NodeRecord.Sequence] through [Record.Ordered] instead; the
// replayer itself never goes through here, because it drives from the ordered
// record directly.
func (r *Recorder) Output(nodeID string, attempt int) (NodeRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range r.rec.Nodes {
		if n.NodeID == nodeID && n.Attempt == attempt {
			return n, nil
		}
	}
	return NodeRecord{}, refuse(CodeArtifactUnavailable, nodeID,
		"the record holds no output for attempt %d", attempt)
}

// Now returns the instant the historical run stamped on one attempt.
//
// It is the only clock this package has. There is no fallback to
// [time.Now]: an attempt the record carries no instant for is
// [CodeArtifactUnavailable], because a replay that invented a timestamp would
// produce a trace that never happened.
func (r *Recorder) Now(nodeID string, attempt int) (time.Time, error) {
	out, err := r.Output(nodeID, attempt)
	if err != nil {
		return time.Time{}, err
	}
	if out.RecordedAt.IsZero() {
		return time.Time{}, refuse(CodeArtifactUnavailable, nodeID,
			"the record carries no instant for attempt %d", attempt)
	}
	return out.RecordedAt.UTC(), nil
}

// Draw returns the next random value the historical attempt drew at this node,
// in the order it drew them. A draw the record does not hold is
// [CodeArtifactUnavailable] rather than a fresh random value.
func (r *Recorder) Draw(nodeID string, attempt int) (string, error) {
	out, err := r.Output(nodeID, attempt)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := nodeID + "\x00" + itoa(attempt)
	i := r.draws[key]
	if i >= len(out.RandomDraws) {
		return "", refuse(CodeArtifactUnavailable, nodeID,
			"the record holds %d random draws for attempt %d; draw %d was requested",
			len(out.RandomDraws), attempt, i+1)
	}
	r.draws[key] = i + 1
	return out.RandomDraws[i], nil
}

// Signal returns the recorded delivery a waiting node consumed.
func (r *Recorder) Signal(nodeID, signalID string) (SignalReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.rec.Signal(signalID)
	if !ok {
		return SignalReceipt{}, refuse(CodeArtifactUnavailable, nodeID,
			"the record holds no delivery for signal %s", signalID)
	}
	return s, nil
}

// Timer returns the recorded settlement a waiting node was woken by.
func (r *Recorder) Timer(nodeID, timerID string) (TimerSettlement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.rec.Timer(timerID)
	if !ok {
		return TimerSettlement{}, refuse(CodeArtifactUnavailable, nodeID,
			"the record holds no settlement for timer %s", timerID)
	}
	return t, nil
}

// guardedAdapter wraps a caller-supplied [NodeAdapter] behind a [Recorder].
//
// Invoke refuses before it would delegate, so the wrapped adapter is never
// called. It exists as a named type rather than an inline closure so that "the
// adapter is unreachable" is a property a reader can check by reading one
// function.
type guardedAdapter struct {
	recorder *Recorder
	inner    NodeAdapter
}

var _ NodeAdapter = guardedAdapter{}

func (g guardedAdapter) Invoke(ctx context.Context, nodeID string, attempt EffectAttempt) error {
	if err := g.recorder.Attempt(ctx, nodeID, attempt); err != nil {
		return err
	}
	// Only a governed read reaches here, and a governed read under replay is
	// answered from the record, never delegated: the wrapped adapter is
	// unreachable by construction.
	return nil
}

// itoa is a tiny local integer formatter, kept here so the draw key does not
// pull strconv into a file whose whole subject is refusing to do things.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
