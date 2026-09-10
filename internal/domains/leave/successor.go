package leave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// Successor kinds: the closed related-intent vocabulary.
const (
	SuccessorExtend  = "ExtendLeave"
	SuccessorShorten = "ShortenLeave"
	SuccessorCancel  = "CancelLeave"
)

// SuccessorIntent is one related intent referencing the LeaveRecord and
// prior intent/proposal. The original request and revision are never
// mutated: the successor carries their digests and appends its own facts.
type SuccessorIntent struct {
	Kind                string
	LeaveRecordID       string
	PriorIntentDigest   string
	PriorProposalDigest string
	NewStart            int
	NewEnd              int
	ReplanDigest        string
	ReplanPrior         string
	ConflictRule        string
	ConflictWinner      string
	ConsumedHours       int
	BalanceSettlement   string
	Lineage             []string
	Digest              string
}

func successorDigest(intent SuccessorIntent) string {
	parts := []string{"leave-successor", intent.Kind, intent.LeaveRecordID, intent.PriorIntentDigest, intent.PriorProposalDigest, fmt.Sprint(intent.NewStart, intent.NewEnd, intent.ConsumedHours), intent.ReplanDigest, intent.ReplanPrior, intent.ConflictRule, intent.ConflictWinner, intent.BalanceSettlement}
	parts = append(parts, intent.Lineage...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SuccessorRegistry guards related intents: overlapping child changes
// never both commit.
type SuccessorRegistry struct {
	mu         sync.Mutex
	successors map[string]SuccessorIntent
}

// NewSuccessorRegistry starts an empty registry.
func NewSuccessorRegistry() *SuccessorRegistry {
	return &SuccessorRegistry{successors: make(map[string]SuccessorIntent)}
}

func successorOverlaps(aStart, aEnd, bStart, bEnd int) bool {
	return aStart <= bEnd && bStart <= aEnd
}

// Declare appends one successor intent. Extensions and shortenings must
// bind a full governed replan of the prior plan; cancellations must
// settle consumed balance instead of rewriting it; overlapping children
// of one prior never both commit.
func (registry *SuccessorRegistry) Declare(intent SuccessorIntent, priorPlanDigest string) (SuccessorIntent, error) {
	if registry == nil {
		return SuccessorIntent{}, fmt.Errorf("leave: nil successor registry")
	}
	switch intent.Kind {
	case SuccessorExtend, SuccessorShorten, SuccessorCancel:
	default:
		return SuccessorIntent{}, fmt.Errorf("leave: successor kind %q is not a related intent", intent.Kind)
	}
	if strings.TrimSpace(intent.LeaveRecordID) == "" || strings.TrimSpace(intent.PriorIntentDigest) == "" || strings.TrimSpace(intent.PriorProposalDigest) == "" {
		return SuccessorIntent{}, fmt.Errorf("leave: successor references its record and prior intent/proposal")
	}
	if intent.NewEnd < intent.NewStart {
		return SuccessorIntent{}, fmt.Errorf("leave: successor interval is inverted")
	}
	if intent.Kind == SuccessorCancel {
		if intent.ConsumedHours > 0 && strings.TrimSpace(intent.BalanceSettlement) == "" {
			return SuccessorIntent{}, fmt.Errorf("leave: cancellation settles consumed balance instead of rewriting it")
		}
	} else {
		if strings.TrimSpace(intent.ReplanDigest) == "" || intent.ReplanPrior != priorPlanDigest {
			return SuccessorIntent{}, fmt.Errorf("leave: extension bypasses full governed replanning")
		}
		if strings.TrimSpace(intent.ConflictRule) == "" || strings.TrimSpace(intent.ConflictWinner) == "" {
			return SuccessorIntent{}, fmt.Errorf("leave: successor resolves conflicts deterministically")
		}
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, prior := range registry.successors {
		if prior.LeaveRecordID != intent.LeaveRecordID || prior.Kind == SuccessorCancel {
			continue
		}
		if intent.Kind != SuccessorCancel && successorOverlaps(prior.NewStart, prior.NewEnd, intent.NewStart, intent.NewEnd) {
			return SuccessorIntent{}, fmt.Errorf("leave: overlapping child changes never both commit")
		}
	}
	intent.Lineage = append([]string{intent.PriorIntentDigest, intent.PriorProposalDigest}, intent.Lineage...)
	intent.Digest = successorDigest(intent)
	key := intent.LeaveRecordID + "\x00" + intent.Kind + "\x00" + fmt.Sprint(intent.NewStart, intent.NewEnd)
	if _, dup := registry.successors[key]; dup {
		return SuccessorIntent{}, fmt.Errorf("leave: identical successor already declared")
	}
	registry.successors[key] = intent
	return intent, nil
}

// Verify recomputes the successor seal.
func (intent SuccessorIntent) Verify() error {
	if intent.Digest == "" || successorDigest(intent) != intent.Digest {
		return fmt.Errorf("leave: successor seal is broken")
	}
	return nil
}
