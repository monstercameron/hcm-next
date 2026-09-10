package legal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func statusObligation() Obligation {
	return Obligation{
		Type: ObligationTypeFinalPayDeadline, Authority: "ca-labor-code-203",
		Subject: "worker:w1", Trigger: "separation", TriggerDay: 100,
		Due:    DueRule{OffsetDays: 3, Calendar: "calendar", Version: "ca-2026.1"},
		Action: "issue final pay", Evidence: "payroll:run-9",
		Owner: "payroll-ops", Risk: "penalties",
	}
}

func TestTodo_LEGAL_004(t *testing.T) {
	obligation, err := OpenObligation(statusObligation())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if obligation.Status != ObligationOpen || obligation.DueDay != 103 {
		t.Fatalf("obligation=%+v", obligation)
	}
	if err := obligation.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: every missing binding refuses.
	base := statusObligation()
	cases := map[string]func(*Obligation){
		"missing authority": func(o *Obligation) { o.Authority = "" },
		"missing subject":   func(o *Obligation) { o.Subject = "" },
		"missing trigger":   func(o *Obligation) { o.Trigger = "" },
		"missing calendar":  func(o *Obligation) { o.Due.Calendar = "" },
		"missing version":   func(o *Obligation) { o.Due.Version = "" },
		"missing action":    func(o *Obligation) { o.Action = "" },
		"missing evidence":  func(o *Obligation) { o.Evidence = "" },
		"missing owner":     func(o *Obligation) { o.Owner = "" },
		"missing risk":      func(o *Obligation) { o.Risk = "" },
	}
	for name, mutate := range cases {
		candidate := base
		mutate(&candidate)
		if _, err := OpenObligation(candidate); err == nil {
			t.Fatalf("%s resolved", name)
		}
	}
	unknown := base
	unknown.Type = ObligationTypeUnspecified
	if _, err := OpenObligation(unknown); err == nil {
		t.Fatal("off-vocabulary type resolved")
	}
	// Contradictory satisfaction without evidence never reports satisfied.
	if _, err := Satisfy(obligation, ""); err == nil {
		t.Fatal("evidence-free satisfaction reported")
	}
	satisfied, err := Satisfy(obligation, "payroll:run-10")
	if err != nil || satisfied.Status != ObligationSatisfied {
		t.Fatalf("satisfied=%+v err=%v", satisfied, err)
	}
	// Waiver requires explicit authority and never deletes the original.
	if _, err := Waive(obligation, ""); err == nil {
		t.Fatal("authority-free waiver granted")
	}
	waived, err := Waive(obligation, "ca-dlse-waiver-1")
	if err != nil || waived.Status != ObligationWaived || waived.WaiverAuthority == "" {
		t.Fatalf("waived=%+v err=%v", waived, err)
	}
	if obligation.Status != ObligationOpen {
		t.Fatal("waiver rewrote the original obligation")
	}
	blocked, err := Block(obligation, "awaiting separation date")
	if err != nil || blocked.Status != ObligationBlocked {
		t.Fatalf("blocked=%+v err=%v", blocked, err)
	}
}

func TestTodo_LEGAL_004_Property(t *testing.T) {
	obligation, err := OpenObligation(statusObligation())
	if err != nil {
		t.Fatal(err)
	}
	// Due computation is exact and deterministic.
	again, err := OpenObligation(statusObligation())
	if err != nil || again.DueDay != obligation.DueDay || again.Digest != obligation.Digest {
		t.Fatal("due computation is not deterministic")
	}
	// Overdue holds exactly past the due date.
	if aged := Age(obligation, 103); aged.Status != ObligationOpen {
		t.Fatalf("due-date boundary aged to %q", aged.Status)
	}
	if aged := Age(obligation, 104); aged.Status != ObligationOverdue {
		t.Fatalf("past-due stayed %q", aged.Status)
	}
	// Terminal states are stable under time.
	satisfied, err := Satisfy(obligation, "payroll:run-10")
	if err != nil {
		t.Fatal(err)
	}
	if aged := Age(satisfied, 999); aged.Status != ObligationSatisfied {
		t.Fatalf("satisfied obligation aged to %q", aged.Status)
	}
	waived, err := Waive(obligation, "ca-dlse-waiver-1")
	if err != nil {
		t.Fatal(err)
	}
	if aged := Age(waived, 999); aged.Status != ObligationWaived {
		t.Fatalf("waived obligation aged to %q", aged.Status)
	}
}

func TestTodo_LEGAL_004_Golden(t *testing.T) {
	obligation, err := OpenObligation(statusObligation())
	if err != nil {
		t.Fatal(err)
	}
	overdue := Age(obligation, 200)
	lines := []string{
		"type=" + obligation.Type.String(),
		"due=" + strings.Join([]string{"trigger+3d", obligation.Due.Calendar, obligation.Due.Version}, "|"),
		"open=" + obligation.Status + " aged=" + overdue.Status,
		"digest=" + obligation.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "legal004_obligation.golden")
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

func TestTodo_LEGAL_004_Security(t *testing.T) {
	obligation, err := OpenObligation(statusObligation())
	if err != nil {
		t.Fatal(err)
	}
	// Satisfying a waived or blocked obligation refuses: terminal states
	// never silently reopen.
	waived, err := Waive(obligation, "ca-dlse-waiver-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Satisfy(waived, "payroll:run-11"); err == nil {
		t.Fatal("waived obligation satisfied")
	}
	if _, err := Waive(waived, "second-authority"); err == nil {
		t.Fatal("waived obligation re-waived")
	}
	blocked, err := Block(obligation, "awaiting date")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Satisfy(blocked, "payroll:run-11"); err == nil {
		t.Fatal("blocked obligation satisfied")
	}
	// Forged seals never verify.
	forged := obligation
	forged.DueDay = 1
	if err := forged.Verify(); err == nil {
		t.Fatal("forged obligation verified")
	}
}

func TestTodo_LEGAL_004_Mutation(t *testing.T) {
	base, err := OpenObligation(statusObligation())
	if err != nil {
		t.Fatal(err)
	}
	// Due-rule change recomputes the due date and re-identifies the record.
	extended := statusObligation()
	extended.Due.OffsetDays = 30
	recomputed, err := OpenObligation(extended)
	if err != nil {
		t.Fatal(err)
	}
	if recomputed.DueDay != 130 || recomputed.Digest == base.Digest {
		t.Fatalf("recomputed=%+v", recomputed)
	}
	// Backdated trigger flips the lifecycle to overdue.
	backdated := statusObligation()
	backdated.TriggerDay = 10
	aged, err := OpenObligation(backdated)
	if err != nil {
		t.Fatal(err)
	}
	if overdue := Age(aged, 100); overdue.Status != ObligationOverdue {
		t.Fatalf("backdated stayed %q", overdue.Status)
	}
	// Type change across the vocabulary re-identifies the record.
	retargeted := statusObligation()
	retargeted.Type = ObligationTypeMiniWARN
	warned, err := OpenObligation(retargeted)
	if err != nil {
		t.Fatal(err)
	}
	if warned.Digest == base.Digest {
		t.Fatal("type mutation kept the obligation digest")
	}
}
