package evidence_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/evidence"
)

const (
	receiptTenant = "tenant-a"
	receiptIntent = "intent/promo-15"
)

// receiptInputs builds the EVIDENCE-001 dimension set: every terminal
// dimension named, PRESENT dimensions digested, the repair dimension
// not-applicable (no repair was needed) and observations citing the
// lineage seal. wantRepair toggles the repair story for redaction tests.
func receiptInputs(t *testing.T, lineageDigest string) []evidence.DimensionInput {
	t.Helper()
	present := func(digest string) evidence.DimensionInput {
		return evidence.DimensionInput{Status: evidence.StatusPresent, Digest: digest}
	}
	inputs := map[string]evidence.DimensionInput{
		"intent":            present("sha256:intent"),
		"request":           present("sha256:request"),
		"snapshot":          present("sha256:snapshot"),
		"simulation":        present("sha256:simulation"),
		"proposal":          present("sha256:proposal"),
		"workflow-version":  present("sha256:workflow-promote-v1"),
		"approvals":         present("sha256:approvals"),
		"transaction-heads": present("sha256:heads"),
		"domain-revisions":  present("sha256:revisions"),
		"effects":           present("sha256:effects"),
		"observations":      present(lineageDigest),
		"reconciliation":    present("sha256:reconciliation"),
		"repair":            {Status: evidence.StatusNotApplicable, Note: "no repair was required"},
		"obligations":       present("sha256:obligations"),
		"terminal":          present("sha256:terminal-closed"),
	}
	ordered := make([]evidence.DimensionInput, 0, len(evidence.Dimensions))
	for _, name := range evidence.Dimensions {
		input := inputs[name]
		input.Name = name
		ordered = append(ordered, input)
	}
	return ordered
}

func mustAssemble(t *testing.T, inputs []evidence.DimensionInput) evidence.BusinessExecutionReceipt {
	t.Helper()
	receipt, err := evidence.Assemble(receiptTenant, receiptIntent, "sha256:lineage-seal", []string{"auth/hrbp", "auth/ledger"}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestTodo_EVIDENCE_001(t *testing.T) {
	receipt := mustAssemble(t, receiptInputs(t, "sha256:lineage-seal"))
	if err := receipt.Verify(); err != nil {
		t.Fatalf("fresh receipt does not verify: %v", err)
	}
	// Every terminal dimension is named exactly once, in vocabulary order.
	if len(receipt.Dimensions) != len(evidence.Dimensions) {
		t.Fatalf("receipt names %d dimensions, want %d", len(receipt.Dimensions), len(evidence.Dimensions))
	}
	for i, name := range evidence.Dimensions {
		if receipt.Dimensions[i].Name != name {
			t.Fatalf("dimension %d = %s, want %s", i, receipt.Dimensions[i].Name, name)
		}
	}
	// Statuses distinguish: repair is explicitly not-applicable, never
	// silent; everything else is present and digested.
	for _, dimension := range receipt.Dimensions {
		switch dimension.Name {
		case "repair":
			if dimension.Status != evidence.StatusNotApplicable || dimension.Note == "" {
				t.Fatalf("repair dimension = %+v, want NOT_APPLICABLE with reason", dimension)
			}
		default:
			if dimension.Status != evidence.StatusPresent || dimension.Digest == "" {
				t.Fatalf("%s dimension = %+v, want PRESENT with digest", dimension.Name, dimension)
			}
		}
	}
	// Redaction keeps verification truth: the redacted receipt verifies,
	// names every dimension still, and reports its redaction count.
	redacted, err := receipt.Redacted("caller clearance excludes observations", "observations")
	if err != nil {
		t.Fatal(err)
	}
	if err := redacted.Verify(); err != nil {
		t.Fatalf("redacted receipt does not verify: %v", err)
	}
	if len(redacted.Dimensions) != len(receipt.Dimensions) || redacted.RedactionCount() != 1 {
		t.Fatalf("redaction dropped or hid dimensions: %+v", redacted.Dimensions)
	}
	if redacted.Digest == receipt.Digest {
		t.Fatal("redaction left the seal untouched")
	}
	// The summary exposes identity and counts, never payloads.
	summary := redacted.Summary()
	for _, want := range []string{receiptIntent, "terminal=sha256:terminal-closed", "redacted=1"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q misses %q", summary, want)
		}
	}
}

func TestTodo_EVIDENCE_001_Golden(t *testing.T) {
	// The observations dimension cites a real lineage seal assembled from
	// the DATA-015 trace shape (digests chain proposal to repair).
	effective := time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)
	nodes := make([]lineage.Node, 0, len(lineage.Stages))
	prev := ""
	for i, stage := range lineage.Stages {
		digest := "sha256:golden-" + string(stage)
		nodes = append(nodes, lineage.Node{
			Stage: stage, Field: "worker.base_pay", Source: "golden", Version: "v1",
			Authority: "auth/golden", Evidence: []string{"ev-golden"},
			EffectiveAt: effective, KnownAt: effective.Add(time.Duration(i) * time.Hour),
			Digest: digest, PrevDigest: prev,
		})
		prev = digest
	}
	trace, err := lineage.Assemble(receiptTenant, receiptIntent, "worker.base_pay", nodes)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustAssemble(t, receiptInputs(t, trace.Digest))
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw) + "\n"
	path := filepath.Join("testdata", "evidence001_receipt.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestTodo_EVIDENCE_001_Race(t *testing.T) {
	first := mustAssemble(t, receiptInputs(t, "sha256:lineage-seal"))
	const workers = 16
	digests := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := evidence.Assemble(receiptTenant, receiptIntent, "sha256:lineage-seal", []string{"auth/hrbp", "auth/ledger"}, receiptInputs(t, "sha256:lineage-seal"))
			if err != nil {
				errs <- err
				return
			}
			if err := receipt.Verify(); err != nil {
				errs <- err
				return
			}
			digests <- receipt.Digest
		}()
	}
	wg.Wait()
	close(digests)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent receipt = %v", err)
	}
	for digest := range digests {
		if digest != first.Digest {
			t.Fatal("concurrent receipts diverge")
		}
	}
}

func TestTodo_EVIDENCE_001_Mutation(t *testing.T) {
	fresh := func() []evidence.DimensionInput {
		return receiptInputs(t, "sha256:lineage-seal")
	}
	// Mutant 1: an omitted dimension is refused.
	inputs := fresh()[:14]
	if _, err := evidence.Assemble(receiptTenant, receiptIntent, "sha256:lineage-seal", []string{"auth/hrbp"}, inputs); !errors.Is(err, evidence.ErrDimensionOmitted) {
		t.Fatalf("omitted dimension = %v, want ErrDimensionOmitted", err)
	}
	// Mutant 2: a PRESENT dimension without its digest is refused.
	inputs = fresh()
	inputs[0].Digest = ""
	if _, err := evidence.Assemble(receiptTenant, receiptIntent, "sha256:lineage-seal", []string{"auth/hrbp"}, inputs); !errors.Is(err, evidence.ErrDimensionUndigested) {
		t.Fatalf("undigested dimension = %v, want ErrDimensionUndigested", err)
	}
	// Mutant 3: a non-present dimension without its reason is refused.
	inputs = fresh()
	inputs[12].Note = ""
	if _, err := evidence.Assemble(receiptTenant, receiptIntent, "sha256:lineage-seal", []string{"auth/hrbp"}, inputs); !errors.Is(err, evidence.ErrDimensionUnexplained) {
		t.Fatalf("unexplained dimension = %v, want ErrDimensionUnexplained", err)
	}
	// Mutant 4: a seal edited after assembly fails verification.
	receipt := mustAssemble(t, fresh())
	receipt.Dimensions[0].Digest = "sha256:edited"
	if err := receipt.Verify(); !errors.Is(err, evidence.ErrSealBroken) {
		t.Fatalf("edited seal verifies = %v, want ErrSealBroken", err)
	}
	// Mutant 5: reasonless redaction is refused.
	if _, err := receipt.Redacted(""); !errors.Is(err, evidence.ErrDimensionUnexplained) {
		t.Fatalf("reasonless redaction = %v, want ErrDimensionUnexplained", err)
	}
}
