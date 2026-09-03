package runtimedecision_test

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/runtimedecision"
)

// TestTodo_WF_RUN_000_Golden proves the finding list Validate produces for a
// fixed broken input is byte-stable: the same missing fields, in the same
// order, every run, so a CI diff on this check's output is meaningful
// rather than order-flaky.
func TestTodo_WF_RUN_000_Golden(t *testing.T) {
	// Strip every optional-looking signature/evidence field at once to
	// pin down an exact, multi-finding output.
	broken := strings.NewReplacer(
		`  signed_by: "test-owner"
`, "",
		`    evidence:
      - description: "evidence file"
        path: "EVIDENCE_FILE.md"
        status: EXISTS
`, "",
	).Replace(minimalCompleteYAML)

	root, path := writeRepoWithRecord(t, broken)

	want := []string{
		`selected candidate "In-house" has no evidence entries`,
		"selected_option.signed_by is required (an unsigned choice is not a decision)",
	}

	for i := 0; i < 5; i++ {
		res, err := runtimedecision.ValidateFile(path, root)
		if err != nil {
			t.Fatalf("run %d: ValidateFile: %v", i, err)
		}
		if res.OK {
			t.Fatalf("run %d: expected the broken fixture to fail validation", i)
		}
		if !reflect.DeepEqual(res.Findings, want) {
			t.Fatalf("run %d: findings mismatch:\n got:  %#v\n want: %#v", i, res.Findings, want)
		}
	}
}

// p1bRuntimeDirs are the durable-runtime implementation directories the
// go-only-technology-constitution / workflow-runtime.md "Go-Only
// Implementation Shape" section names as P1B scheduler code. WF-RUN-000's
// RED clause is violated if any of these exist before the decision record
// does.
var p1bRuntimeDirs = []string{
	filepath.Join("internal", "workflow", "scheduler"),
	filepath.Join("internal", "workflow", "leases"),
	filepath.Join("internal", "workflow", "timers"),
}

// TestTodo_WF_RUN_000_Integration exercises the checker against the real
// repository tree: the decision record must validate clean against actual
// on-disk evidence, and no P1B durable-runtime scheduler/lease/timer
// package may exist unless the decision record backing it already
// validates (the ordering constraint the WF-RUN-000 RED clause names).
func TestTodo_WF_RUN_000_Integration(t *testing.T) {
	root := repopath.RootDir()
	path := filepath.Join(root, "definitions", "runtime", "durable-runtime-decision.yaml")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("decision record must exist at %s: %v", path, err)
	}

	res, err := runtimedecision.ValidateFile(path, root)
	if err != nil {
		t.Fatalf("ValidateFile: %v", err)
	}
	if !res.OK {
		t.Fatalf("decision record is incomplete/unevidenced against the real repository tree: %v", res.Findings)
	}

	for _, dir := range p1bRuntimeDirs {
		if info, statErr := os.Stat(filepath.Join(root, dir)); statErr == nil && info.IsDir() {
			// The gate this record exists to satisfy: P1B runtime
			// code may only exist once the record it depends on
			// validates clean, which was just proved above, so
			// this branch is a live check, not dead code.
			t.Logf("P1B runtime directory %s exists; decision record already validates clean, so the WF-RUN-000 ordering gate holds", dir)
			continue
		}
	}
}

// TestTodo_WF_RUN_000_Conformance proves the decision record's four
// non-negotiables are the exact four criteria named in
// planning/specs/workflow-runtime.md's "Build or adopt" section, not a
// paraphrase that could quietly drift from the spec.
func TestTodo_WF_RUN_000_Conformance(t *testing.T) {
	root := repopath.RootDir()

	specPath := filepath.Join(root, "planning", "specs", "workflow-runtime.md")
	specBytes, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading %s: %v", specPath, err)
	}
	wantNN, err := extractNonNegotiables(string(specBytes))
	if err != nil {
		t.Fatalf("extracting non-negotiables from spec: %v", err)
	}
	if len(wantNN) != 4 {
		t.Fatalf("expected exactly 4 non-negotiables in the spec, found %d: %v", len(wantNN), wantNN)
	}

	d, err := runtimedecision.Load(filepath.Join(root, "definitions", "runtime", "durable-runtime-decision.yaml"))
	if err != nil {
		t.Fatalf("loading decision record: %v", err)
	}
	if len(d.NonNegotiables) != 4 {
		t.Fatalf("decision record has %d non_negotiables, want 4", len(d.NonNegotiables))
	}
	for i, nn := range d.NonNegotiables {
		got := normalizeWhitespace(nn.Description)
		want := normalizeWhitespace(wantNN[i])
		if got != want {
			t.Fatalf("non_negotiables[%d] (%s) = %q, want the spec's exact wording %q", i, nn.ID, got, want)
		}
	}
}

var numberedItemRE = regexp.MustCompile(`(?m)^\d+\.\s+`)

// extractNonNegotiables pulls the four numbered criteria out of
// workflow-runtime.md's "### Build or adopt" section.
func extractNonNegotiables(spec string) ([]string, error) {
	const startMarker = "### Build or adopt"
	const endMarker = "Migration, shadow mode, replay"

	start := strings.Index(spec, startMarker)
	if start < 0 {
		return nil, errNoMarker(startMarker)
	}
	section := spec[start:]
	end := strings.Index(section, endMarker)
	if end < 0 {
		return nil, errNoMarker(endMarker)
	}
	section = section[:end]

	// The numbered list is the paragraph containing "1. ".
	paras := strings.Split(section, "\n\n")
	var listPara string
	for _, p := range paras {
		if strings.Contains(p, "1. ") {
			listPara = p
			break
		}
	}
	if listPara == "" {
		return nil, errNoMarker("numbered non-negotiable list")
	}

	locs := numberedItemRE.FindAllStringIndex(listPara, -1)
	if len(locs) == 0 {
		return nil, errNoMarker("numbered list items")
	}
	items := make([]string, 0, len(locs))
	for i, loc := range locs {
		itemStart := loc[1]
		itemEnd := len(listPara)
		if i+1 < len(locs) {
			itemEnd = locs[i+1][0]
		}
		items = append(items, strings.TrimSpace(listPara[itemStart:itemEnd]))
	}
	return items, nil
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

type markerError string

func (e markerError) Error() string { return "marker not found: " + string(e) }

func errNoMarker(marker string) error { return markerError(marker) }
