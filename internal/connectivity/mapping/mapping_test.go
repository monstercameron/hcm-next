package mapping_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/connectivity/mapping"
)

func TestTodo_CONN_RT_005(t *testing.T) {
	ir := mapping.IR{Version: "partner/v1", Rules: []mapping.Rule{
		{Source: "name", Target: "person.name", Op: mapping.OpTrim},
		{Source: "department", Target: "department.id", Op: mapping.OpLookup, Lookup: map[string]string{"eng": "ENG-1"}},
		{Source: "missing", Target: "optional", Op: mapping.OpIdentity, Null: mapping.NullOmit},
	}}
	r1, err := mapping.Execute(ir, map[string]string{"name": "  Ada  ", "department": "eng"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := mapping.Execute(ir, map[string]string{"department": "eng", "name": "  Ada  "})
	if err != nil {
		t.Fatal(err)
	}
	if r1.PayloadDigest != r2.PayloadDigest {
		t.Fatalf("digest changed with input map order: %s != %s", r1.PayloadDigest, r2.PayloadDigest)
	}
	if len(r1.Fields) != 2 || r1.Fields[0].Target != "department.id" || r1.Fields[1].Value != "Ada" {
		t.Fatalf("unexpected fields: %#v", r1.Fields)
	}
	if len(r1.Diagnostics) != 1 || r1.Diagnostics[0].Code != "source.missing" {
		t.Fatalf("unexpected diagnostics: %#v", r1.Diagnostics)
	}
}

func TestTodo_CONN_RT_005_Golden(t *testing.T) {
	ir := mapping.IR{Version: "v1", Rules: []mapping.Rule{{Source: "amount", Target: "pay", Op: mapping.OpMoney, Argument: "USD"}}}
	r, err := mapping.Execute(ir, map[string]string{"amount": "USD 1234.50"})
	if err != nil {
		t.Fatal(err)
	}
	if r.PayloadDigest != "sha256:aa66b2458ea8e711ef09c961884a86cc0dbfa5bae4d929824c401aaa28a249d4" {
		t.Fatalf("digest = %s", r.PayloadDigest)
	}
}

func TestTodo_CONN_RT_005_Red(t *testing.T) {
	_, err := mapping.Execute(mapping.IR{Version: "v1", Rules: []mapping.Rule{{Source: "x", Target: "y", Op: mapping.Op("EVAL")}}}, nil)
	if err == nil {
		t.Fatal("arbitrary operation accepted")
	}
	_, err = mapping.Execute(mapping.IR{Version: "v1", Rules: []mapping.Rule{{Source: "x", Target: "y", Op: mapping.OpMoney, Argument: "usd"}}}, map[string]string{"x": "1"})
	if err == nil {
		t.Fatal("untyped currency accepted")
	}
}
