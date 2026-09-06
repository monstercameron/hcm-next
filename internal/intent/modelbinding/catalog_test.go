package modelbinding

import "testing"

func TestBindCatalogNoError(t *testing.T) {
	if _, err := BindCatalog(); err != nil {
		t.Fatalf("BindCatalog: %v", err)
	}
}

func TestDigestDeterministic(t *testing.T) {
	t1, err := BindCatalog()
	if err != nil {
		t.Fatalf("BindCatalog 1: %v", err)
	}
	t2, err := BindCatalog()
	if err != nil {
		t.Fatalf("BindCatalog 2: %v", err)
	}
	if t1.Digest() != t2.Digest() {
		t.Fatal("two BindCatalog runs produced different digests")
	}
}

func TestDigestSensitiveToGaps(t *testing.T) {
	a := Table{}
	b := Table{Gaps: []Gap{{Definition: intentRefFor("x", 1), Element: "read_property", Detail: "d"}}}
	if a.Digest() == b.Digest() {
		t.Fatal("Digest did not change when Gaps changed")
	}
}

func TestIntentRefFor(t *testing.T) {
	r := intentRefFor("hcmnext.test.thing", 3)
	if r.TypeID != "hcmnext.test.thing" || r.Version != 3 {
		t.Fatalf("intentRefFor = %+v", r)
	}
	if r.String() != "hcmnext.test.thing/v3" {
		t.Fatalf("String() = %q", r.String())
	}
}
