package records

import (
	"strings"
	"testing"
	"time"
)

var recordsNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func recordsFixture() SimulationRequest {
	cutoff := recordsNow.AddDate(0, 0, -400)
	return SimulationRequest{
		AsOf:   recordsNow,
		Copies: []Copy{{ID: "copy-1", RecordSeries: "promotion", Custodian: "records", Jurisdiction: "US-CA", CreatedAt: cutoff.Add(-time.Hour), CutoffAt: &cutoff, ArchiveAcknowledged: true}},
		Rules:  []RetentionRule{{RecordSeries: "promotion", Jurisdiction: "US-CA", MinimumDays: 365, AuthorityRef: "legal-pack/2026.1"}},
	}
}

func TestTodo_RECORDS_DISP_001(t *testing.T) {
	report, err := Simulate(recordsFixture())
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != Eligible || report.Copies[0].Status != Eligible {
		t.Fatalf("status = %s, copy=%+v", report.Status, report.Copies[0])
	}
	if report.DeletionCount != 0 || report.DeletedBytes != 0 {
		t.Fatalf("simulation deleted bytes: %+v", report)
	}
	if report.Digest == "" || !strings.Contains(report.Explain(), report.Digest) {
		t.Fatalf("missing evidence: %+v", report)
	}
}

func TestTodo_RECORDS_DISP_001_Security(t *testing.T) {
	in := recordsFixture()
	in.Copies[0].ActiveHoldRefs = []string{"hold-1"}
	report, err := Simulate(in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != BlockedWithReasons || len(report.Blockers) == 0 {
		t.Fatalf("active hold did not block: %+v", report)
	}
	in = recordsFixture()
	in.Copies[0].ArchiveAcknowledged = false
	report, err = Simulate(in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != BlockedWithReasons {
		t.Fatalf("missing archive acknowledgement did not block: %+v", report)
	}
}

func TestTodo_RECORDS_DISP_001_Mutation(t *testing.T) {
	in := recordsFixture()
	in.Rules = append(in.Rules, RetentionRule{RecordSeries: "promotion", Jurisdiction: "US-NY", MinimumDays: 1000, MaximumDays: 1100, AuthorityRef: "legal-pack/2026.1"})
	in.Rules[0].MaximumDays = 500
	report, err := Simulate(in)
	if err != nil || report.Status != BlockedWithReasons {
		t.Fatalf("cross-jurisdiction schedule conflict = report=%+v err=%v", report, err)
	}
	in = recordsFixture()
	in.Rules[0].MinimumDays++
	base, err := Simulate(recordsFixture())
	if err != nil {
		t.Fatal(err)
	}
	mutated, err := Simulate(in)
	if err != nil {
		t.Fatal(err)
	}
	if base.Digest == mutated.Digest {
		t.Fatal("schedule mutation did not change evidence digest")
	}
}

func TestRecordsSimulatorReturnsRepairForUnknownCustodianAndSeries(t *testing.T) {
	in := recordsFixture()
	in.Copies[0].Custodian = ""
	in.Copies[0].RecordSeries = ""
	report, err := Simulate(in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != RepairRequired || len(report.Blockers) < 2 {
		t.Fatalf("unknown metadata was not repairable: %+v", report)
	}
}

func TestRecordsSimulatorRepairAndCorrection(t *testing.T) {
	in := recordsFixture()
	in.Copies[0].CutoffAt = nil
	report, err := Simulate(in)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != RepairRequired {
		t.Fatalf("missing cutoff status = %s", report.Status)
	}
	cutoff := recordsNow.AddDate(0, 0, -300)
	corrected := recordsNow.AddDate(0, 0, -200)
	in = recordsFixture()
	in.Copies[0].CutoffAt = &cutoff
	in.Corrections = []CutoffCorrection{{CopyID: "copy-1", At: recordsNow.Add(-time.Hour), PreviousCutoff: &cutoff, CorrectedCutoff: &corrected, Reason: "custodian correction"}}
	report, err = Simulate(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.CutoffHistory) != 1 || report.CutoffHistory[0].To == nil {
		t.Fatalf("correction history missing: %+v", report)
	}
}
