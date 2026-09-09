package quarantine_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

func TestStateValid(t *testing.T) {
	for _, s := range []quarantine.State{quarantine.Quarantined, quarantine.Admitted, quarantine.Rejected} {
		if !s.Valid() {
			t.Errorf("State %q should be valid", s)
		}
	}
	if quarantine.State("SAFE").Valid() {
		t.Error("an undeclared state must not be valid")
	}
}

func TestContentTypeExpectedCategory(t *testing.T) {
	cases := map[quarantine.ContentType]quarantine.Category{
		quarantine.ContentPDF:  quarantine.CategoryPDF,
		quarantine.ContentPNG:  quarantine.CategoryPNG,
		quarantine.ContentJPEG: quarantine.CategoryJPEG,
		quarantine.ContentDOCX: quarantine.CategoryZIP,
		quarantine.ContentCSV:  quarantine.CategoryText,
		quarantine.ContentTXT:  quarantine.CategoryText,
	}
	for ct, want := range cases {
		if !ct.Valid() {
			t.Errorf("%q should be a valid declared content type", ct)
		}
		got, ok := ct.ExpectedCategory()
		if !ok || got != want {
			t.Errorf("ExpectedCategory(%q) = %q, %v; want %q, true", ct, got, ok, want)
		}
	}
	if quarantine.ContentType("exe").Valid() {
		t.Error("an undeclared content type must not be valid")
	}
}

func TestPolicyValidate(t *testing.T) {
	base := quarantine.Policy{
		MaxContentBytes:     1024,
		AllowedContentTypes: []quarantine.ContentType{quarantine.ContentPDF},
		ScannerID:           "clamav",
		ScannerVersion:      "1.2.3",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("well-formed policy rejected: %v", err)
	}

	cases := []struct {
		name string
		fn   func(quarantine.Policy) quarantine.Policy
	}{
		{"zero max bytes", func(p quarantine.Policy) quarantine.Policy { p.MaxContentBytes = 0; return p }},
		{"negative max bytes", func(p quarantine.Policy) quarantine.Policy { p.MaxContentBytes = -1; return p }},
		{"no allowed types", func(p quarantine.Policy) quarantine.Policy { p.AllowedContentTypes = nil; return p }},
		{"unrecognized allowed type", func(p quarantine.Policy) quarantine.Policy {
			p.AllowedContentTypes = []quarantine.ContentType{"exe"}
			return p
		}},
		{"no scanner id", func(p quarantine.Policy) quarantine.Policy { p.ScannerID = ""; return p }},
		{"no scanner version", func(p quarantine.Policy) quarantine.Policy { p.ScannerVersion = ""; return p }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.fn(base)
			err := p.Validate()
			if err == nil {
				t.Fatalf("%s: expected a validation error", c.name)
			}
			var invalid quarantine.ErrRequestInvalid
			if got := errorsAsRequestInvalid(err, &invalid); !got {
				t.Fatalf("%s: error %v is not ErrRequestInvalid", c.name, err)
			}
			if invalid.Code() != quarantine.CodeRequestInvalid {
				t.Fatalf("%s: code %s, want %s", c.name, invalid.Code(), quarantine.CodeRequestInvalid)
			}
		})
	}
}

// errorsAsRequestInvalid avoids importing "errors" into every test file that
// only needs this one assertion.
func errorsAsRequestInvalid(err error, target *quarantine.ErrRequestInvalid) bool {
	e, ok := err.(quarantine.ErrRequestInvalid)
	if ok {
		*target = e
	}
	return ok
}
