package journey

import (
	"sort"
	"strings"
	"testing"
)

// samplePages is the set every table-driven test in this package runs over:
// the three states the page has a distinct layout for. Adding a fourth
// fixture here automatically subjects it to every structural, escaping,
// landmark and labelling assertion in the package.
func samplePages() map[string]Page {
	return map[string]Page{
		"list":      SampleListPage(),
		"detail":    SampleDetailPage(),
		"completed": SampleCompletedDetailPage(),
	}
}

// sampleNames returns the fixture names in a stable order, so failures read
// the same way on every run.
func sampleNames() []string {
	names := make([]string, 0, len(samplePages()))
	for name := range samplePages() {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestSamplePagesShareTheirChrome(t *testing.T) {
	for _, name := range sampleNames() {
		p := samplePages()[name]
		t.Run(name, func(t *testing.T) {
			if p.Title == "" {
				t.Error("Title is empty; the browser tab and the document outline both need it")
			}
			if p.Brand == "" || p.TenantLabel == "" {
				t.Error("the masthead needs both a brand and a tenant label")
			}
			if p.Principal.Subject == "" || len(p.Principal.Roles) == 0 || p.Principal.Purpose == "" {
				t.Error("the principal chip needs a subject, at least one role and a purpose")
			}
			if len(p.Nav) == 0 {
				t.Error("the masthead has no navigation")
			}
			current := 0
			for _, l := range p.Nav {
				if l.Label == "" || l.Href == "" {
					t.Errorf("nav link %+v is missing a label or an href", l)
				}
				if l.Current {
					current++
				}
			}
			if current != 1 {
				t.Errorf("%d nav links are marked current, want exactly 1", current)
			}
			if p.Notice == nil {
				t.Error("every fixture carries a notice so the live region is exercised")
			}
			if p.Footer.PolicyVersion == "" || p.Footer.CellID == "" || p.Footer.BuildRef == "" {
				t.Error("the provenance line needs a policy version, a cell id and a build ref")
			}
		})
	}
}

func TestSamplePagesSetExactlyOneView(t *testing.T) {
	for _, name := range sampleNames() {
		p := samplePages()[name]
		t.Run(name, func(t *testing.T) {
			if (p.List == nil) == (p.Detail == nil) {
				t.Fatalf("want exactly one of List and Detail set, got List=%v Detail=%v", p.List != nil, p.Detail != nil)
			}
		})
	}
}

// TestSampleListCoversEveryStageTone matters because the stage chip is the
// list's densest piece of meaning: if the fixture only ever showed one
// tone, the golden document would not prove the other three render at all.
func TestSampleListCoversEveryStageTone(t *testing.T) {
	v := SampleListPage().List
	if v == nil {
		t.Fatal("SampleListPage has no List view")
	}
	seen := map[string]bool{}
	for _, j := range v.Journeys {
		if j.WorkerName == "" || j.Href == "" || j.IntentID == "" {
			t.Errorf("journey %+v is missing a name, href or intent id", j)
		}
		if j.StageLabel == "" {
			t.Errorf("journey %s has a stage with no label; the chip would be a bare color", j.IntentID)
		}
		seen[j.StageTone] = true
	}
	for _, tone := range []string{toneInfo, toneSuccess, toneWarning, toneDanger} {
		if !seen[tone] {
			t.Errorf("no sample journey uses the %q stage tone", tone)
		}
	}
}

// TestSampleFormCoversEveryFieldKind keeps the fixture exercising every
// branch of fieldNode.
func TestSampleFormCoversEveryFieldKind(t *testing.T) {
	form := SampleListPage().List.Form
	if form.Action == "" {
		t.Error("the proposal form posts nowhere")
	}
	if form.Hidden["csrf_token"] == "" {
		t.Error("the proposal form carries no CSRF token")
	}
	kinds := map[string]bool{}
	ids := map[string]bool{}
	for _, f := range form.Fields {
		if f.ID == "" || f.Name == "" || f.Label == "" {
			t.Errorf("field %+v is missing an id, name or label", f)
		}
		if ids[f.ID] {
			t.Errorf("field id %q is used twice; label association would be ambiguous", f.ID)
		}
		ids[f.ID] = true
		kinds[f.Kind] = true
	}
	for _, kind := range []string{fieldKindText, fieldKindNumber, fieldKindDate, fieldKindSelect, fieldKindTextarea} {
		if !kinds[kind] {
			t.Errorf("the sample form has no %q field", kind)
		}
	}
	var adorned bool
	for _, f := range form.Fields {
		if f.Prefix != "" && f.Suffix != "" {
			adorned = true
		}
	}
	if !adorned {
		t.Error("no sample field carries both a prefix and a suffix adornment")
	}
}

// TestSampleDetailPopulatesEverySection is what makes the detail golden
// meaningful: a section whose slice is empty renders as nothing at all, so
// an under-filled fixture would quietly stop testing half the page.
func TestSampleDetailPopulatesEverySection(t *testing.T) {
	for _, name := range []string{"detail", "completed"} {
		d := samplePages()[name].Detail
		t.Run(name, func(t *testing.T) {
			if d == nil {
				t.Fatal("no Detail view")
			}
			checks := []struct {
				what string
				n    int
			}{
				{"steps", len(d.Steps)},
				{"proposal facts", len(d.Proposal)},
				{"comparison rows", len(d.Comparison)},
				{"findings", len(d.Findings)},
				{"engine facts", len(d.Engine)},
				{"nodes", len(d.Nodes)},
				{"work items", len(d.WorkItems)},
				{"evidence", len(d.Evidence)},
				{"timeline events", len(d.Timeline)},
				{"actions", len(d.Actions)},
			}
			for _, c := range checks {
				if c.n == 0 {
					t.Errorf("the %s fixture has no %s", name, c.what)
				}
			}
		})
	}
}

// TestSampleDetailStatesDifferInTheWaysThatMatter guards against the
// completed fixture drifting into a copy of the open one, which would make
// the third golden worthless.
func TestSampleDetailStatesDifferInTheWaysThatMatter(t *testing.T) {
	open := SampleDetailPage().Detail
	done := SampleCompletedDetailPage().Detail

	if open.Ledger != nil {
		t.Error("the open journey should have no ledger fact yet")
	}
	if done.Ledger == nil {
		t.Fatal("the completed journey should have recorded exactly one ledger fact")
	}
	if done.Ledger.StreamKey == "" || done.Ledger.Sequence == "" || done.Ledger.Digest == "" {
		t.Error("the ledger card is missing its identifying fields")
	}

	var openActive, doneActive int
	for _, s := range open.Steps {
		if s.State == stepActive {
			openActive++
		}
	}
	for _, s := range done.Steps {
		if s.State == stepActive {
			doneActive++
		}
	}
	if openActive != 1 {
		t.Errorf("the open journey has %d active steps, want exactly 1", openActive)
	}
	if doneActive != 0 {
		t.Errorf("the completed journey has %d active steps, want 0", doneActive)
	}

	if len(done.Timeline) <= len(open.Timeline) {
		t.Error("completing the journey should have added timeline events")
	}
	if len(done.Evidence) <= len(open.Evidence) {
		t.Error("completing the journey should have added evidence records")
	}
	for _, a := range done.Actions {
		if !a.Disabled || a.DisabledReason == "" {
			t.Errorf("action %q should be refused with a reason once the journey is closed", a.ID)
		}
	}
}

// TestSampleActionsAreSelfContained: each action is its own form, so each
// needs its own route and its own CSRF token. Sharing one would mean the
// page depended on client-side state to decide what a submit does.
func TestSampleActionsAreSelfContained(t *testing.T) {
	actions := SampleDetailPage().Detail.Actions
	ids := map[string]bool{}
	variants := map[string]bool{}
	var enabled, disabledWithReason, actsAs int
	for _, a := range actions {
		if a.ID == "" || a.Label == "" || a.Action == "" {
			t.Errorf("action %+v is missing an id, label or route", a)
		}
		if ids[a.ID] {
			t.Errorf("action id %q is used twice", a.ID)
		}
		ids[a.ID] = true
		variants[a.Variant] = true
		if a.Hidden["csrf_token"] == "" {
			t.Errorf("action %q carries no CSRF token", a.ID)
		}
		if a.Disabled {
			if a.DisabledReason != "" {
				disabledWithReason++
			}
		} else {
			enabled++
		}
		if a.ActsAs != "" {
			actsAs++
		}
	}
	for _, variant := range []string{"primary", "secondary", "danger"} {
		if !variants[variant] {
			t.Errorf("no sample action uses the %q variant", variant)
		}
	}
	if enabled == 0 {
		t.Error("the open journey offers no action the caller can actually take")
	}
	if disabledWithReason == 0 {
		t.Error("no sample action exercises the refused-with-a-reason state")
	}
	if actsAs == 0 {
		t.Error("no sample action names the principal the engine acts as")
	}
}

// TestSampleSubjectMatchesTheBriefedPromotion pins the one promotion the
// whole fixture is about, so a careless edit to the numbers is caught here
// rather than being silently blessed into the golden files.
func TestSampleSubjectMatchesTheBriefedPromotion(t *testing.T) {
	j := SampleDetailPage().Detail.Journey
	if j.WorkerName != "Omar Reyes" {
		t.Errorf("worker = %q, want Omar Reyes", j.WorkerName)
	}
	for _, want := range []string{"OPS-HRBP2", "P2", "OPS-HRBP3", "P3"} {
		if !strings.Contains(j.Headline, want) {
			t.Errorf("headline %q does not mention %q", j.Headline, want)
		}
	}
	for _, want := range []string{"93,000.00", "98,000.00", "USD"} {
		if !strings.Contains(j.PayLine, want) {
			t.Errorf("pay line %q does not mention %q", j.PayLine, want)
		}
	}
	if j.EffectiveDate != "1 Jun 2026" {
		t.Errorf("effective date = %q, want 1 Jun 2026", j.EffectiveDate)
	}
}
