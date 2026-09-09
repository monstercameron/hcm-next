package authzsim

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

func TestDoc_Smoke(t *testing.T) {
	if got := DiffDecisions(authz.Decision{SubjectDisclosable: false}, authz.Decision{SubjectDisclosable: false}); got.Explanation != "subject_disclosable: false -> false" {
		t.Fatalf("documented coarse diff = %#v", got)
	}
}

func TestDoc_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	if got := boolText(false); got != "false" {
		t.Fatalf("bool rendering = %q", got)
	}
}
