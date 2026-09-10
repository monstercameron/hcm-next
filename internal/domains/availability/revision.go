package availability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Revision is one appended availability successor. History is append-only:
// Employment and authored schedule history are never overwritten because
// revisions only ever add.
type Revision struct {
	RevisionID      string
	WorkerID        string
	PriorRevision   string
	State           State
	Restored        bool
	IntentID        string
	ProposalDigest  string
	Reason          string
	LeaveEventID    string
	EffectiveStart  int
	EffectiveEnd    int
	Reconciliations []string
	Digest          string
}

func revisionDigest(revision Revision) string {
	parts := []string{"availability-revision", revision.WorkerID, revision.PriorRevision, string(revision.State), fmt.Sprint(revision.Restored), revision.IntentID, revision.ProposalDigest, revision.Reason, revision.LeaveEventID, fmt.Sprint(revision.EffectiveStart, revision.EffectiveEnd)}
	reconciliations := append([]string(nil), revision.Reconciliations...)
	sort.Strings(reconciliations)
	parts = append(parts, reconciliations...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func overlaps(aStart, aEnd, bStart, bEnd int) bool {
	return aStart <= bEnd && bStart <= aEnd
}

// RevisionLog is the owning transaction: one mutex-guarded append log per
// worker with two-phase prepare/commit for crash recovery.
type RevisionLog struct {
	mu        sync.Mutex
	heads     map[string]string
	revisions map[string]Revision
	byEvent   map[string]string
	prepared  map[string]Revision
}

// NewRevisionLog starts an empty log.
func NewRevisionLog() *RevisionLog {
	return &RevisionLog{heads: make(map[string]string), revisions: make(map[string]Revision), byEvent: make(map[string]string), prepared: make(map[string]Revision)}
}

// Prepare stages one successor revision. Stale heads, inverted or
// overlapping incompatible intervals, off-vocabulary states and
// availability without its owning Leave event refuse. A duplicate
// leave/return effect returns the existing revision instead of appending.
func (log *RevisionLog) Prepare(revision Revision) (string, error) {
	if log == nil {
		return "", fmt.Errorf("availability: nil revision log")
	}
	if strings.TrimSpace(revision.WorkerID) == "" || strings.TrimSpace(revision.IntentID) == "" || strings.TrimSpace(revision.ProposalDigest) == "" {
		return "", fmt.Errorf("availability: revision needs a worker, intent and proposal")
	}
	if strings.TrimSpace(revision.Reason) == "" {
		return "", fmt.Errorf("availability: revision needs a reason")
	}
	if strings.TrimSpace(revision.LeaveEventID) == "" {
		return "", fmt.Errorf("availability: availability never exists without its owning leave event")
	}
	if revision.EffectiveEnd < revision.EffectiveStart {
		return "", fmt.Errorf("availability: effective interval is inverted")
	}
	switch revision.State {
	case Unavailable, Restricted, Available:
	default:
		return "", fmt.Errorf("availability: state %q is not a successor", revision.State)
	}
	if revision.State == Available && !revision.Restored {
		return "", fmt.Errorf("availability: returning to available restores explicitly")
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if existing, dup := log.byEvent[revision.LeaveEventID]; dup {
		return existing, nil
	}
	if head, ok := log.heads[revision.WorkerID]; ok {
		if revision.PriorRevision != head {
			return "", fmt.Errorf("availability: stale schedule head")
		}
	} else if strings.TrimSpace(revision.PriorRevision) != "" {
		return "", fmt.Errorf("availability: first revision names no prior")
	}
	for _, prior := range log.revisions {
		if prior.WorkerID != revision.WorkerID {
			continue
		}
		if overlaps(prior.EffectiveStart, prior.EffectiveEnd, revision.EffectiveStart, revision.EffectiveEnd) && prior.State != revision.State {
			return "", fmt.Errorf("availability: overlapping incompatible interval")
		}
	}
	staged := revision
	// Zero-padded sequence: History sorts by RevisionID, so the IDs must
	// sort in append order past the tenth revision.
	staged.RevisionID = fmt.Sprintf("arev:%s:%06d", revision.WorkerID, len(log.revisions)+len(log.prepared)+1)
	staged.Reconciliations = []string{"schedule:reconcile", "wfm:reconcile"}
	staged.Digest = revisionDigest(staged)
	token := "prepare:" + staged.RevisionID
	log.prepared[token] = staged
	return token, nil
}

// Commit appends one prepared revision exactly once: the schedule/WFM
// reconciliation effects queue with it, and the head advances.
func (log *RevisionLog) Commit(token string, inject func(string) error) (Revision, error) {
	if log == nil {
		return Revision{}, fmt.Errorf("availability: nil revision log")
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	staged, ok := log.prepared[token]
	if !ok {
		return Revision{}, fmt.Errorf("availability: unknown prepare token")
	}
	if inject != nil {
		if err := inject("append"); err != nil {
			delete(log.prepared, token)
			return Revision{}, fmt.Errorf("availability: rolled back before append: %v", err)
		}
	}
	if _, dup := log.byEvent[staged.LeaveEventID]; dup {
		delete(log.prepared, token)
		return Revision{}, fmt.Errorf("availability: duplicate leave effect")
	}
	log.revisions[staged.RevisionID] = staged
	log.heads[staged.WorkerID] = staged.RevisionID
	log.byEvent[staged.LeaveEventID] = staged.RevisionID
	delete(log.prepared, token)
	return staged, nil
}

// Recover commits every unfinished prepare exactly once after a crash:
// availability never appears without its owning leave event, and never
// twice.
func (log *RevisionLog) Recover() ([]Revision, error) {
	if log == nil {
		return nil, fmt.Errorf("availability: nil revision log")
	}
	log.mu.Lock()
	tokens := make([]string, 0, len(log.prepared))
	for token := range log.prepared {
		tokens = append(tokens, token)
	}
	log.mu.Unlock()
	sort.Strings(tokens)
	var committed []Revision
	for _, token := range tokens {
		revision, err := log.Commit(token, nil)
		if err != nil {
			return committed, err
		}
		committed = append(committed, revision)
	}
	return committed, nil
}

// History lists one worker's revisions in append order.
func (log *RevisionLog) History(workerID string) []Revision {
	if log == nil {
		return nil
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	var out []Revision
	for _, revision := range log.revisions {
		if revision.WorkerID == workerID {
			out = append(out, revision)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RevisionID < out[j].RevisionID })
	return out
}

// Verify recomputes the revision seal.
func (revision Revision) Verify() error {
	if revision.Digest == "" || revisionDigest(revision) != revision.Digest {
		return fmt.Errorf("availability: revision seal is broken")
	}
	return nil
}
