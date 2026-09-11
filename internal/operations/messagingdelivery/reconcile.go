package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Delivery failure kinds: the closed reconciliation vocabulary.
const (
	FailureOutage          = "outage"
	FailureBounce          = "bounce"
	FailureInvalidEndpoint = "invalid-endpoint"
)

// Reconciliation outcomes: retry, fallback or terminally visible.
const (
	ReconcileRetry         = "RETRY"
	ReconcileFallbackInbox = "FALLBACK_SECURE_INBOX"
	ReconcileFallbackTask  = "FALLBACK_HUMAN_TASK"
)

// RetryPolicy bounds reconciliation: attempts never run forever, and
// permanent failures skip straight to fallback.
type RetryPolicy struct {
	MaxAttempts   int
	BackoffTicks  int64
	FallbackAfter int
}

// Fallback is the secure alternative: same recipient and classification,
// never broadened.
type Fallback struct {
	Kind           string
	RecipientRef   string
	Classification string
	Purpose        string
	TaskAssignee   string
	Reason         string
}

// ObligationRecord keeps the unsatisfied communication obligation visible.
type ObligationRecord struct {
	IntentID       string
	RecipientRef   string
	Classification string
	Satisfied      bool
	Visible        bool
	Attempts       int
	Digest         string
}

func obligationDigest(intentID, recipient, classification string, attempts int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{"delivery-obligation", intentID, recipient, classification, fmt.Sprint(attempts)}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Reconciliation is the terminal reconciliation of one failed delivery.
type Reconciliation struct {
	AttemptID  string
	Outcome    string
	NextTick   int64
	Fallback   Fallback
	Obligation ObligationRecord
}

// Reconcile folds one failure into the bounded policy. Outages and
// bounces retry within bound; invalid endpoints and exhausted attempts
// fall back; nothing silently satisfies and nothing retries forever.
func Reconcile(intentID, recipientRef, classification, purpose string, failureKind string, attempts int, nowTick int64, policy RetryPolicy) (Reconciliation, error) {
	if strings.TrimSpace(intentID) == "" || strings.TrimSpace(recipientRef) == "" {
		return Reconciliation{}, fmt.Errorf("delivery: reconciliation needs an intent and a recipient")
	}
	if strings.TrimSpace(classification) == "" || strings.TrimSpace(purpose) == "" {
		return Reconciliation{}, fmt.Errorf("delivery: reconciliation needs a classification and a purpose")
	}
	switch failureKind {
	case FailureOutage, FailureBounce, FailureInvalidEndpoint:
	default:
		return Reconciliation{}, fmt.Errorf("delivery: failure kind %q is not reconcilable", failureKind)
	}
	if policy.MaxAttempts <= 0 || policy.FallbackAfter <= 0 || policy.FallbackAfter > policy.MaxAttempts {
		return Reconciliation{}, fmt.Errorf("delivery: retry policy is unbounded")
	}
	if attempts < 0 {
		return Reconciliation{}, fmt.Errorf("delivery: attempt count is negative")
	}
	reconciliation := Reconciliation{AttemptID: "attempt:" + intentID + ":" + fmt.Sprint(attempts+1)}
	reconciliation.Obligation = ObligationRecord{
		IntentID: intentID, RecipientRef: recipientRef, Classification: classification,
		Satisfied: false, Visible: true, Attempts: attempts + 1,
	}
	reconciliation.Obligation.Digest = obligationDigest(intentID, recipientRef, classification, attempts+1)
	needsFallback := failureKind == FailureInvalidEndpoint || attempts+1 >= policy.FallbackAfter
	switch {
	case !needsFallback:
		reconciliation.Outcome = ReconcileRetry
		reconciliation.NextTick = nowTick + policy.BackoffTicks
	case classification == "RESTRICTED" || failureKind == FailureInvalidEndpoint:
		// Restricted material and dead endpoints fall back to a human
		// task: no alternate channel is assumed safe.
		reconciliation.Outcome = ReconcileFallbackTask
		reconciliation.Fallback = Fallback{Kind: ReconcileFallbackTask, RecipientRef: recipientRef, Classification: classification, Purpose: purpose, TaskAssignee: "comms-ops", Reason: failureKind}
	default:
		reconciliation.Outcome = ReconcileFallbackInbox
		reconciliation.Fallback = Fallback{Kind: ReconcileFallbackInbox, RecipientRef: recipientRef, Classification: classification, Purpose: purpose, Reason: failureKind}
	}
	return reconciliation, nil
}

// Reconciler guards obligations for concurrent reconciliation.
type Reconciler struct {
	mu          sync.Mutex
	obligations map[string]ObligationRecord
}

// NewReconciler starts an empty reconciler.
func NewReconciler() *Reconciler {
	return &Reconciler{obligations: make(map[string]ObligationRecord)}
}

// Record stores one reconciliation's obligation. Obligations stay
// visible until satisfied elsewhere: recording never hides one.
func (reconciler *Reconciler) Record(reconciliation Reconciliation) {
	if reconciler == nil {
		return
	}
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	reconciler.obligations[reconciliation.Obligation.IntentID] = reconciliation.Obligation
}

// Visible lists every unsatisfied obligation in order.
func (reconciler *Reconciler) Visible() []ObligationRecord {
	if reconciler == nil {
		return nil
	}
	reconciler.mu.Lock()
	defer reconciler.mu.Unlock()
	var out []ObligationRecord
	for _, obligation := range reconciler.obligations {
		if !obligation.Satisfied && obligation.Visible {
			out = append(out, obligation)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IntentID < out[j].IntentID })
	return out
}
