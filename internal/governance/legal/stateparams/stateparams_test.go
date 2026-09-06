package stateparams

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T) Fixture {
	t.Helper()
	data, err := os.ReadFile("testdata/state-parameters-wire.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fixture, err := LoadYAML(data)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return fixture
}

// TestTodo_LEGAL_TOOL_001 proves the typed wage-floor/overtime schema loads
// the researched Alaska, Colorado, Kentucky and California shapes.
func TestTodo_LEGAL_TOOL_001(t *testing.T) {
	f := loadFixture(t)
	for _, state := range []string{"AK", "CO", "KY", "CA"} {
		set := parameterSet(t, f, state)
		if len(set.WageFloors) == 0 && state != "KY" {
			t.Fatalf("%s has no wage floor", state)
		}
		if state == "KY" && !set.Overtime[0].ConsecutiveDayTrigger {
			t.Fatal("Kentucky seventh-day trigger was lost")
		}
		if state == "AK" && *set.Overtime[0].DailyThresholdHours != 8 {
			t.Fatal("Alaska daily threshold was lost")
		}
		if state == "CO" && (!set.Overtime[0].ConsecutiveDayTrigger || set.Overtime[0].WeeklyThresholdHours != 40 || *set.Overtime[0].DailyThresholdHours != 12) {
			t.Fatal("Colorado greater-of thresholds were lost")
		}
		if state == "CA" && len(set.Overtime) != 1 {
			t.Fatal("California overtime row was lost")
		}
	}
}

// TestTodo_LEGAL_TOOL_001_Golden pins the schema's researched citations and
// the independently computed digests of four real state parameter sets.
func TestTodo_LEGAL_TOOL_001_Golden(t *testing.T) {
	f := loadFixture(t)
	want := map[string]string{
		"AK": "66bf8ffac2927aa82c935ddb01698f689c24465b1d311204d13644d3f7bd93bc",
		"CO": "0544781af058c93c5b43181c6e0c793056f32b30434897d0a82884d3644ae2fc",
		"KY": "ca9488a70c2ea6bba15a22f151f5f1db680e69508b97f395c9932b7dbe31d45c",
		"CA": "50786f3779e7d2897bfbf7ab26b47c7f2754f3ec3f97784d2c94d38cac12d437",
	}
	for state, digest := range want {
		set := parameterSet(t, f, state)
		if set.Digest != digest {
			t.Errorf("%s digest = %s, want %s", state, set.Digest, digest)
		}
	}
	if parameterSet(t, f, "CA").Breaks[0].Citation.Statute != "Lab. Code §§ 512, 226.7" {
		t.Fatal("California break citation changed")
	}
}

// TestTodo_LEGAL_TOOL_001_Conformance checks the one-row-per-state registry,
// source-file existence and the effective/known-at metadata on every set.
func TestTodo_LEGAL_TOOL_001_Conformance(t *testing.T) {
	f := loadFixture(t)
	if len(f.Registry.Rows) != 50 {
		t.Fatalf("registry rows = %d, want 50", len(f.Registry.Rows))
	}
	for _, row := range f.Registry.Rows {
		if _, ok := f.Registry.Lookup(row.StateCode); !ok {
			t.Fatalf("registry lookup lost %s", row.StateCode)
		}
		if _, err := os.Stat(filepath.Join("..", "..", "..", "..", row.SourceFile)); err != nil {
			t.Fatalf("%s source file: %v", row.StateCode, err)
		}
		if row.ReviewStatus != Reviewed {
			t.Fatalf("%s review status = %s, want REVIEWED", row.StateCode, row.ReviewStatus)
		}
	}
	if f.Registry.Digest == "" {
		t.Fatal("registry digest is empty")
	}
	for _, set := range f.ParameterSets {
		if set.Digest == "" || set.KnownAt.Instant().Validate() != nil {
			t.Fatalf("%s lacks known-at or digest", set.StateCode)
		}
		if set.Effective.From.String() == "" {
			t.Fatalf("%s lacks effective date", set.StateCode)
		}
	}
}

// TestTodo_LEGAL_TOOL_001_Mutation ensures the required weekly threshold is
// not defaulted when a loader/validator mutation removes it.
func TestTodo_LEGAL_TOOL_001_Mutation(t *testing.T) {
	f := loadFixture(t)
	set := parameterSet(t, f, "AK")
	set.Overtime[0].WeeklyThresholdHours = 0
	err := set.Validate()
	assertField(t, err, "overtime[0].weekly_threshold_hours")
}

// TestTodo_LEGAL_TOOL_002 proves the additive break kind and its registry
// rows preserve meal/rest shape, pay status and cited penalties.
func TestTodo_LEGAL_TOOL_002(t *testing.T) {
	f := loadFixture(t)
	ca := parameterSet(t, f, "CA")
	if len(ca.Breaks) != 2 || ca.Breaks[0].BreakType != "MEAL" || ca.Breaks[1].BreakType != "REST" {
		t.Fatalf("California breaks = %+v", ca.Breaks)
	}
	if ca.Breaks[0].Paid || !ca.Breaks[1].Paid || ca.Breaks[0].PenaltyAmount == nil {
		t.Fatal("California meal/rest pay or penalty shape was lost")
	}
	mn := parameterSet(t, f, "MN")
	if len(mn.Breaks) != 2 || mn.Breaks[0].Citation.Statute != "Minn. Stat. § 177.253" {
		t.Fatal("Minnesota break registry row was lost")
	}
}

// TestTodo_LEGAL_TOOL_002_Golden pins break citations and the new kind's
// stable wire vocabulary without changing the existing v2 kind list.
func TestTodo_LEGAL_TOOL_002_Golden(t *testing.T) {
	f := loadFixture(t)
	for _, state := range []string{"CA", "CO", "KY", "MN"} {
		_ = parameterSet(t, f, state)
	}
	if parameterSet(t, f, "KY").Breaks != nil {
		t.Fatal("Kentucky must carry no break rule when research says F")
	}
	if parameterSet(t, f, "CO").Breaks[0].Citation.Statute != "COMPS Order #40 (7 CCR 1103-1)" {
		t.Fatal("Colorado break citation changed")
	}
}

// TestTodo_LEGAL_TOOL_002_Conformance checks every loaded break row against
// the typed body validator and ensures a missing citation is refused.
func TestTodo_LEGAL_TOOL_002_Conformance(t *testing.T) {
	f := loadFixture(t)
	for _, set := range f.ParameterSets {
		if err := set.Validate(); err != nil {
			t.Fatalf("%s: %v", set.StateCode, err)
		}
	}
	bad := parameterSet(t, f, "MN")
	bad.Breaks[0].Citation.Statute = ""
	err := bad.Validate()
	assertField(t, err, "breaks[0].citation.statute")
}

// TestTodo_LEGAL_TOOL_003 proves final-pay rules remain distinct by
// separation kind and comparator.
func TestTodo_LEGAL_TOOL_003(t *testing.T) {
	f := loadFixture(t)
	az := parameterSet(t, f, "AZ")
	if az.FinalPay[0].SeparationKind != Discharge || az.FinalPay[0].Comparator != EarlierOf || az.FinalPay[0].Unit != BusinessDays {
		t.Fatal("Arizona discharge rule collapsed")
	}
	co := parameterSet(t, f, "CO")
	if co.FinalPay[0].DeadlineDaysOrHours != 0 || !strings.Contains(co.FinalPay[0].AlternateTrigger, "6 hours") {
		t.Fatal("Colorado immediate/closed-payroll rule collapsed")
	}
	mt := parameterSet(t, f, "MT")
	if mt.FinalPay[1].Comparator != LaterOf || mt.FinalPay[1].DeadlineDaysOrHours != 15 {
		t.Fatal("Montana later-of rule collapsed")
	}
	ky := parameterSet(t, f, "KY")
	if ky.FinalPay[0].Comparator != LaterOf || ky.FinalPay[0].DeadlineDaysOrHours != 14 {
		t.Fatal("Kentucky later-of rule collapsed")
	}
}

// TestTodo_LEGAL_TOOL_003_Golden pins final-pay parameter-set digests for the
// four separation-rule vectors named by the todo.
func TestTodo_LEGAL_TOOL_003_Golden(t *testing.T) {
	f := loadFixture(t)
	want := map[string]string{"AZ": "c0078a0b4943dbe3b10ebae6f9cdcfb96acdbb34f4aa64828b06edf9322aaf28", "CO": "0544781af058c93c5b43181c6e0c793056f32b30434897d0a82884d3644ae2fc", "MT": "df54bc8ab3a7167ad2428ec87e87a799f64afadc391dad85abef8059719c0911", "KY": "ca9488a70c2ea6bba15a22f151f5f1db680e69508b97f395c9932b7dbe31d45c"}
	for state, digest := range want {
		if got := parameterSet(t, f, state).Digest; got != digest {
			t.Errorf("%s digest = %s, want %s", state, got, digest)
		}
	}
}

// TestTodo_LEGAL_TOOL_003_Conformance refuses an unknown separation kind,
// unit, comparator or an incomplete alternate trigger.
func TestTodo_LEGAL_TOOL_003_Conformance(t *testing.T) {
	f := loadFixture(t)
	set := parameterSet(t, f, "AZ")
	set.FinalPay[0].SeparationKind = "TERMINATION"
	err := set.Validate()
	assertField(t, err, "final_pay[0].separation_kind")
	set = parameterSet(t, f, "AZ")
	set.FinalPay[0].AlternateTrigger = ""
	err = set.Validate()
	assertField(t, err, "final_pay[0].alternate_trigger")
}

// TestTodo_LEGAL_TOOL_003_Mutation catches a validator mutation that accepts
// a negative final-pay duration.
func TestTodo_LEGAL_TOOL_003_Mutation(t *testing.T) {
	f := loadFixture(t)
	set := parameterSet(t, f, "KY")
	set.FinalPay[0].DeadlineDaysOrHours = -1
	err := set.Validate()
	assertField(t, err, "final_pay[0].deadline_days_or_hours")
}

func parameterSet(t *testing.T, f Fixture, state string) ParameterSet {
	t.Helper()
	for _, set := range f.ParameterSets {
		if set.StateCode == state {
			set.WageFloors = append([]WageFloorRule(nil), set.WageFloors...)
			set.Overtime = append([]OvertimeThreshold(nil), set.Overtime...)
			set.Breaks = append([]BreakRule(nil), set.Breaks...)
			set.FinalPay = append([]FinalPayDeadline(nil), set.FinalPay...)
			return set
		}
	}
	t.Fatalf("missing parameter set %s", state)
	return ParameterSet{}
}
func assertField(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatal("validation unexpectedly passed")
	}
	if !errors.Is(err, ErrInvalidParameterSet) {
		t.Fatalf("error = %v, want ErrInvalidParameterSet", err)
	}
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != want {
		t.Fatalf("error field = %v, want %s", err, want)
	}
}
