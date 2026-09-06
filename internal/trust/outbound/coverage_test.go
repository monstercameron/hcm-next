package outbound

import (
	"errors"
	"strings"
	"testing"
)

func TestNewPolicy_RejectsMalformedAndCopiesDestinations(t *testing.T) {
	valid := func() Destination {
		return Destination{Name: "partner", TrustBundleRef: "bundle:v1", Purposes: []string{"export"}, DataClasses: []string{"PII"}}
	}
	for _, tc := range []struct {
		name string
		dest Destination
	}{
		{"missing name", Destination{TrustBundleRef: "bundle", Purposes: []string{"p"}, DataClasses: []string{"c"}}},
		{"padded name", Destination{Name: " partner", TrustBundleRef: "bundle", Purposes: []string{"p"}, DataClasses: []string{"c"}}},
		{"missing bundle", Destination{Name: "d", Purposes: []string{"p"}, DataClasses: []string{"c"}}},
		{"empty purpose list", Destination{Name: "d", TrustBundleRef: "bundle", DataClasses: []string{"c"}}},
		{"empty class list", Destination{Name: "d", TrustBundleRef: "bundle", Purposes: []string{"p"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewPolicy(tc.dest); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("NewPolicy error = %v, want ErrInvalidPolicy", err)
			}
		})
	}
	d := valid()
	p, err := NewPolicy(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Purposes[0], d.DataClasses[0] = "changed", "changed"
	decision, err := p.Check(CheckRequest{Destination: "partner", Purpose: "export", DataClass: "PII"})
	if err != nil || !decision.Allowed || decision.TrustBundleRef != "bundle:v1" {
		t.Fatalf("copied destination no longer allows original request: decision=%+v err=%v", decision, err)
	}
	duplicate := valid()
	if _, err := NewPolicy(duplicate, duplicate); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("duplicate policy error = %v, want ErrInvalidPolicy", err)
	}
}

func TestPolicy_CheckMalformedAndExplain(t *testing.T) {
	var nilPolicy *Policy
	if _, err := nilPolicy.Check(CheckRequest{Destination: "d", Purpose: "p", DataClass: "c"}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("nil policy error = %v, want ErrInvalidPolicy", err)
	}
	p, err := NewPolicy(Destination{Name: "d", TrustBundleRef: "bundle", Purposes: []string{"p"}, DataClasses: []string{"c"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []CheckRequest{{Purpose: "p", DataClass: "c"}, {Destination: "d", DataClass: "c"}, {Destination: "d", Purpose: "p"}, {Destination: " ", Purpose: "p", DataClass: "c"}} {
		if _, err := p.Check(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("Check(%+v) error = %v, want ErrInvalidRequest", req, err)
		}
	}
	dec := Decision{Destination: "d", Purpose: "p", DataClass: "c", Allowed: true}
	for _, want := range []string{"d", "p", "c", "allowed=true"} {
		if !strings.Contains(dec.Explain(), want) {
			t.Fatalf("Explain() = %q, missing %q", dec.Explain(), want)
		}
	}
}
