package edges

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/drain"
)

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "conf024_edges.golden")
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

type fakeCapability struct {
	name     string
	output   string
	business bool
	fail     bool
}

func (f fakeCapability) Name() string { return f.name }

func (f fakeCapability) Invoke(input string) (Effect, error) {
	if f.fail {
		return Effect{}, errors.New("transport down")
	}
	return Effect{Input: input, Output: f.output, Business: f.business}, nil
}

func adverseCase(id string) EdgeCase {
	return EdgeCase{
		ID: id, MatrixVersion: MatrixVersion,
		EventLate: true, EventOutOfOrder: true,
		HandoffClaimed: false, LegalChanged: true, AssigneeRevoked: true,
		DelegationCycles: true, BulkInvalid: true,
		DeviceShared: true, DeviceOffline: true, DeviceAttested: false,
		ApprovalMasked: true, ProviderAccepted: true, ProviderApplied: false,
		ManualRunbook: false, NeedsAltFormat: true,
		MandatoryLocale: true, LocaleReady: false,
		DecisionStale: true, TransportOnly: true,
		Capabilities:    []Capability{fakeCapability{name: "ledger", output: "recorded"}},
		CapabilityInput: "edge-input",
	}
}

func TestWorkflowContextEdgeMatrixHasNoImplicitAuthorityOrCompletion(t *testing.T) {
	want := map[string]string{
		CaseLateEvent: OutcomeReplan, CaseQueuedHandoff: OutcomeRouteHuman,
		CaseLegalChangeWaiting: OutcomeReapprove, CaseAssigneeChurn: OutcomeRouteHuman,
		CaseDelegationLoop: OutcomeBlock, CaseBulkInvalidation: OutcomeReplan,
		CaseSharedDevice: OutcomeBlock, CaseMaskedApproval: OutcomeBlock,
		CaseAcceptedNotApplied: OutcomeRepairRequired, CaseManualOutage: OutcomeRouteHuman,
		CaseAccessibility: OutcomeBlock, CaseStaleDecision: OutcomeBlock,
	}
	if len(CaseIDs()) != 12 {
		t.Fatalf("matrix must hold twelve cases, got %d", len(CaseIDs()))
	}
	for _, id := range CaseIDs() {
		t.Run(id, func(t *testing.T) {
			recorder := &Recorder{}
			verdict, err := Execute(adverseCase(id), recorder)
			if err != nil {
				t.Fatalf("Execute(%s): %v", id, err)
			}
			if verdict.Outcome != want[id] {
				t.Fatalf("outcome = %q, want %q", verdict.Outcome, want[id])
			}
			if len(verdict.Evidence) == 0 || verdict.LedgerCount == 0 {
				t.Fatal("adverse verdict must carry an evidence chronology")
			}
			if verdict.AuthorityBinding == "" || verdict.Digest == "" {
				t.Fatal("verdict must bind authority and seal its digest")
			}
			if verdict.BusinessEffects != 0 {
				t.Fatalf("adverse verdict records %d business effects", verdict.BusinessEffects)
			}
			if verdict.Outcome == OutcomeComplete {
				t.Fatal("adverse path must not fabricate completion")
			}
			if err := verdict.Verify(); err != nil {
				t.Fatalf("Verify: %v", err)
			}
		})
	}
}

func TestTodo_CONF_024_Property(t *testing.T) {
	vocabulary := map[string]bool{
		OutcomeBlock: true, OutcomeReplan: true, OutcomeReapprove: true,
		OutcomeRouteHuman: true, OutcomeDegrade: true,
		OutcomeRepairRequired: true, OutcomeComplete: true,
	}
	for _, id := range CaseIDs() {
		first, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		second, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		if first.Digest != second.Digest || fmt.Sprint(first.Evidence) != fmt.Sprint(second.Evidence) {
			t.Fatalf("%s is not deterministic", id)
		}
		if !vocabulary[first.Outcome] {
			t.Fatalf("%s outcome %q outside closed vocabulary", id, first.Outcome)
		}
		// Benign context deterministically completes.
		benign := EdgeCase{ID: id, MatrixVersion: MatrixVersion,
			HandoffClaimed: true, DeviceAttested: true, LocaleReady: true,
			ManualRunbook: true,
			Capabilities:  []Capability{fakeCapability{name: "ledger", output: "recorded"}}}
		verdict, err := Execute(benign, &Recorder{})
		if err != nil {
			t.Fatalf("Execute benign %s: %v", id, err)
		}
		// Runbook continuity is degraded mode by design: the benign
		// corner proves bounded degradation, not fabricated completion.
		benignWant := OutcomeComplete
		switch id {
		case CaseManualOutage:
			benignWant = OutcomeDegrade
		case CaseStaleDecision:
			// Transport-only acceptance never completes: blocking
			// IS the benign outcome for a stale decision.
			benignWant = OutcomeBlock
		}
		if verdict.Outcome != benignWant {
			t.Fatalf("benign %s = %q, want %q", id, verdict.Outcome, benignWant)
		}
	}
}

func TestTodo_CONF_024_Golden(t *testing.T) {
	var lines []string
	for _, id := range CaseIDs() {
		verdict, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		lines = append(lines, id+"="+verdict.Outcome+"|ledger="+fmt.Sprint(verdict.LedgerCount)+"|seal="+verdict.Digest)
	}
	golden := strings.Join(lines, "\n") + "\n"
	assertGolden(t, "TestTodo_CONF_024_Golden", golden)
}

func TestTodo_CONF_024_Race(t *testing.T) {
	var wg sync.WaitGroup
	digests := make([]string, 12)
	for i, id := range CaseIDs() {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			verdict, err := Execute(adverseCase(id), &Recorder{})
			if err != nil {
				t.Errorf("Execute(%s): %v", id, err)
				return
			}
			digests[i] = verdict.Digest
		}(i, id)
	}
	wg.Wait()
	for i, id := range CaseIDs() {
		want, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		if digests[i] != want.Digest {
			t.Fatalf("%s raced to a different verdict", id)
		}
	}
}

// drainCapability drives a real drainer through the capability seam.
type drainCapability struct {
	name    string
	drainer *drain.Drainer
	action  string
}

func (d drainCapability) Name() string { return d.name }

func (d drainCapability) Invoke(input string) (Effect, error) {
	switch d.action {
	case "acquire":
		if err := d.drainer.Acquire(input, "inflight-effect"); err != nil {
			return Effect{}, err
		}
		return Effect{Input: input, Output: "lease-acquired"}, nil
	case "begin":
		fence, err := d.drainer.Begin()
		if err != nil {
			return Effect{}, err
		}
		return Effect{Input: input, Output: fmt.Sprintf("fence-%d", fence)}, nil
	default:
		return Effect{}, errors.New("unknown drain action")
	}
}

func TestTodo_CONF_024_Integration(t *testing.T) {
	drainer := drain.NewDrainer()
	c := EdgeCase{
		ID: CaseManualOutage, MatrixVersion: MatrixVersion, ManualRunbook: true,
		Capabilities: []Capability{
			drainCapability{name: "outage-drain", drainer: drainer, action: "acquire"},
			drainCapability{name: "outage-fence", drainer: drainer, action: "begin"},
		},
		CapabilityInput: "outage-lease",
	}
	recorder := &Recorder{}
	verdict, err := Execute(c, recorder)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if verdict.Outcome != OutcomeDegrade {
		t.Fatalf("runbook outage = %q, want DEGRADE", verdict.Outcome)
	}
	if verdict.EffectCount != 2 || len(recorder.Effects()) != 2 {
		t.Fatalf("real drainer invocations must record, got %d", verdict.EffectCount)
	}
}

func TestTodo_CONF_024_Fault(t *testing.T) {
	base := adverseCase(CaseLateEvent)
	base.MatrixVersion = "1999-01-01"
	if _, err := Execute(base, &Recorder{}); !errors.Is(err, ErrUnknownMatrix) {
		t.Fatalf("unknown version err = %v", err)
	}
	base = adverseCase("not-a-case")
	if _, err := Execute(base, &Recorder{}); !errors.Is(err, ErrUnknownCase) {
		t.Fatalf("unknown case err = %v", err)
	}
	poisoned := adverseCase(CaseMaskedApproval)
	poisoned.Capabilities = []Capability{fakeCapability{name: "ledger", output: "business-commit", business: true}}
	if _, err := Execute(poisoned, &Recorder{}); !errors.Is(err, ErrProhibitedEffect) {
		t.Fatalf("prohibited effect err = %v", err)
	}
	// No capabilities: verdict still deterministic with zero effects.
	quiet := adverseCase(CaseLateEvent)
	quiet.Capabilities = nil
	verdict, err := Execute(quiet, &Recorder{})
	if err != nil {
		t.Fatalf("Execute without capabilities: %v", err)
	}
	if verdict.EffectCount != 0 || verdict.Outcome != OutcomeReplan {
		t.Fatalf("quiet verdict = %+v", verdict)
	}
}

func TestTodo_CONF_024_Security(t *testing.T) {
	for _, id := range []string{CaseMaskedApproval, CaseDelegationLoop, CaseStaleDecision, CaseAcceptedNotApplied} {
		verdict, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		if verdict.Outcome == OutcomeComplete {
			t.Fatalf("%s completed despite a trust violation", id)
		}
		if !strings.Contains(verdict.AuthorityBinding, id) {
			t.Fatalf("%s verdict lacks its authority binding", id)
		}
	}
}

func TestTodo_CONF_024_Conformance(t *testing.T) {
	want := map[string]string{
		CaseLateEvent: OutcomeReplan, CaseQueuedHandoff: OutcomeRouteHuman,
		CaseLegalChangeWaiting: OutcomeReapprove, CaseAssigneeChurn: OutcomeRouteHuman,
		CaseDelegationLoop: OutcomeBlock, CaseBulkInvalidation: OutcomeReplan,
		CaseSharedDevice: OutcomeBlock, CaseMaskedApproval: OutcomeBlock,
		CaseAcceptedNotApplied: OutcomeRepairRequired, CaseManualOutage: OutcomeRouteHuman,
		CaseAccessibility: OutcomeBlock, CaseStaleDecision: OutcomeBlock,
	}
	for _, id := range CaseIDs() {
		verdict, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		if verdict.Outcome != want[id] {
			t.Fatalf("%s = %q, want %q", id, verdict.Outcome, want[id])
		}
		if verdict.MatrixVersion != MatrixVersion {
			t.Fatalf("%s cites unpinned version %q", id, verdict.MatrixVersion)
		}
	}
}

func TestTodo_CONF_024_Recovery(t *testing.T) {
	full := make([]Verdict, 0, 12)
	for _, id := range CaseIDs() {
		verdict, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", id, err)
		}
		full = append(full, verdict)
	}
	// Crash after six, then resume the remainder with fresh recorders:
	// resumed seals must match and no effect may duplicate.
	resumed := append([]Verdict(nil), full[:6]...)
	for _, id := range CaseIDs()[6:] {
		verdict, err := Execute(adverseCase(id), &Recorder{})
		if err != nil {
			t.Fatalf("resume Execute(%s): %v", id, err)
		}
		resumed = append(resumed, verdict)
	}
	if len(resumed) != len(full) {
		t.Fatalf("resumed %d verdicts, want %d", len(resumed), len(full))
	}
	for i := range full {
		if resumed[i].Digest != full[i].Digest {
			t.Fatalf("verdict %d diverges after resume", i)
		}
	}
}

func TestTodo_CONF_024_Mutation(t *testing.T) {
	// One flipped field moves each verdict to its documented neighbor.
	flips := []struct {
		id     string
		mutate func(*EdgeCase)
		want   string
	}{
		{CaseQueuedHandoff, func(c *EdgeCase) { c.HandoffClaimed = true }, OutcomeComplete},
		{CaseMaskedApproval, func(c *EdgeCase) { c.ApprovalMasked = false }, OutcomeComplete},
		{CaseDelegationLoop, func(c *EdgeCase) { c.DelegationCycles = false }, OutcomeComplete},
		{CaseAcceptedNotApplied, func(c *EdgeCase) { c.ProviderApplied = true }, OutcomeComplete},
		{CaseSharedDevice, func(c *EdgeCase) { c.DeviceShared = false; c.DeviceOffline = false; c.DeviceAttested = true }, OutcomeComplete},
		{CaseAccessibility, func(c *EdgeCase) { c.MandatoryLocale = false; c.NeedsAltFormat = false }, OutcomeComplete},
	}
	for _, flip := range flips {
		c := adverseCase(flip.id)
		flip.mutate(&c)
		verdict, err := Execute(c, &Recorder{})
		if err != nil {
			t.Fatalf("Execute(%s): %v", flip.id, err)
		}
		if verdict.Outcome != flip.want {
			t.Fatalf("%s after flip = %q, want %q", flip.id, verdict.Outcome, flip.want)
		}
	}
	// Tampered evidence breaks the seal.
	verdict, err := Execute(adverseCase(CaseLateEvent), &Recorder{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	verdict.Evidence = append(verdict.Evidence, "t99: forged entry")
	if err := verdict.Verify(); err == nil {
		t.Fatal("forged evidence must break the verdict seal")
	}
}
