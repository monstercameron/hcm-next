package replay

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func sampleTrace() Trace {
	tr := Trace{
		TenantID: "tenant", InstanceID: "instance", WorkflowID: "wf", Version: 1,
		PlanDigest: "sha256:plan", HistoricalIntentID: "intent:1",
		Entries: []TraceEntry{{
			Sequence: 1, NodeID: "a", Attempt: 1, StepType: workflow.StepTransform,
			Source: SourceRecordedOutput, RouteKey: "SUCCEEDED", OutputDigest: "sha256:a",
			CompletedState: "SUCCEEDED", Frontier: []string{"b"},
			TransitionDigest: "sha256:t", ObservedAt: time.Unix(1, 0).UTC(),
		}},
		Status: StatusOpenFrontier, Frontier: []string{"b"},
	}
	tr.digest = computeTraceDigest(tr)
	return tr
}

// TestTrace_DigestIsProfilePrefixedAndContentBound checks the two properties a
// digest has to have to be worth minting: it covers the content, and it cannot
// collide with another artifact's digest of the same bytes.
func TestTrace_DigestIsProfilePrefixedAndContentBound(t *testing.T) {
	tr := sampleTrace()
	if err := tr.Verify(); err != nil {
		t.Fatalf("a freshly minted trace does not verify: %v", err)
	}
	if len(tr.Digest()) != 64 {
		t.Fatalf("digest %q is not a hex sha256", tr.Digest())
	}

	// Every field that is part of the trace's identity moves the digest.
	for name, edit := range map[string]func(*Trace){
		"tenant":       func(x *Trace) { x.TenantID = "other" },
		"instance":     func(x *Trace) { x.InstanceID = "other" },
		"intent":       func(x *Trace) { x.HistoricalIntentID = "other" },
		"plan":         func(x *Trace) { x.PlanDigest = "sha256:other" },
		"status":       func(x *Trace) { x.Status = StatusComplete },
		"terminal":     func(x *Trace) { x.TerminalCode = "T" },
		"frontier":     func(x *Trace) { x.Frontier = []string{"c"} },
		"entry route":  func(x *Trace) { x.Entries[0].RouteKey = "FAILED" },
		"entry digest": func(x *Trace) { x.Entries[0].OutputDigest = "sha256:other" },
		"entry instant": func(x *Trace) {
			x.Entries[0].ObservedAt = time.Unix(2, 0).UTC()
		},
	} {
		t.Run(name, func(t *testing.T) {
			edited := sampleTrace()
			edited.Entries = append([]TraceEntry(nil), edited.Entries...)
			edit(&edited)
			if computeTraceDigest(edited) == tr.Digest() {
				t.Fatalf("editing the %s did not move the digest", name)
			}
			if err := edited.Verify(); err == nil {
				t.Fatalf("an edited trace verified against the original digest")
			}
		})
	}

	// The profile prefix is what keeps a trace's bytes from digesting as some
	// other artifact's.
	if !strings.HasPrefix(traceDigestProfile, "hcmnext.workflow.replay.") {
		t.Fatalf("profile %q is not namespaced to this package", traceDigestProfile)
	}
}

// TestTrace_JSONIsDeterministicAndCarriesTheDigest checks the rendering the
// golden is taken over.
func TestTrace_JSONIsDeterministicAndCarriesTheDigest(t *testing.T) {
	tr := sampleTrace()
	first, err := tr.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	second, err := tr.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("two renderings differ")
	}
	if first[len(first)-1] != '\n' {
		t.Fatalf("rendering does not end in a newline")
	}

	var rendered struct {
		Digest string `json:"digest"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(first, &rendered); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rendered.Digest != tr.Digest() {
		t.Fatalf("rendered digest %q, want %q", rendered.Digest, tr.Digest())
	}
	if rendered.Status != string(StatusOpenFrontier) {
		t.Fatalf("rendered status %q", rendered.Status)
	}
}

// TestInputSource_IsClosed pins the three declared sources.
func TestInputSource_IsClosed(t *testing.T) {
	seen := map[InputSource]bool{}
	for _, s := range []InputSource{SourceRecordedOutput, SourceRecordedSignal, SourceRecordedTimer} {
		if s == "" || seen[s] {
			t.Fatalf("input source %q is empty or duplicated", s)
		}
		if !strings.HasPrefix(string(s), "RECORDED_") {
			t.Fatalf("input source %q does not say it came from the record", s)
		}
		seen[s] = true
	}
}
