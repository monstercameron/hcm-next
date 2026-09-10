package correction

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_CONF_006_StoreTamper is the white-box half of the CONF-006
// mutation matrix: it simulates ledger corruption beneath the public API
// (edited amounts riding on the original digest) and requires
// AppendCorrection to refuse the drift instead of superseding a run that
// is no longer the filed original.
func TestTodo_CONF_006_StoreTamper(t *testing.T) {
	rounding, err := values.ParseRoundingMode("HALF_UP")
	if err != nil {
		t.Fatal(err)
	}
	dec := func(s string) values.Decimal {
		d, err := values.NewDecimal(s, 2, rounding)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	rule := RuleVersion{ID: "2026-B", OvertimeMultiplier: dec("1.50"), TaxRate: dec("0.20")}
	inputs := Inputs{EmployeeID: "ana", RegularHours: dec("80"), OvertimeHours: dec("10"), HourlyRate: dec("25.00")}
	known := time.Date(2026, time.January, 16, 9, 0, 0, 0, time.UTC)
	original, err := Calculate("run/2026-01", inputs, rule, "2026-01-15", known)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	if err := store.AppendRun(original); err != nil {
		t.Fatal(err)
	}
	// Corrupt the filed copy beneath the API: edited gross, stale digest.
	corrupted := original
	corrupted.Gross = dec("9999.99")
	store.runs[original.RunID] = corrupted

	proposal := Propose("corr/evil", original, original, Delta{})
	proposal.State = CorrectionApproved
	proposal.Approvals = []string{"payroll-lead"}
	if _, err := store.AppendCorrection(proposal); !errors.Is(err, ErrOriginalTampered) {
		t.Fatalf("drifted-original append = %v, want ErrOriginalTampered", err)
	}
}
