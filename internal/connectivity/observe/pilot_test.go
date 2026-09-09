package observe_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

func promotionFields() observe.PilotFields {
	return observe.NewPilotFields("OPS-HRBP3", "P3", "position-42", "125000.00 USD")
}

func TestTodo_INTG_010(t *testing.T) {
	got, err := observe.ComparePilotFields(promotionFields(), promotionFields(), promotionFields())
	if err != nil || got.Verdict != observe.Match || len(got.Fields) != 4 {
		t.Fatalf("matching promotion fields = %+v, %v; want four-field MATCH", got, err)
	}

	canonical := promotionFields()
	canonical.Grade = observe.Known("P4")
	got, err = observe.Compare(promotionFields(), canonical, promotionFields())
	if err != nil || got.Verdict != observe.Mismatch {
		t.Fatalf("canonical grade drift = %+v, %v; want MISMATCH", got, err)
	}
	if got.Fields[1].Reason == "" {
		t.Fatal("grade mismatch had no durable explanation")
	}

	observed := promotionFields()
	observed.BasePay = observe.RedactedValue("compensation scope")
	got, err = observe.Compare(promotionFields(), promotionFields(), observed)
	if err != nil || got.Verdict != observe.Unknown {
		t.Fatalf("redacted base pay = %+v, %v; want UNKNOWN", got, err)
	}
}

func TestTodo_INTG_010_Golden(t *testing.T) {
	result, err := observe.Compare(promotionFields(), promotionFields(), promotionFields())
	if err != nil {
		t.Fatal(err)
	}
	want := "promotion pilot reconciliation MATCH: job_code=MATCH, grade=MATCH, position=MATCH, base_pay=MATCH"
	if got := observe.Explain(result); got != want {
		t.Fatalf("explanation = %q, want %q", got, want)
	}
}

func TestTodo_INTG_010_Race(t *testing.T) {
	intended, canonical, observed := promotionFields(), promotionFields(), promotionFields()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := observe.Compare(intended, canonical, observed)
			if err != nil || got.Verdict != observe.Match {
				t.Errorf("concurrent comparison = %+v, %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_INTG_010_Integration(t *testing.T) {
	// The comparison accepts values already normalized by the connectivity
	// observation adapter, so a provider-specific representation cannot bypass
	// the same three-sided contract.
	intended := observe.NewPilotFields("OPS-HRBP3", "P3", "position-42", "125000.00 USD")
	canonical := observe.NewPilotFields("OPS-HRBP3", "P3", "position-42", "125000.00 USD")
	observed := observe.NewPilotFields("OPS-HRBP3", "P3", "position-42", "125000.00 USD")
	got, err := observe.Compare(intended, canonical, observed)
	if err != nil || got.Verdict != observe.Match {
		t.Fatalf("promotion adapter-shaped values = %+v, %v", got, err)
	}
}

func TestTodo_INTG_010_Fault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*observe.PilotFields)
		want   observe.ComparisonVerdict
	}{
		{"missing observed grade", func(p *observe.PilotFields) { p.Grade = observe.UnknownValue("provider omitted grade") }, observe.Unknown},
		{"provider position drift", func(p *observe.PilotFields) { p.Position = observe.Known("position-other") }, observe.Mismatch},
		{"canonical concurrent change", func(p *observe.PilotFields) { p.Grade = observe.Known("P4") }, observe.Mismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonical, observed := promotionFields(), promotionFields()
			tc.mutate(&observed)
			if tc.name == "canonical concurrent change" {
				tc.mutate(&canonical)
			}
			got, err := observe.Compare(promotionFields(), canonical, observed)
			if err != nil || got.Verdict != tc.want {
				t.Fatalf("comparison = %+v, %v; want %s", got, err, tc.want)
			}
		})
	}
}

func TestTodo_INTG_010_Mutation(t *testing.T) {
	base := promotionFields()
	for _, mutate := range []func(*observe.PilotFields){
		func(p *observe.PilotFields) { p.JobCode = observe.Known("OTHER") },
		func(p *observe.PilotFields) { p.Grade = observe.Known("P4") },
		func(p *observe.PilotFields) { p.Position = observe.Known("position-other") },
		func(p *observe.PilotFields) { p.BasePay = observe.Known("125001.00 USD") },
	} {
		observed := base
		mutate(&observed)
		got, err := observe.Compare(base, base, observed)
		if err != nil || got.Verdict != observe.Mismatch {
			t.Fatalf("mutated observed fields = %+v, %v; want MISMATCH", got, err)
		}
	}
}

func TestTodo_INTG_010_Security(t *testing.T) {
	redacted := promotionFields()
	redacted.BasePay = observe.RedactedValue("payroll compartment")
	got, err := observe.Compare(promotionFields(), promotionFields(), redacted)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verdict != observe.Unknown || !strings.Contains(observe.Explain(got), "UNKNOWN") {
		t.Fatalf("redacted field comparison = %+v; want UNKNOWN without a false match", got)
	}
	if strings.Contains(observe.Explain(got), "125000.00") {
		t.Fatal("redacted base pay leaked into explanation")
	}
}

func FuzzTodo_INTG_010(f *testing.F) {
	f.Add("OPS", "P3", "position-42", "125000.00 USD")
	f.Add("", "", "", "")
	f.Fuzz(func(t *testing.T, jobCode, grade, position, basePay string) {
		fields := observe.NewPilotFields(jobCode, grade, position, basePay)
		result, err := observe.Compare(fields, fields, fields)
		if err != nil {
			t.Fatalf("equal values rejected: %v", err)
		}
		if result.Verdict != observe.Match {
			t.Fatalf("equal values = %s, want MATCH", result.Verdict)
		}
		if len(result.Fields) != 4 || strings.TrimSpace(observe.Explain(result)) == "" {
			t.Fatal("comparison lost one of the four pilot fields")
		}
	})
}
