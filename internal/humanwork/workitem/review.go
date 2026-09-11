package workitem

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Review verdicts: the closed typed-finding vocabulary. Free-form
// eligibility or diagnosis never leaves the compartment.
const (
	ReviewSufficient   = "SUFFICIENT"
	ReviewInsufficient = "INSUFFICIENT"
	ReviewMoreInfo     = "MORE_INFORMATION_REQUIRED"
	ReviewUnknown      = "UNKNOWN"
)

// Safe reason codes: the only reasons a finding may carry. Sensitive
// notes remain compartmented artifacts; the workflow sees only these.
var safeReasons = map[string]bool{
	"evidence-clear":         true,
	"evidence-contradictory": true,
	"evidence-partial":       true,
	"evidence-unreadable":    true,
}

// ReviewTask is one restricted evidence-review task.
type ReviewTask struct {
	TaskID              string
	PolicyID            string
	RequirementID       string
	RequirementVersion  string
	ArtifactID          string
	ArtifactVersion     string
	ArtifactCurrent     string
	ArtifactQuarantined bool
	Scope               []string
	ReviewerRole        string
	ExpiresTick         int64
}

// Finding is the minimum typed result the workflow may see. It binds
// requirement and artifact versions, reviewed scope, safe reason, expiry
// and evidence receipt — and nothing else. It is workflow input, never
// legal eligibility by itself: the type carries no eligibility verdict.
type Finding struct {
	TaskID             string
	Verdict            string
	RequirementID      string
	RequirementVersion string
	ArtifactID         string
	ArtifactVersion    string
	Scope              []string
	Reason             string
	ExpiresTick        int64
	EvidenceReceipt    string
	Digest             string
}

func findingDigest(finding Finding) string {
	scope := append([]string(nil), finding.Scope...)
	sort.Strings(scope)
	parts := []string{"workitem-finding", finding.TaskID, finding.Verdict, finding.RequirementID, finding.RequirementVersion, finding.ArtifactID, finding.ArtifactVersion, strings.Join(scope, ","), finding.Reason, fmt.Sprint(finding.ExpiresTick), finding.EvidenceReceipt}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Reviewer completes one restricted review behind the compartment policy.
// The reviewer needs an explicit grant; the verdict stays inside the
// typed vocabulary with a safe reason; stale or quarantined artifacts
// refuse; duplicate completions return the identical finding instead of
// diverging.
type Reviewer struct {
	mu        sync.Mutex
	policies  map[string]AccessPolicy
	completed map[string]Finding
}

// NewReviewer starts an empty reviewer.
func NewReviewer() *Reviewer {
	return &Reviewer{policies: make(map[string]AccessPolicy), completed: make(map[string]Finding)}
}

// RegisterPolicy publishes one compartment policy.
func (reviewer *Reviewer) RegisterPolicy(policy AccessPolicy) error {
	if reviewer == nil {
		return fmt.Errorf("workitem: nil reviewer")
	}
	if strings.TrimSpace(policy.PolicyID) == "" {
		return fmt.Errorf("workitem: policy id is required")
	}
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	reviewer.policies[policy.PolicyID] = policy
	return nil
}

// Complete finishes one review task. Sensitive notes never leave the
// compartment: only the typed finding returns.
func (reviewer *Reviewer) Complete(task ReviewTask, verdict, reason, evidenceReceipt string, nowTick int64) (Finding, error) {
	if reviewer == nil {
		return Finding{}, fmt.Errorf("workitem: nil reviewer")
	}
	reviewer.mu.Lock()
	defer reviewer.mu.Unlock()
	if prior, done := reviewer.completed[task.TaskID]; done {
		if prior.Verdict != verdict || prior.Reason != reason {
			return Finding{}, fmt.Errorf("workitem: duplicate completion of %s diverges", task.TaskID)
		}
		return prior, nil
	}
	policy, ok := reviewer.policies[task.PolicyID]
	if !ok {
		return Finding{}, fmt.Errorf("workitem: unknown policy %s", task.PolicyID)
	}
	decision, err := Authorize(policy, task.ReviewerRole, "view-artifact:medical-document", nowTick)
	if err != nil || !decision.Permitted {
		return Finding{}, fmt.Errorf("workitem: reviewer lacks an evidence grant")
	}
	if nowTick > task.ExpiresTick {
		return Finding{}, fmt.Errorf("workitem: review task %s expired", task.TaskID)
	}
	switch verdict {
	case ReviewSufficient, ReviewInsufficient, ReviewMoreInfo, ReviewUnknown:
	default:
		return Finding{}, fmt.Errorf("workitem: verdict %q is not a typed finding", verdict)
	}
	if !safeReasons[reason] {
		return Finding{}, fmt.Errorf("workitem: reason %q exposes free-form detail", reason)
	}
	if task.ArtifactVersion != task.ArtifactCurrent || !task.ArtifactQuarantined {
		return Finding{}, fmt.Errorf("workitem: review relies on a stale or unquarantined artifact")
	}
	if strings.TrimSpace(task.RequirementID) == "" || strings.TrimSpace(task.RequirementVersion) == "" {
		return Finding{}, fmt.Errorf("workitem: review binds a requirement and version")
	}
	if strings.TrimSpace(evidenceReceipt) == "" {
		return Finding{}, fmt.Errorf("workitem: review needs an evidence receipt")
	}
	finding := Finding{
		TaskID: task.TaskID, Verdict: verdict,
		RequirementID: task.RequirementID, RequirementVersion: task.RequirementVersion,
		ArtifactID: task.ArtifactID, ArtifactVersion: task.ArtifactVersion,
		Scope: append([]string(nil), task.Scope...), Reason: reason,
		ExpiresTick: task.ExpiresTick, EvidenceReceipt: evidenceReceipt,
	}
	finding.Digest = findingDigest(finding)
	reviewer.completed[task.TaskID] = finding
	return finding, nil
}

// Verify recomputes the finding seal.
func (finding Finding) Verify() error {
	if finding.Digest == "" || findingDigest(finding) != finding.Digest {
		return fmt.Errorf("workitem: finding seal is broken")
	}
	return nil
}
