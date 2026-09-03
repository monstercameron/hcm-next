package celqual

import "testing"

func contract() Contract {
	return Contract{MaxNodes: 32, MaxCost: 48, AllowedVariables: map[string]struct{}{"employee": {}, "amount": {}, "approved": {}}}
}
func TestCELBackendQualification(t *testing.T) {
	if p, err := contract().Validate(" approved && amount > 0 "); err != nil || !p.Deterministic() {
		t.Fatalf("valid expression: %#v, %v", p, err)
	}
	for _, e := range []string{"now() > 0", "random() == true", "reflect(x)", "http(\"x\")", "employee[0]", "{ employee }"} {
		if _, err := contract().Validate(e); err == nil {
			t.Errorf("accepted forbidden expression %q", e)
		}
	}
}
func TestTodo_LIB_005_Golden(t *testing.T) {
	p, err := contract().Validate("approved && amount > 0")
	if err != nil {
		t.Fatal(err)
	}
	if p.Canonical != "approved && amount > 0" || p.Nodes != 3 || p.Cost != 4 {
		t.Fatalf("program = %#v", p)
	}
	if p.Digest != "b7cea908474cc5ead65379d690cd6d3d815f15a478cf6059015cce662bdecf2f" {
		t.Fatalf("digest drift: %s", p.Digest)
	}
}
func TestTodo_LIB_005_Fault(t *testing.T) {
	for _, c := range []Contract{{}, {MaxNodes: 1, MaxCost: 1}} {
		if _, err := c.Validate("approved"); err == nil {
			t.Error("invalid limits accepted")
		}
	}
	if _, err := contract().Validate("unknown == true"); err == nil {
		t.Error("undeclared variable accepted")
	}
}
func TestTodo_LIB_005_Conformance(t *testing.T) {
	for _, e := range []string{"approved", "amount > 0", "employee == employee"} {
		if _, err := contract().Validate(e); err != nil {
			t.Errorf("%q: %v", e, err)
		}
	}
}
func TestTodo_LIB_005_Integration(t *testing.T) {
	a, _ := contract().Validate("approved && amount > 0")
	b, _ := contract().Validate("  approved   &&   amount > 0 ")
	if a != b {
		t.Fatalf("whitespace changed program: %#v != %#v", a, b)
	}
}
func FuzzTodo_LIB_005(f *testing.F) {
	for _, s := range []string{"approved", "amount > 0", "now()", "unknown"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, err := contract().Validate(s)
		if err == nil && !p.Deterministic() {
			t.Fatalf("non-deterministic program: %#v", p)
		}
	})
}
