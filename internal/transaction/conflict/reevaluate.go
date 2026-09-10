package conflict

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// PreflightVerdict is one execution-preflight outcome.
type PreflightVerdict string

// Execution-preflight verdicts.
const (
	PreflightClear              PreflightVerdict = "CLEAR"
	PreflightReapprovalRequired PreflightVerdict = "REAPPROVAL_REQUIRED"
	PreflightReplanRequired     PreflightVerdict = "REPLAN_REQUIRED"
	PreflightBlocked            PreflightVerdict = "BLOCKED"
)

// ReevaluateRequest asks for one execution-time conflict-set
// re-evaluation: the approval-time pinned intents against the current
// execution set under one versioned policy.
type ReevaluateRequest struct {
	PolicyVersion string
	Pinned        []WriteIntent
	Current       []WriteIntent
}

// OverlapExplanation names one scope collision.
type OverlapExplanation struct {
	IntentA     string
	IntentB     string
	ScopeDigest string
	Reason      string
}

// ExecutionPreflight is one re-evaluation outcome. Blocked paths commit
// zero business effects: re-evaluation only reads.
type ExecutionPreflight struct {
	PolicyVersion string
	Verdict       PreflightVerdict
	New           []string
	Missing       []string
	Changed       []string
	Overlaps      []OverlapExplanation
	Digest        string
}

// ReevaluatePreflight compares pinned and current footprints and
// effective intervals. Post-approval collisions block with an explanation
// per overlap; post-approval creations need reapproval; drift needs
// replan; identical sets clear.
func ReevaluatePreflight(req ReevaluateRequest) (ExecutionPreflight, error) {
	if strings.TrimSpace(req.PolicyVersion) == "" {
		return ExecutionPreflight{}, fmt.Errorf("conflict: ReevaluatePreflight: %w", ErrInvalidIntent)
	}
	for _, intent := range req.Pinned {
		if err := intent.Validate(); err != nil {
			return ExecutionPreflight{}, err
		}
	}
	for _, intent := range req.Current {
		if err := intent.Validate(); err != nil {
			return ExecutionPreflight{}, err
		}
	}
	pinned := make(map[string]WriteIntent, len(req.Pinned))
	for _, intent := range req.Pinned {
		pinned[intent.ID] = intent
	}
	current := make(map[string]WriteIntent, len(req.Current))
	for _, intent := range req.Current {
		current[intent.ID] = intent
	}
	outcome := ExecutionPreflight{PolicyVersion: req.PolicyVersion}
	for id := range current {
		if _, ok := pinned[id]; !ok {
			outcome.New = append(outcome.New, id)
		}
	}
	for id := range pinned {
		if _, ok := current[id]; !ok {
			outcome.Missing = append(outcome.Missing, id)
		}
	}
	for id, pinnedIntent := range pinned {
		currentIntent, ok := current[id]
		if !ok {
			continue
		}
		if intentDigest(pinnedIntent) != intentDigest(currentIntent) {
			outcome.Changed = append(outcome.Changed, id)
		}
	}
	sort.Strings(outcome.New)
	sort.Strings(outcome.Missing)
	sort.Strings(outcome.Changed)
	byScope := make(map[string][]string)
	ordered := make([]string, 0, len(current))
	for id := range current {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		seen := make(map[string]bool)
		for _, footprint := range current[id].Footprints {
			scope := footprint.ScopeDigest()
			if scope == "" || seen[scope] {
				continue
			}
			seen[scope] = true
			byScope[scope] = append(byScope[scope], id)
		}
	}
	for scope, ids := range byScope {
		if len(ids) < 2 {
			continue
		}
		sort.Strings(ids)
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				if _, ok := pinned[ids[i]]; ok {
					if _, ok := pinned[ids[j]]; ok {
						continue
					}
				}
				outcome.Overlaps = append(outcome.Overlaps, OverlapExplanation{
					IntentA:     ids[i],
					IntentB:     ids[j],
					ScopeDigest: scope,
					Reason:      fmt.Sprintf("intents %q and %q contend for scope %s", ids[i], ids[j], scope),
				})
			}
		}
	}
	sort.Slice(outcome.Overlaps, func(i, j int) bool {
		if outcome.Overlaps[i].IntentA != outcome.Overlaps[j].IntentA {
			return outcome.Overlaps[i].IntentA < outcome.Overlaps[j].IntentA
		}
		return outcome.Overlaps[i].IntentB < outcome.Overlaps[j].IntentB
	})
	switch {
	case len(outcome.Overlaps) != 0:
		outcome.Verdict = PreflightBlocked
	case len(outcome.New) != 0:
		outcome.Verdict = PreflightReapprovalRequired
	case len(outcome.Missing) != 0 || len(outcome.Changed) != 0:
		outcome.Verdict = PreflightReplanRequired
	default:
		outcome.Verdict = PreflightClear
	}
	outcome.Digest = preflightDigest(req.PolicyVersion, outcome)
	return outcome, nil
}

// preflightDigest binds one deterministic identity over the verdict.
func preflightDigest(policy string, outcome ExecutionPreflight) string {
	parts := []string{"preflight", policy, string(outcome.Verdict),
		"new:" + strings.Join(outcome.New, ","),
		"missing:" + strings.Join(outcome.Missing, ","),
		"changed:" + strings.Join(outcome.Changed, ",")}
	for _, overlap := range outcome.Overlaps {
		parts = append(parts, "overlap:"+overlap.IntentA+"\x01"+overlap.IntentB+"\x01"+overlap.ScopeDigest)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
