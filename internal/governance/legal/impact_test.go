package legal

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func impactChange() RuleChange {
	return RuleChange{RuleID: "ca-final-pay", OldRelease: "2025.3", NewRelease: "2026.1", EffectiveDate: 100, Watermark: 120}
}

func impactScopes() []Scope {
	return []Scope{
		{Tenant: "acme", BusinessDate: 110, Releases: map[string]string{"ca-final-pay": "2025.3"}, Calculations: []string{"final-pay"}, Workflows: []string{"separation"}, Notices: []string{"final-pay-notice"}, PendingApprovals: []string{"appr-1"}},
		{Tenant: "globex", BusinessDate: 110, Releases: map[string]string{"ca-final-pay": "2026.1"}, Calculations: []string{"final-pay"}},
		{Tenant: "initech", BusinessDate: 90, Releases: map[string]string{"ca-final-pay": "2025.3"}, Calculations: []string{"final-pay"}},
		{Tenant: "umbrella", BusinessDate: 115, Releases: map[string]string{"ca-final-pay": "2025.3"}},
	}
}

func TestTodo_LEGAL_003(t *testing.T) {
	report, err := AssessImpact(impactChange(), 120, impactScopes())
	if err != nil {
		t.Fatalf("AssessImpact: %v", err)
	}
	// acme runs the old release at business date 110 >= effective 100:
	// affected with all four actions. globex already runs the new
	// release, initech's business date precedes effectiveness, and
	// umbrella has no bindings: unaffected.
	if len(report.Affected) != 2 {
		t.Fatalf("affected = %v", report.Affected)
	}
	acme := report.Affected[0]
	if acme.Tenant != "acme" || acme.OldRelease != "2025.3" {
		t.Fatalf("acme=%+v", acme)
	}
	for _, action := range []string{ActionRecompute, ActionNotice, ActionReapproval, ActionHold} {
		found := false
		for _, have := range acme.Actions {
			if have == action {
				found = true
			}
		}
		if !found {
			t.Fatalf("acme actions = %v", acme.Actions)
		}
	}
	if report.Unaffected != 2 {
		t.Fatalf("unaffected = %d", report.Unaffected)
	}
	if err := report.Verify(impactChange()); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: hollow changes, same-release changes and future watermarks refuse.
	if _, err := AssessImpact(RuleChange{}, 120, impactScopes()); err == nil {
		t.Fatal("hollow change assessed")
	}
	same := impactChange()
	same.NewRelease = same.OldRelease
	if _, err := AssessImpact(same, 120, impactScopes()); err == nil {
		t.Fatal("same-release change assessed")
	}
	if _, err := AssessImpact(impactChange(), 119, impactScopes()); err == nil {
		t.Fatal("future watermark assessed")
	}
	// Old outcomes retain their original release: inputs never mutate.
	scopes := impactScopes()
	if _, err := AssessImpact(impactChange(), 120, scopes); err != nil {
		t.Fatal(err)
	}
	if scopes[0].Releases["ca-final-pay"] != "2025.3" {
		t.Fatal("assessment rewrote historical results")
	}
}

func TestTodo_LEGAL_003_Property(t *testing.T) {
	first, err := AssessImpact(impactChange(), 120, impactScopes())
	if err != nil {
		t.Fatal(err)
	}
	second, err := AssessImpact(impactChange(), 120, impactScopes())
	if err != nil || first.Digest != second.Digest {
		t.Fatal("impact is not deterministic")
	}
	// Affected scopes always run the old release.
	for _, affected := range first.Affected {
		if affected.OldRelease != "2025.3" {
			t.Fatalf("affected=%+v", affected)
		}
	}
	// Business dates decide: moving initech past effectiveness affects it.
	moved := impactScopes()
	moved[2].BusinessDate = 100
	changed, err := AssessImpact(impactChange(), 120, moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Affected) != 3 {
		t.Fatalf("affected=%v", changed.Affected)
	}
}

func TestTodo_LEGAL_003_Golden(t *testing.T) {
	report, err := AssessImpact(impactChange(), 120, impactScopes())
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{"rule=" + report.RuleID, "affected=" + strings.Join([]string{report.Affected[0].Tenant, report.Affected[1].Tenant}, ",")}
	for _, affected := range report.Affected {
		lines = append(lines, affected.Tenant+"="+strings.Join(affected.Actions, ","))
	}
	lines = append(lines, "digest="+report.Digest)
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "legal003_impact.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_LEGAL_003_Race(t *testing.T) {
	registry := NewScopeRegistry()
	for _, scope := range impactScopes() {
		if err := registry.Register(scope); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	const workers = 16
	var wg sync.WaitGroup
	digests := make([]string, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			report, err := AssessImpact(impactChange(), 120, registry.Snapshot())
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = report.Digest
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if digests[i] != digests[0] {
			t.Fatalf("worker %d diverged", i)
		}
	}
	if err := registry.Register(impactScopes()[0]); err == nil {
		t.Fatal("duplicate scope registered")
	}
}

func TestTodo_LEGAL_003_Security(t *testing.T) {
	// Unpinned scopes never join the affected set.
	rogue := impactScopes()
	rogue[0].Releases = map[string]string{}
	report, err := AssessImpact(impactChange(), 120, rogue)
	if err != nil {
		t.Fatal(err)
	}
	for _, affected := range report.Affected {
		if affected.Tenant == "acme" {
			t.Fatal("unpinned scope assessed as affected")
		}
	}
	// Forged reports never verify.
	full, err := AssessImpact(impactChange(), 120, impactScopes())
	if err != nil {
		t.Fatal(err)
	}
	full.Unaffected = 99
	if err := full.Verify(impactChange()); err == nil {
		t.Fatal("forged report verified")
	}
}

func TestTodo_LEGAL_003_Mutation(t *testing.T) {
	base, err := AssessImpact(impactChange(), 120, impactScopes())
	if err != nil {
		t.Fatal(err)
	}
	// New-release change re-identifies the report.
	advanced := impactChange()
	advanced.NewRelease = "2026.2"
	changed, err := AssessImpact(advanced, 120, impactScopes())
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == base.Digest {
		t.Fatal("release mutation kept the report digest")
	}
}
