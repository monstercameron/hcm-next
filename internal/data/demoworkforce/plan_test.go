package demoworkforce

import (
	"regexp"
	"testing"

	"github.com/google/uuid"
)

func TestHarborCarePlanIsCoherentAndPhotoCoverageIsExact(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	employees, err := Plan(tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(employees) != 60 {
		t.Fatalf("workers = %d, want 60", len(employees))
	}
	keys, numbers, positions := map[string]bool{}, map[string]bool{}, map[string]bool{}
	knownManagers := map[string]bool{"board:harborcare": true}
	photoCount := 0
	for _, employee := range employees {
		knownManagers[employee.Row.WorkerKey] = true
	}
	for _, employee := range employees {
		row := employee.Row
		if err := row.Validate(); err != nil {
			t.Errorf("%s: %v", row.WorkerKey, err)
		}
		if row.WorkerType != "employee" || row.LifecycleStatus != "active" || row.PayBasis != "ANNUAL_SALARY" {
			t.Errorf("%s has noncanonical workflow tokens: type=%q lifecycle=%q pay_basis=%q", row.WorkerKey, row.WorkerType, row.LifecycleStatus, row.PayBasis)
		}
		if keys[row.WorkerKey] || numbers[row.WorkerNumber] || positions[row.PositionID] {
			t.Errorf("duplicate identity in %+v", row)
		}
		keys[row.WorkerKey], numbers[row.WorkerNumber], positions[row.PositionID] = true, true, true
		if !knownManagers[employee.ManagerKey] {
			t.Errorf("%s has unknown manager %q", row.WorkerKey, employee.ManagerKey)
		}
		if employee.HasProfilePhoto {
			photoCount++
			if !regexp.MustCompile(`^/workspace/assets/person-hc-\d{3}-small\.jpg$`).MatchString(employee.PhotoProxyRef) || employee.PhotoOriginalRef == "" {
				t.Errorf("%s photo refs = %q %q", row.WorkerKey, employee.PhotoOriginalRef, employee.PhotoProxyRef)
			}
		} else if employee.PhotoProxyRef != "" || employee.PhotoOriginalRef != "" {
			t.Errorf("%s has partial absent-photo metadata", row.WorkerKey)
		}
	}
	if photoCount != 45 || photoCount*4 != len(employees)*3 {
		t.Fatalf("photo coverage = %d/%d, want exactly 75%%", photoCount, len(employees))
	}
}

func TestHarborCareOrganizationHierarchyHasValidParents(t *testing.T) {
	units := map[string]OrganizationUnit{}
	for _, unit := range HarborCare.Units {
		if units[unit.Code].Code != "" {
			t.Fatalf("duplicate organization code %q", unit.Code)
		}
		units[unit.Code] = unit
	}
	for _, unit := range HarborCare.Units {
		if unit.ParentCode != "" {
			if _, ok := units[unit.ParentCode]; !ok {
				t.Errorf("unit %q has missing parent %q", unit.Code, unit.ParentCode)
			}
		}
	}
	if len(units) < 15 {
		t.Fatalf("organization units = %d, want a meaningful inner organization", len(units))
	}
}
