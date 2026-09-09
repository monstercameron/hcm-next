package snapshot_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	promosnapshot "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/snapshot"
	enginesnapshot "github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
)

// update rewrites the golden verdict table instead of comparing against it.
var update = flag.Bool("update", false, "rewrite the promotion snapshot golden files")

// TestTodo_PROMO_001_Golden pins the whole artifact: the per-input verdict
// table, the disclosed canonical texts and the two digests.
//
// It is a text golden rather than a set of assertions because the point of the
// snapshot is that it is one exact, reproducible artifact: an assertion suite
// proves the fields it thought to name, while a golden proves that nothing at
// all changed without somebody saying so.
func TestTodo_PROMO_001_Golden(t *testing.T) {
	t.Parallel()
	snap := newHarness(t).build(t, fixtureRequest(t))
	assertGolden(t, "ready-promotion.txt", renderGolden(snap))
}

// TestTodo_PROMO_001_Golden_Withheld pins the same artifact for a caller who
// may see the placement but not the pay or the pool. It exists beside the
// ready table so that what a denial changes -- and what it must not change --
// is a diff between two committed files.
func TestTodo_PROMO_001_Golden_Withheld(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Compensation.SubjectDisclosable = false
	req.Authorization.Compensation.SubjectDenialReason = "policy:no_compensation_disclosure"
	req.Authorization.BudgetDisclosable = false
	req.Authorization.BudgetDenialReason = "policy:no_budget_disclosure"

	snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
	if err == nil {
		t.Fatal("a build with two withheld required inputs was accepted")
	}
	if got := promosnapshot.InputNameOf(err); got == "" {
		t.Fatalf("the refusal names no input: %v", err)
	}
	assertGolden(t, "withheld-promotion.txt", renderGolden(snap))
}

// renderGolden writes the snapshot as a stable, diffable verdict table. It
// includes the disclosed canonical texts, because a golden that hid them could
// not detect a value silently changing, and it marks every non-disclosed input
// with its availability and reason instead.
func renderGolden(snap promosnapshot.PromotionInputSnapshot) string {
	verdicts := make(map[string]enginesnapshot.CompletenessDisposition, len(snap.Completeness.Dispositions))
	for _, d := range snap.Completeness.Dispositions {
		verdicts[d.Name] = d
	}
	var b strings.Builder
	fmt.Fprintf(&b, "tenant: %s\n", snap.Tenant)
	fmt.Fprintf(&b, "subject: %s\n", snap.Subject)
	fmt.Fprintf(&b, "target position: %s\n", snap.TargetPosition)
	fmt.Fprintf(&b, "effective on: %s\n", snap.EffectiveOn)
	fmt.Fprintf(&b, "known at: %s\n", snap.KnownAt)
	fmt.Fprintf(&b, "overall completeness: %s\n", snap.Completeness.Overall)
	fmt.Fprintf(&b, "read digest: %s\n", snap.Reads.Digest)
	fmt.Fprintf(&b, "material digest: %s\n", snap.Digest)
	b.WriteString("\ninputs:\n")
	for _, in := range snap.Inputs() {
		d := verdicts[in.Name]
		fmt.Fprintf(&b, "- %s\n", in.Name)
		fmt.Fprintf(&b, "    owner:          %s\n", in.Entry.Owner)
		fmt.Fprintf(&b, "    authority:      %s\n", in.Entry.Authority)
		fmt.Fprintf(&b, "    source:         %s/%s/%s\n", in.Entry.Source.System, in.Entry.Source.Connection, in.Entry.Source.Ref)
		fmt.Fprintf(&b, "    classification: %s\n", in.Entry.Classification)
		fmt.Fprintf(&b, "    watermark:      %s\n", in.Entry.Watermark)
		fmt.Fprintf(&b, "    reference:      %s\n", in.Entry.ReferenceVersion)
		fmt.Fprintf(&b, "    policy:         %s\n", d.Policy)
		fmt.Fprintf(&b, "    availability:   %s\n", in.Availability)
		fmt.Fprintf(&b, "    verdict:        %s\n", d.Verdict)
		if in.Availability == promosnapshot.AvailabilityDisclosed {
			fmt.Fprintf(&b, "    value:          %s\n", in.CanonicalText)
			continue
		}
		fmt.Fprintf(&b, "    reason:         %s\n", in.Reason)
	}
	return b.String()
}

// assertGolden compares rendered against the committed file, or rewrites it
// under -update.
func assertGolden(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if got := rendered; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden %s does not match.\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
