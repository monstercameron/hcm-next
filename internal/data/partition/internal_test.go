package partition

import "testing"

func TestQuoteIdent(t *testing.T) {
	t.Parallel()
	if got := quoteIdent("tenant_isolation"); got != `"tenant_isolation"` {
		t.Fatalf("quoteIdent(plain) = %s", got)
	}
	if got := quoteIdent(`weird"name`); got != `"weird""name"` {
		t.Fatalf("quoteIdent(embedded quote) = %s, want doubled quote escaping", got)
	}
}

func TestFirstLine(t *testing.T) {
	t.Parallel()
	if got := firstLine("  SELECT 1\nFROM x\n"); got != "SELECT 1" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine("  SELECT 1  "); got != "SELECT 1" {
		t.Fatalf("firstLine(no newline) = %q", got)
	}
}

func TestKit_fill(t *testing.T) {
	t.Parallel()
	k := Kit{Table: "ledger_event", ShadowTable: "ledger_event_shadow"}
	got := k.fill("ledger_event", "SELECT * FROM {table} WHERE tenant_id = $1 ORDER BY sequence")
	want := "SELECT * FROM ledger_event WHERE tenant_id = $1 ORDER BY sequence"
	if got != want {
		t.Fatalf("fill() = %q, want %q", got, want)
	}
	got = k.fill(k.ShadowTable, "SELECT * FROM {table}")
	if got != "SELECT * FROM ledger_event_shadow" {
		t.Fatalf("fill(shadow) = %q", got)
	}
}

func TestValidateIdent(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("validateIdent did not panic on a non-identifier")
		}
	}()
	validateIdent("table", "not a valid identifier; DROP TABLE x")
}

func TestValidateIdent_Accepts(t *testing.T) {
	t.Parallel()
	// Must not panic.
	validateIdent("table", "ledger_event_shadow")
	validateIdent("role", "hcmnext_app")
}

func TestPolicySetsEqual(t *testing.T) {
	t.Parallel()
	a := []policySignature{{name: "tenant_isolation", cmd: "ALL", roles: "public", qual: "q", withCheck: "w"}}
	b := []policySignature{{name: "tenant_isolation", cmd: "ALL", roles: "public", qual: "q", withCheck: "w"}}
	if !policySetsEqual(a, b) {
		t.Fatal("identical policy sets compared unequal")
	}
	if policySetsEqual(a, nil) {
		t.Fatal("a non-empty set compared equal to an empty one")
	}
	c := []policySignature{{name: "tenant_isolation", cmd: "ALL", roles: "public", qual: "different", withCheck: "w"}}
	if policySetsEqual(a, c) {
		t.Fatal("policy sets with different qual text compared equal")
	}
}
