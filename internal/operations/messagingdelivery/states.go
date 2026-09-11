package delivery

// Honest delivery and recipient states (MSG-007): the delivery attempt
// and the recipient read two distinct lifecycles that never stand in for
// each other. Attempts move queued/submitted/accepted/delivered/bounced/
// failed/expired; recipients move unseen/seen/read/acknowledged/
// responded. ACCEPTED_BY_PROVIDER satisfies no delivered, read or
// acknowledged requirement, and bounced or expired attempts are retained
// states, never disappearances. Material transitions ledger through the
// tracker; transport detail stays operational.

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Extended attempt states: delivery confirmations and dead ends past
// provider acceptance.
const (
	Delivered State = "DELIVERED"
	Bounced   State = "BOUNCED"
	Expired   State = "EXPIRED"
)

// AttemptEvent is one closable provider or lifecycle signal.
type AttemptEvent string

const (
	EventSubmit  AttemptEvent = "SUBMIT"
	EventAccept  AttemptEvent = "ACCEPT"
	EventDeliver AttemptEvent = "DELIVER"
	EventBounce  AttemptEvent = "BOUNCE"
	EventFail    AttemptEvent = "FAIL"
	EventExpire  AttemptEvent = "EXPIRE"
)

// RecipientState is the closed recipient vocabulary.
type RecipientState string

const (
	RecipientUnseen       RecipientState = "UNSEEN"
	RecipientSeen         RecipientState = "SEEN"
	RecipientRead         RecipientState = "READ"
	RecipientAcknowledged RecipientState = "ACKNOWLEDGED"
	RecipientResponded    RecipientState = "RESPONDED"
)

// RecipientEvent is one user signal.
type RecipientEvent string

const (
	EventSeen        RecipientEvent = "SEEN"
	EventRead        RecipientEvent = "READ"
	EventAcknowledge RecipientEvent = "ACKNOWLEDGE"
	EventRespond     RecipientEvent = "RESPOND"
)

// Recipient is one recipient's read state for one attempt.
type Recipient struct {
	AttemptID    string
	RecipientRef string
	State        RecipientState
	Revision     uint64
}

var (
	// ErrAttemptTransition reports an event the attempt cannot take from
	// its current state.
	ErrAttemptTransition = errors.New("delivery: invalid attempt transition")
	// ErrRecipientTransition reports a signal the recipient cannot take.
	ErrRecipientTransition = errors.New("delivery: invalid recipient transition")
	// ErrDeliveryRequirement reports an unmet delivered/read/acknowledged
	// requirement.
	ErrDeliveryRequirement = errors.New("delivery: requirement not satisfied")
)

// AdvanceAttempt moves one attempt forward. Terminal states (delivered,
// bounced, failed, expired) take no further event: dead ends are
// retained, never rewritten or dropped.
func AdvanceAttempt(attempt Attempt, event AttemptEvent, providerRef string) (Attempt, error) {
	next, ok := map[State]map[AttemptEvent]State{
		Queued:             {EventSubmit: Submitted, EventFail: Failed},
		Submitted:          {EventAccept: AcceptedByProvider, EventFail: Failed},
		AcceptedByProvider: {EventDeliver: Delivered, EventBounce: Bounced, EventFail: Failed, EventExpire: Expired},
	}[attempt.State][event]
	if !ok {
		return Attempt{}, fmt.Errorf("%w: %s takes no %s", ErrAttemptTransition, attempt.State, event)
	}
	attempt.State = next
	if event == EventAccept || event == EventDeliver {
		if providerRef != "" {
			attempt.ProviderRef = providerRef
		}
	}
	if event == EventFail && attempt.ErrorCode == "" {
		attempt.ErrorCode = "PROVIDER_FAILED"
	}
	return attempt, nil
}

// AdvanceRecipient moves one recipient signal forward, one step at a
// time: unseen to seen to read to acknowledged to responded.
func AdvanceRecipient(recipient Recipient, event RecipientEvent) (Recipient, error) {
	next, ok := map[RecipientState]map[RecipientEvent]RecipientState{
		RecipientUnseen:       {EventSeen: RecipientSeen},
		RecipientSeen:         {EventRead: RecipientRead},
		RecipientRead:         {EventAcknowledge: RecipientAcknowledged},
		RecipientAcknowledged: {EventRespond: RecipientResponded},
	}[recipient.State][event]
	if !ok {
		return Recipient{}, fmt.Errorf("%w: %s takes no %s", ErrRecipientTransition, recipient.State, event)
	}
	recipient.State = next
	recipient.Revision++
	return recipient, nil
}

// RequireDelivered proves provider delivery: acceptance alone never
// satisfies it.
func RequireDelivered(attempt Attempt) error {
	if attempt.State != Delivered {
		return fmt.Errorf("%w: attempt is %s, not delivered", ErrDeliveryRequirement, attempt.State)
	}
	return nil
}

// RequireRead proves the recipient read the message: no attempt state
// stands in for it.
func RequireRead(recipient Recipient) error {
	if recipient.State != RecipientRead && recipient.State != RecipientAcknowledged && recipient.State != RecipientResponded {
		return fmt.Errorf("%w: recipient is %s, not read", ErrDeliveryRequirement, recipient.State)
	}
	return nil
}

// RequireAcknowledged proves explicit acknowledgement.
func RequireAcknowledged(recipient Recipient) error {
	if recipient.State != RecipientAcknowledged && recipient.State != RecipientResponded {
		return fmt.Errorf("%w: recipient is %s, not acknowledged", ErrDeliveryRequirement, recipient.State)
	}
	return nil
}

// Tracker ledgers material attempt and recipient transitions by ID.
// Attempts and recipients are keyed independently: neither lifecycle
// implies the other.
type Tracker struct {
	mu         sync.Mutex
	attempts   map[string]Attempt
	recipients map[string]Recipient
}

// NewTracker returns an empty ledger.
func NewTracker() *Tracker {
	return &Tracker{attempts: make(map[string]Attempt), recipients: make(map[string]Recipient)}
}

// RecordAttempt files one attempt; refiling the same ID with the same
// state is idempotent, any other rewrite is refused.
func (t *Tracker) RecordAttempt(attempt Attempt) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if attempt.ID == "" {
		return fmt.Errorf("%w: attempt has no identity", ErrAttemptTransition)
	}
	if stored, ok := t.attempts[attempt.ID]; ok && stored != attempt {
		return fmt.Errorf("%w: attempt %s is immutable", ErrAttemptTransition, attempt.ID)
	}
	t.attempts[attempt.ID] = attempt
	return nil
}

// RecordRecipient files one recipient revision; revisions must advance.
func (t *Tracker) RecordRecipient(recipient Recipient) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if recipient.AttemptID == "" {
		return fmt.Errorf("%w: recipient names no attempt", ErrRecipientTransition)
	}
	key := recipient.AttemptID + "\x00" + recipient.RecipientRef
	if stored, ok := t.recipients[key]; ok && recipient.Revision <= stored.Revision {
		return fmt.Errorf("%w: recipient revision must advance", ErrRecipientTransition)
	}
	t.recipients[key] = recipient
	return nil
}

// Attempt returns one retained attempt, including terminal ones: bounced
// and expired attempts never disappear.
func (t *Tracker) Attempt(id string) (Attempt, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	attempt, ok := t.attempts[id]
	if !ok {
		return Attempt{}, fmt.Errorf("%w: unknown attempt", ErrAttemptTransition)
	}
	return attempt, nil
}

// Recipient returns one retained recipient state.
func (t *Tracker) Recipient(attemptID, recipientRef string) (Recipient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	recipient, ok := t.recipients[attemptID+"\x00"+recipientRef]
	if !ok {
		return Recipient{}, fmt.Errorf("%w: unknown recipient", ErrRecipientTransition)
	}
	return recipient, nil
}

// AttemptIDs lists every retained attempt in order.
func (t *Tracker) AttemptIDs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	ids := make([]string, 0, len(t.attempts))
	for id := range t.attempts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
