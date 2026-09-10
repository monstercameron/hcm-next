package delivery

// Messaging workflow signals (MSG-008): typed satisfied, failed,
// acknowledged and fallback-required signals correlate exactly once to
// one intent, task and workflow with their evidence preserved. A signal
// derives from the honest attempt and recipient states: duplicate or
// provider-only state never resumes a workflow twice, and an unsatisfied
// mandatory requirement never advances. Workflow owns waits and
// escalation; this package owns the signal truth.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// SignalKind is the closed signal vocabulary.
type SignalKind string

const (
	SignalSatisfied        SignalKind = "SATISFIED"
	SignalFailed           SignalKind = "FAILED"
	SignalAcknowledged     SignalKind = "ACKNOWLEDGED"
	SignalFallbackRequired SignalKind = "FALLBACK_REQUIRED"
)

// Requirement states the mandatory bar for one correlation.
type Requirement struct {
	NeedDelivery    bool
	NeedRead        bool
	NeedAck         bool
	FallbackChannel string
}

// Correlation binds one attempt and recipient to one workflow target.
type Correlation struct {
	Attempt    Attempt
	Recipient  Recipient
	IntentID   string
	TaskID     string
	WorkflowID string
}

// Signal is one typed, evidenced workflow signal.
type Signal struct {
	ID         string
	Kind       SignalKind
	IntentID   string
	TaskID     string
	WorkflowID string
	Evidence   string
}

var (
	// ErrSignalCorrelation reports an attempt/recipient pair that cannot
	// correlate.
	ErrSignalCorrelation = errors.New("delivery: cannot correlate signal")
	// ErrRequirementUnmet reports an unsatisfied mandatory requirement:
	// the workflow must not advance.
	ErrRequirementUnmet = errors.New("delivery: mandatory requirement unmet")
	// ErrDuplicateSignal reports a signal already emitted: the stored one
	// resumes the workflow, never a second.
	ErrDuplicateSignal = errors.New("delivery: signal already emitted")
)

func signalID(intentID, attemptID string, kind SignalKind) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"workflow-signal", intentID, attemptID, string(kind)}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Correlate derives exactly one signal from honest states. The recipient
// must belong to the attempt; terminal failure without a fallback
// channel fails; mandatory delivery, read and acknowledgement bars hold
// before any advance.
func Correlate(correlation Correlation, requirement Requirement) (Signal, error) {
	attempt, recipient := correlation.Attempt, correlation.Recipient
	if attempt.ID == "" || recipient.AttemptID != attempt.ID {
		return Signal{}, fmt.Errorf("%w: recipient does not belong to the attempt", ErrSignalCorrelation)
	}
	if correlation.IntentID == "" || correlation.TaskID == "" || correlation.WorkflowID == "" {
		return Signal{}, fmt.Errorf("%w: intent, task and workflow are required", ErrSignalCorrelation)
	}
	evidence := fmt.Sprintf("attempt=%s recipient=%s revisions=%d", attempt.State, recipient.State, recipient.Revision)
	terminalFailure := attempt.State == Failed || attempt.State == Bounced || attempt.State == Expired
	if terminalFailure {
		if requirement.FallbackChannel != "" {
			return Signal{
				ID:   signalID(correlation.IntentID, attempt.ID, SignalFallbackRequired),
				Kind: SignalFallbackRequired, IntentID: correlation.IntentID,
				TaskID: correlation.TaskID, WorkflowID: correlation.WorkflowID,
				Evidence: evidence + " fallback=" + requirement.FallbackChannel,
			}, nil
		}
		return Signal{
			ID:   signalID(correlation.IntentID, attempt.ID, SignalFailed),
			Kind: SignalFailed, IntentID: correlation.IntentID,
			TaskID: correlation.TaskID, WorkflowID: correlation.WorkflowID,
			Evidence: evidence,
		}, nil
	}
	if requirement.NeedDelivery {
		if err := RequireDelivered(attempt); err != nil {
			return Signal{}, fmt.Errorf("%w: delivery bar: %v", ErrRequirementUnmet, err)
		}
	}
	if requirement.NeedRead {
		if err := RequireRead(recipient); err != nil {
			return Signal{}, fmt.Errorf("%w: read bar: %v", ErrRequirementUnmet, err)
		}
	}
	if requirement.NeedAck {
		if err := RequireAcknowledged(recipient); err != nil {
			return Signal{}, fmt.Errorf("%w: acknowledgement bar: %v", ErrRequirementUnmet, err)
		}
	}
	kind := SignalSatisfied
	if recipient.State == RecipientAcknowledged || recipient.State == RecipientResponded {
		kind = SignalAcknowledged
	}
	return Signal{
		ID:   signalID(correlation.IntentID, attempt.ID, kind),
		Kind: kind, IntentID: correlation.IntentID,
		TaskID: correlation.TaskID, WorkflowID: correlation.WorkflowID,
		Evidence: evidence,
	}, nil
}

// Signaller emits each signal exactly once: a duplicate emits the stored
// signal so the workflow resumes once, never twice.
type Signaller struct {
	mu      sync.Mutex
	emitted map[string]Signal
}

// NewSignaller returns an empty emitter.
func NewSignaller() *Signaller {
	return &Signaller{emitted: make(map[string]Signal)}
}

// Emit records one signal or returns the stored original on redelivery.
func (s *Signaller) Emit(signal Signal) (Signal, bool, error) {
	if signal.ID == "" || signal.IntentID == "" || signal.TaskID == "" || signal.WorkflowID == "" || signal.Evidence == "" {
		return Signal{}, false, fmt.Errorf("%w: signal identity, targets and evidence are required", ErrSignalCorrelation)
	}
	switch signal.Kind {
	case SignalSatisfied, SignalFailed, SignalAcknowledged, SignalFallbackRequired:
	default:
		return Signal{}, false, fmt.Errorf("%w: unknown kind %q", ErrSignalCorrelation, signal.Kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.emitted[signal.ID]; ok {
		return stored, true, nil
	}
	s.emitted[signal.ID] = signal
	return signal, false, nil
}

// Count reports emitted signals.
func (s *Signaller) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.emitted)
}
