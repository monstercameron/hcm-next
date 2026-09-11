package runtime

// Poison-node quarantine (WF-RUN-007): an exhausted node never retries
// forever, is never dropped and never reports workflow success. It lands
// in QuarantinedWork with its attempts, error, ambiguity, idempotency,
// owner, SLA and next action retained, and routes the workflow to
// BLOCKED, REPAIR_REQUIRED or QUARANTINED. Dead-letter storage stays an
// operations projection: the route is a workflow state, never a terminal
// business outcome.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Quarantine routes: the only states a poisoned workflow may report.
const (
	WorkflowBlocked        = "BLOCKED"
	WorkflowRepairRequired = "REPAIR_REQUIRED"
	WorkflowQuarantined    = "QUARANTINED"
)

// QuarantinedWork retains everything the poisoned node proved.
type QuarantinedWork struct {
	NodeID         string
	WorkflowID     string
	Attempts       int
	LastError      string
	Ambiguous      bool
	IdempotencyKey string
	Owner          string
	SLA            time.Duration
	NextAction     string
	RepairRoute    string
	Route          string
	Digest         string
}

// Verify recomputes the record seal.
func (q QuarantinedWork) Verify() error {
	if quarantineDigest(q) != q.Digest {
		return fmt.Errorf("runtime: quarantine %s: seal broken", q.NodeID)
	}
	return nil
}

func quarantineDigest(q QuarantinedWork) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"quarantine", q.NodeID, q.WorkflowID, fmt.Sprintf("%d", q.Attempts), q.LastError,
		fmt.Sprintf("%v", q.Ambiguous), q.IdempotencyKey, q.Owner, q.SLA.String(),
		q.NextAction, q.RepairRoute, q.Route,
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// QuarantineSpec admits one exhausted node.
type QuarantineSpec struct {
	NodeID         string
	WorkflowID     string
	Attempts       int
	LastError      string
	Ambiguous      bool
	IdempotencyKey string
	Owner          string
	SLA            time.Duration
	NextAction     string
	Terminal       RetryRoute
}

// Admit routes one terminal retry outcome into quarantine. A RETRY route
// is never quarantinable: only exhaustion lands here.
func Admit(spec QuarantineSpec) (QuarantinedWork, error) {
	if spec.Terminal.Decision != RouteTerminal {
		return QuarantinedWork{}, fmt.Errorf("runtime: Admit %s: retryable work is not poison", spec.NodeID)
	}
	if spec.NodeID == "" || spec.WorkflowID == "" || spec.IdempotencyKey == "" || spec.Attempts <= 0 {
		return QuarantinedWork{}, fmt.Errorf("runtime: Admit: node, workflow, idempotency and attempts are required")
	}
	work := QuarantinedWork{
		NodeID: spec.NodeID, WorkflowID: spec.WorkflowID, Attempts: spec.Attempts,
		LastError: spec.LastError, Ambiguous: spec.Ambiguous, IdempotencyKey: spec.IdempotencyKey,
		Owner: spec.Owner, SLA: spec.SLA, RepairRoute: spec.Terminal.RepairRoute,
	}
	switch {
	case spec.Ambiguous:
		// Ambiguity cannot resolve to success or to a blind retry: the
		// node waits under quarantine with its next action stated.
		if spec.Owner == "" || spec.NextAction == "" {
			return QuarantinedWork{}, fmt.Errorf("runtime: Admit %s: ambiguous work needs owner and next action", spec.NodeID)
		}
		work.Route = WorkflowQuarantined
		work.NextAction = spec.NextAction
	case spec.Terminal.Reason == ReasonBudgetExhausted:
		// An exhausted budget carries its repair route: the workflow
		// waits for repair, never for another silent attempt.
		work.Route = WorkflowRepairRequired
		work.NextAction = "repair:" + spec.Terminal.RepairRoute
	default:
		// Permanent and do-not-retry failures need an operator decision.
		if spec.Owner == "" {
			return QuarantinedWork{}, fmt.Errorf("runtime: Admit %s: blocked work needs an owner", spec.NodeID)
		}
		work.Route = WorkflowBlocked
		work.NextAction = "operator-decision"
	}
	work.Digest = quarantineDigest(work)
	return work, nil
}

// QuarantineLedger retains quarantined work by idempotency key:
// re-admission returns the stored record instead of duplicating it, and
// nothing is ever dropped.
type QuarantineLedger struct {
	mu      sync.Mutex
	records map[string]QuarantinedWork
}

// NewQuarantineLedger returns an empty ledger.
func NewQuarantineLedger() *QuarantineLedger {
	return &QuarantineLedger{records: make(map[string]QuarantinedWork)}
}

// File admits one record idempotently.
func (l *QuarantineLedger) File(work QuarantinedWork) (QuarantinedWork, error) {
	if err := work.Verify(); err != nil {
		return QuarantinedWork{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if stored, ok := l.records[work.IdempotencyKey]; ok {
		return stored, nil
	}
	l.records[work.IdempotencyKey] = work
	return work, nil
}

// Get returns one retained record. Missing keys are refused, never
// reported as success.
func (l *QuarantineLedger) Get(idempotencyKey string) (QuarantinedWork, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	work, ok := l.records[idempotencyKey]
	if !ok {
		return QuarantinedWork{}, fmt.Errorf("runtime: Get %s: no quarantined work", idempotencyKey)
	}
	return work, nil
}

// Keys lists every retained idempotency key in order.
func (l *QuarantineLedger) Keys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	keys := make([]string, 0, len(l.records))
	for key := range l.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
