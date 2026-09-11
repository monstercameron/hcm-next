package schedule

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/eventpolicy"
)

var (
	// ErrInvalidFiring reports a firing that cannot dispatch: an unknown
	// kind, an empty key or a zero sequence.
	ErrInvalidFiring = errors.New("schedule: invalid trigger firing")

	// ErrCursorRewind reports a new firing behind the dispatch cursor:
	// history never replays under a fresh key.
	ErrCursorRewind = errors.New("schedule: firing behind dispatch cursor")
)

// FiringKind is OCCURRENCE or EVENT.
type FiringKind string

// Firing kinds.
const (
	FiringOccurrence FiringKind = "OCCURRENCE"
	FiringEvent      FiringKind = "EVENT"
)

// Firing is one ingested trigger firing: either a frozen schedule
// occurrence or an event observation, each carrying everything its
// converter needs. The dispatcher names intents through the converters
// only; it never invokes domain, workflow or provider effects directly.
type Firing struct {
	Kind         FiringKind
	Key          string
	Sequence     uint64
	ObservedAt   time.Time
	Occurrence   Occurrence
	Trigger      PublishedTrigger
	TargetScope  []string
	Leader       LeaderClaim
	AllowCatchUp bool
	Policy       eventpolicy.TriggerPolicy
	Observation  eventpolicy.Observation
	Resolve      eventpolicy.TruthResolver
}

// DispatchOutcome is one durable dispatch record: exactly one created
// intent set or one typed receipt, replayable by key.
type DispatchOutcome struct {
	Key       string
	Sequence  uint64
	Duplicate bool
	Created   bool
	IntentIDs []string
	Receipt   string
	Evidence  string
}

// DispatchPolicy bounds one dispatcher.
type DispatchPolicy struct {
	MaxAge time.Duration
	Clock  func() time.Time
}

// Dispatcher ingests trigger firings in sequence and dispatches each key
// exactly once through the occurrence or event converter. It is safe for
// concurrent use.
type Dispatcher struct {
	mu        sync.Mutex
	policy    DispatchPolicy
	clock     func() time.Time
	converter *Converter
	events    *eventpolicy.Converter
	cursor    uint64
	log       map[string]DispatchOutcome
}

// NewDispatcher validates one dispatch policy.
func NewDispatcher(policy DispatchPolicy) (*Dispatcher, error) {
	if policy.MaxAge <= 0 {
		return nil, fmt.Errorf("schedule: NewDispatcher: %w", ErrInvalidFiring)
	}
	clock := policy.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Dispatcher{
		policy:    policy,
		clock:     clock,
		converter: NewConverter(),
		events:    eventpolicy.NewConverter(),
		log:       make(map[string]DispatchOutcome),
	}, nil
}

// Dispatch records one firing exactly once. Redeliveries replay the
// stored outcome; rewinds and poison fail without recording anything.
func (d *Dispatcher) Dispatch(firing Firing) (DispatchOutcome, error) {
	if (firing.Kind != FiringOccurrence && firing.Kind != FiringEvent) ||
		strings.TrimSpace(firing.Key) == "" || firing.Sequence == 0 {
		return DispatchOutcome{}, fmt.Errorf("schedule: Dispatch: %w", ErrInvalidFiring)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if prior, ok := d.log[firing.Key]; ok {
		prior.Duplicate = true
		return prior, nil
	}
	if firing.Sequence <= d.cursor {
		return DispatchOutcome{}, fmt.Errorf("schedule: Dispatch: %w", ErrCursorRewind)
	}
	if d.clock().Sub(firing.ObservedAt) > d.policy.MaxAge {
		outcome := DispatchOutcome{Key: firing.Key, Sequence: firing.Sequence, Receipt: "STALE",
			Evidence: "STALE: firing past the dispatch threshold names no intent"}
		d.log[firing.Key] = outcome
		d.cursor = firing.Sequence
		return outcome, nil
	}
	var outcome DispatchOutcome
	var err error
	if firing.Kind == FiringOccurrence {
		outcome, err = d.dispatchOccurrence(firing)
	} else {
		outcome, err = d.dispatchEvent(firing)
	}
	if err != nil {
		return DispatchOutcome{}, err
	}
	d.log[firing.Key] = outcome
	d.cursor = firing.Sequence
	return outcome, nil
}

// dispatchOccurrence routes one occurrence through the intent converter.
func (d *Dispatcher) dispatchOccurrence(firing Firing) (DispatchOutcome, error) {
	conversion, err := d.converter.Convert(ConversionRequest{
		Trigger:      firing.Trigger,
		Occurrence:   firing.Occurrence,
		TargetScope:  firing.TargetScope,
		Leader:       firing.Leader,
		AllowCatchUp: firing.AllowCatchUp,
	})
	if err != nil {
		return DispatchOutcome{}, err
	}
	outcome := DispatchOutcome{Key: firing.Key, Sequence: firing.Sequence}
	if conversion.Intent != nil {
		outcome.Created = true
		outcome.IntentIDs = []string{conversion.Intent.IntentID}
		outcome.Evidence = "CREATED: occurrence named one intent"
	} else {
		outcome.Receipt = string(conversion.Receipt.Disposition)
		outcome.Evidence = string(conversion.Receipt.Disposition) + ": " + conversion.Receipt.Reason
	}
	return outcome, nil
}

// dispatchEvent routes one observation through the event policy converter.
func (d *Dispatcher) dispatchEvent(firing Firing) (DispatchOutcome, error) {
	result, err := d.events.Convert(firing.Policy, firing.Observation, firing.Resolve)
	if err != nil {
		return DispatchOutcome{}, err
	}
	outcome := DispatchOutcome{Key: firing.Key, Sequence: firing.Sequence}
	if result.Decision == eventpolicy.DecisionAccepted {
		outcome.Created = true
		for _, named := range result.Intents {
			outcome.IntentIDs = append(outcome.IntentIDs, named.IntentID)
		}
		outcome.Evidence = "CREATED: event accepted under its policy"
	} else {
		outcome.Receipt = string(result.Decision)
		outcome.Evidence = result.Evidence
	}
	return outcome, nil
}

// Cursor returns the last dispatched sequence.
func (d *Dispatcher) Cursor() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cursor
}
