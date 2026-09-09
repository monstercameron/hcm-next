package shadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

const TerminalNotExecuted = "SHADOW_NOT_EXECUTED"

type TraceEntry struct {
	Sequence     int               `json:"sequence"`
	NodeID       string            `json:"node_id"`
	Attempt      int               `json:"attempt"`
	StepType     workflow.StepType `json:"step_type"`
	Outcome      workflow.Outcome  `json:"outcome,omitempty"`
	RouteKey     string            `json:"route_key,omitempty"`
	OutputDigest string            `json:"output_digest,omitempty"`
	StateDigest  string            `json:"state_digest"`
	ObservedAt   time.Time         `json:"observed_at"`
}

type Trace struct {
	TenantID   string         `json:"tenant_id"`
	InstanceID string         `json:"instance_id"`
	WorkflowID string         `json:"workflow_id"`
	PlanDigest string         `json:"plan_digest"`
	Entries    []TraceEntry   `json:"entries"`
	Terminal   TerminalRecord `json:"terminal,omitzero"`
	digest     string
}

func (t Trace) Digest() string { return t.digest }
func (t Trace) Verify() error {
	if computeTraceDigest(t) != t.digest {
		return fmt.Errorf("shadow: trace digest mismatch")
	}
	return nil
}
func (t Trace) JSON() ([]byte, error) {
	type rendered struct {
		Trace
		Digest string `json:"digest"`
	}
	b, err := json.MarshalIndent(rendered{Trace: t, Digest: t.digest}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

type Divergence struct {
	Sequence int    `json:"sequence"`
	NodeID   string `json:"node_id"`
	Field    string `json:"field"`
	Shadow   string `json:"shadow"`
	Live     string `json:"live"`
}

type DivergenceReport struct {
	Equal       bool         `json:"equal"`
	Divergences []Divergence `json:"divergences,omitempty"`
}

// Compare returns a deterministic field-level report between shadow evidence
// and a live instance's recorded trace.
func Compare(shadow, live Trace) DivergenceReport {
	report := DivergenceReport{Equal: true}
	if shadow.WorkflowID != live.WorkflowID {
		report.add(0, "", "workflow_id", shadow.WorkflowID, live.WorkflowID)
	}
	if shadow.PlanDigest != live.PlanDigest {
		report.add(0, "", "plan_digest", shadow.PlanDigest, live.PlanDigest)
	}
	limit := len(shadow.Entries)
	if len(live.Entries) > limit {
		limit = len(live.Entries)
	}
	for i := 0; i < limit; i++ {
		var left, right TraceEntry
		if i < len(shadow.Entries) {
			left = shadow.Entries[i]
		}
		if i < len(live.Entries) {
			right = live.Entries[i]
		}
		seq := i + 1
		node := left.NodeID
		if node == "" {
			node = right.NodeID
		}
		checks := [][3]string{{"node_id", left.NodeID, right.NodeID}, {"route_key", left.RouteKey, right.RouteKey}, {"output_digest", left.OutputDigest, right.OutputDigest}, {"state_digest", left.StateDigest, right.StateDigest}}
		for _, check := range checks {
			if check[1] != check[2] {
				report.add(seq, node, check[0], check[1], check[2])
			}
		}
	}
	if shadow.Terminal.BusinessCode != live.Terminal.BusinessCode {
		report.add(0, "", "terminal_business_code", shadow.Terminal.BusinessCode, live.Terminal.BusinessCode)
	}
	return report
}

func CompareTraces(shadow, live Trace) DivergenceReport { return Compare(shadow, live) }
func (r *DivergenceReport) add(sequence int, node, field, shadow, live string) {
	r.Equal = false
	r.Divergences = append(r.Divergences, Divergence{Sequence: sequence, NodeID: node, Field: field, Shadow: shadow, Live: live})
}

func marshalCanonical(value any) ([]byte, error) { return json.Marshal(value) }
func digestBytes(profile string, b []byte) string {
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
func computeTraceDigest(t Trace) string {
	t.digest = ""
	b, _ := marshalCanonical(t)
	return digestBytes("hcmnext.workflow.shadow.Trace/v1", b)
}
func digestState(s State) string {
	type pair struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	pairs := make([]pair, 0, len(s))
	for _, key := range stateKeys(s) {
		pairs = append(pairs, pair{Key: key, Value: hex.EncodeToString(s[key])})
	}
	b, _ := json.Marshal(pairs)
	return digestBytes("hcmnext.workflow.shadow.State/v1", b)
}
func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
