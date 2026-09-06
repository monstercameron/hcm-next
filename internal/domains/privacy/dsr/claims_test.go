package dsr

import "testing"

func TestSubjectClaims_ValidateRequiresAtLeastOneClaim(t *testing.T) {
	if err := (SubjectClaims{}).Validate(); err == nil {
		t.Fatal("empty SubjectClaims validated, want ErrClaimsInvalid")
	}
	if err := (SubjectClaims{Email: "a@b.com"}).Validate(); err != nil {
		t.Fatalf("SubjectClaims with only an email failed to validate: %v", err)
	}
	if err := (SubjectClaims{FullName: "  "}).Validate(); err == nil {
		t.Fatal("SubjectClaims with only whitespace validated, want ErrClaimsInvalid")
	}
}

func TestSubjectClaims_KeyPreferenceOrder(t *testing.T) {
	// ExternalRef wins over everything else.
	c := SubjectClaims{ExternalRef: "person:1", Email: "a@b.com", Phone: "555-0100", FullName: "A B"}
	if got := c.Key(); got != "person:1" {
		t.Errorf("Key() = %q, want ExternalRef to win", got)
	}

	// Email wins over phone/name when ExternalRef is absent.
	c = SubjectClaims{Email: "  A@B.com  ", Phone: "555-0100", FullName: "A B"}
	if got := c.Key(); got != "a@b.com" {
		t.Errorf("Key() = %q, want normalized email %q", got, "a@b.com")
	}

	// Falls through to name when nothing else is present.
	c = SubjectClaims{FullName: "  Alex Example  "}
	if got := c.Key(); got != "alex example" {
		t.Errorf("Key() = %q, want normalized name %q", got, "alex example")
	}

	// Nothing claimed: empty key.
	if got := (SubjectClaims{}).Key(); got != "" {
		t.Errorf("Key() on empty claims = %q, want empty", got)
	}
}

func TestSubjectClaims_CanonicalBytesChangesPerField(t *testing.T) {
	base := SubjectClaims{FullName: "A", Email: "a@b.com"}
	baseDigest := digestHex(base.canonicalBytes(nil))

	withRep := base
	withRep.RepresentativeRef = "rep-1"
	if digestHex(withRep.canonicalBytes(nil)) == baseDigest {
		t.Error("adding a RepresentativeRef did not change the canonical encoding")
	}
}
