package workitem

import (
	"sync"
	"testing"
)

func compartmentPolicy() AccessPolicy {
	return AccessPolicy{
		PolicyID: "pol-1", WorkItemID: "wi-1", Compartment: "medical", Purpose: "leave-evidence-review",
		Grants: map[string]Grant{
			RoleLeaveAdministrator: {ViewFields: []string{"absence-dates", "expected-return", "diagnosis-code"}, Artifacts: []string{"medical-document"}, Download: true, UntilTick: 200},
		},
	}
}

func TestTodo_WORK_008(t *testing.T) {
	policy := compartmentPolicy()
	// LeaveAdministrator receives time-bounded authorized evidence access.
	admin, err := Authorize(policy, RoleLeaveAdministrator, "view-artifact:medical-document", 150)
	if err != nil || !admin.Permitted || admin.Digest == "" {
		t.Fatalf("admin=%+v err=%v", admin, err)
	}
	// The same grant expired refuses.
	if _, err := Authorize(policy, RoleLeaveAdministrator, "view-artifact:medical-document", 201); err == nil {
		t.Fatal("expired grant permitted")
	}
	// Managers receive only permitted operational absence facts.
	for _, field := range []string{"absence-dates", "expected-return"} {
		decision, err := Authorize(policy, RoleManager, "view-field:"+field, 150)
		if err != nil || !decision.Permitted {
			t.Fatalf("manager %s: %+v err=%v", field, decision, err)
		}
	}
	// RED: every restricted surface refuses the manager.
	for _, action := range []string{"view-artifact:medical-document", "view-field:diagnosis-code", "view-field:filename", "view-field:thumbnail", "view-field:ocr-diagnosis", "download"} {
		if _, err := Authorize(policy, RoleManager, action, 150); err == nil {
			t.Fatalf("manager %s permitted", action)
		}
	}
	// Queue counts never leak restricted existence.
	items := []CompartmentItem{
		{ID: "wi-1", RestrictedExistence: true, Fields: map[string]string{"absence-dates": "2026-09-01", "diagnosis-code": "J06"}},
		{ID: "wi-2", Fields: map[string]string{"absence-dates": "2026-09-02"}},
	}
	if VisibleCount(items, RoleManager) != 1 {
		t.Fatal("manager count leaks the restricted task")
	}
	if VisibleCount(items, RoleLeaveAdministrator) != 2 {
		t.Fatal("administrator count hides a task")
	}
	// Search returns only permitted fields per role.
	managerView := SearchFields(items[0], RoleManager, Grant{})
	if len(managerView) != 0 {
		t.Fatalf("restricted search leaks to manager: %v", managerView)
	}
	adminView := SearchFields(items[0], RoleLeaveAdministrator, policy.Grants[RoleLeaveAdministrator])
	if adminView["diagnosis-code"] != "J06" || adminView["absence-dates"] != "2026-09-01" {
		t.Fatalf("admin view = %v", adminView)
	}
}

func TestTodo_WORK_008_Race(t *testing.T) {
	registry := NewPolicyRegistry()
	if err := registry.Register(compartmentPolicy()); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			role := RoleManager
			action := "view-field:absence-dates"
			if i%2 == 0 {
				role = RoleLeaveAdministrator
				action = "view-artifact:medical-document"
			}
			if _, err := registry.Authorize("pol-1", role, action, 150); err != nil {
				errs[i] = err
			}
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
	}
	if len(registry.Log()) != workers {
		t.Fatalf("log = %d decisions, want %d", len(registry.Log()), workers)
	}
	if err := registry.Register(compartmentPolicy()); err == nil {
		t.Fatal("duplicate policy registered")
	}
}

func TestTodo_WORK_008_Integration(t *testing.T) {
	// A derived absence message carrying only operational facts scans
	// clean; one smuggling a diagnosis refuses.
	clean, err := DeriveMessage("Absence Sep 1-3", "Worker is absent Sep 1-3, expected return Sep 4.", map[string]string{"receipt": "ok"}, []string{"J06", "influenza"})
	if err != nil {
		t.Fatalf("DeriveMessage: %v", err)
	}
	if !clean.Clean() {
		t.Fatalf("clean message findings = %v", clean.Findings)
	}
	dirty, err := DeriveMessage("Absence Sep 1-3", "Diagnosis J06 influenza confirmed.", map[string]string{}, []string{"J06", "influenza"})
	if err != nil {
		t.Fatalf("DeriveMessage: %v", err)
	}
	if dirty.Clean() {
		t.Fatal("medical fact smuggled through a derived message")
	}
	// Provider metadata outside purpose refuses the same way.
	meta, err := DeriveMessage("Absence", "Absent.", map[string]string{"ocr": "J06"}, []string{"J06"})
	if err != nil {
		t.Fatalf("DeriveMessage: %v", err)
	}
	if meta.Clean() {
		t.Fatal("provider metadata leaks the compartment")
	}
}

func TestTodo_WORK_008_Security(t *testing.T) {
	policy := compartmentPolicy()
	// Unknown roles and ungranted artifacts refuse with receipts.
	decision, err := Authorize(policy, "stranger", "view-field:absence-dates", 150)
	if err == nil || decision.Permitted || decision.Digest == "" {
		t.Fatalf("stranger=%+v err=%v", decision, err)
	}
	// Assignment authority never implies artifact access: the manager may
	// own the task and still not see the artifact.
	owned := policy
	if _, err := Authorize(owned, RoleManager, "view-artifact:medical-document", 150); err == nil {
		t.Fatal("assignment authority opened the artifact compartment")
	}
	// Receipts are deterministic per access.
	first, _ := Authorize(policy, RoleManager, "view-field:absence-dates", 150)
	second, _ := Authorize(policy, RoleManager, "view-field:absence-dates", 150)
	if first.Digest != second.Digest {
		t.Fatal("identical accesses produced different receipts")
	}
}
