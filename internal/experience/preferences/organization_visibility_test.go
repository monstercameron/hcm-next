package preferences

import (
	"errors"
	"testing"
)

func TestNormalizeOrganizationVisibilityIsClosedAndDeduplicated(t *testing.T) {
	value := NormalizeOrganizationVisibility(OrganizationVisibility{Mode: " allowlist ", OrganizationUnits: []string{" Finance ", "finance", "", "People"}})
	if value.Mode != OrganizationVisibilityAllowlist || len(value.OrganizationUnits) != 2 || value.OrganizationUnits[0] != "Finance" || value.OrganizationUnits[1] != "People" {
		t.Fatalf("normalized policy = %+v", value)
	}
	if got := NormalizeOrganizationVisibility(OrganizationVisibility{Mode: "unrecognized"}); got.Mode != OrganizationVisibilityAll {
		t.Fatalf("unknown mode = %q, want fail-safe ALL default", got.Mode)
	}
}

func TestValidateOrganizationVisibilityRejectsUnknownAndMissingModes(t *testing.T) {
	for _, mode := range []string{"", "unrecognized"} {
		if err := ValidateOrganizationVisibility(OrganizationVisibility{Mode: mode}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mode %q validation error = %v, want ErrInvalid", mode, err)
		}
	}
	if err := ValidateOrganizationVisibility(OrganizationVisibility{Mode: " own_unit "}); err != nil {
		t.Fatalf("known normalized mode validation error = %v", err)
	}
}
