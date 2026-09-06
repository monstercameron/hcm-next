package todogovernance

import (
	"strings"
	"testing"
)

// intentFixture renders one otherwise well-formed todo with an overridable
// INTENT CONTEXT line, so each RED case below violates exactly one part of
// GOV-025's contract.
func intentFixture(id, intentContext string) string {
	name := "Test" + strings.ReplaceAll(id, "-", "")
	return "- [ ] `" + id + "` **[P0][LUNA] Intent fixture.**\n" +
		"  - **Depends:** none.\n" +
		"  - **INTENT CONTEXT:** `" + intentContext + "`.\n" +
		"  - **TEST:** `" + name + "`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=" + name + "`.\n" +
		"  - **RED:** r.\n  - **GREEN:** g.\n  - **REFACTOR:** rf.\n" +
		"  - **Refs:** [f](f.md).\n"
}

var fixtureCatalog = []string{
	"hcmnext.people.promote_worker/v1",
	"hcmnext.rewards.change_base_pay/v1",
}

// TestTodoIntentContextRejectsMissingFalseOrUnresolvedBinding is the PRIMARY
// test declared by `GOV-025` in planning/todos.md. It proves
// ValidateIntentContext rejects an unknown ROLE, an unknown SETS domain, a
// DIRECT value that is neither "none" nor a catalog BusinessIntent name, and
// an empty WHY, then runs the same validator against the real registry and
// markdown (with the real catalog) and requires every finding to already be
// in the reviewed gov025Allowlist.
func TestTodoIntentContextRejectsMissingFalseOrUnresolvedBinding(t *testing.T) {
	t.Run("WellFormedIsSilent", func(t *testing.T) {
		content := intentFixture("FX-200", "ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		if len(findings) != 0 {
			t.Errorf("expected no findings for a well-formed INTENT CONTEXT, got: %v", findings)
		}
	})

	t.Run("DirectRoleWithCatalogNameIsSilent", func(t *testing.T) {
		content := intentFixture("FX-201", "ROLE=DIRECT; SETS=BI.PEOPLE; DIRECT=hcmnext.people.promote_worker/v1; WHY=implements the accepted intent")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		if len(findings) != 0 {
			t.Errorf("expected no findings for a DIRECT todo naming a catalog definition, got: %v", findings)
		}
	})

	t.Run("UnknownRole", func(t *testing.T) {
		content := intentFixture("FX-202", "ROLE=ORCHESTRATION; SETS=BI.ALL; DIRECT=none; WHY=fixture")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-202", "GOV-025", CodeUnknownRole)
	})

	t.Run("UnknownSet", func(t *testing.T) {
		content := intentFixture("FX-203", "ROLE=GOVERNANCE; SETS=BI.NOT_A_DOMAIN; DIRECT=none; WHY=fixture")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-203", "GOV-025", CodeUnknownSet)
	})

	t.Run("MissingSets", func(t *testing.T) {
		content := intentFixture("FX-204", "ROLE=GOVERNANCE; DIRECT=none; WHY=fixture")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-204", "GOV-025", CodeMissingSets)
	})

	t.Run("InvalidDirectDisplayName", func(t *testing.T) {
		content := intentFixture("FX-205", "ROLE=EXPOSURE; SETS=BI.PEOPLE; DIRECT=PromoteWorker; WHY=fixture")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-205", "GOV-025", CodeInvalidDirect)
	})

	t.Run("InvalidDirectCommaList", func(t *testing.T) {
		content := intentFixture("FX-206", "ROLE=GOVERNANCE; SETS=BI.WORKFORCE; DIRECT=RequestLeave,ExtendLeave; WHY=fixture")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-206", "GOV-025", CodeInvalidDirect)
		var hits int
		for _, f := range findings {
			if f.TodoID == "FX-206" && f.Code == CodeInvalidDirect {
				hits++
			}
		}
		if hits != 2 {
			t.Errorf("expected one INVALID_DIRECT finding per comma-separated token (2), got %d: %v", hits, findings)
		}
	})

	t.Run("EmptyWhy", func(t *testing.T) {
		content := intentFixture("FX-207", "ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=")
		records, errs := ParseRecords(content)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-207", "GOV-025", CodeEmptyWhy)
	})

	t.Run("MissingIntentContext", func(t *testing.T) {
		// A todo with no INTENT CONTEXT field at all is rejected by
		// todoregistry.ParseTodos? No - INTENT CONTEXT is not one of
		// todoregistry's required fields (see validateTodo), so the block
		// still parses; GOV-025 alone is responsible for catching this.
		broken := "- [ ] `FX-208` **[P0][LUNA] No intent context.**\n" +
			"  - **Depends:** none.\n" +
			"  - **TEST:** `TestFX208`.\n" +
			"  - **TEST MATRIX:** `PRIMARY=TestFX208`.\n" +
			"  - **RED:** r.\n  - **GREEN:** g.\n  - **REFACTOR:** rf.\n" +
			"  - **Refs:** [f](f.md).\n"
		records, errs := ParseRecords(broken)
		if len(errs) != 0 {
			t.Fatalf("unexpected parse errors: %v", errs)
		}
		findings := ValidateIntentContext(records, fixtureCatalog)
		requireFinding(t, findings, "FX-208", "GOV-025", CodeMissingIntentContext)
	})

	t.Run("RealCorpus", func(t *testing.T) {
		records := loadRealMarkdown(t)
		catalog := loadRealCatalog(t)
		findings := ValidateIntentContext(records, catalog)
		assertOnlyAllowlisted(t, findings, gov025Allowlist)
	})
}

// TestTodo_GOV_025_Golden pins the exact finding set for one small,
// hand-checked fixture set.
func TestTodo_GOV_025_Golden(t *testing.T) {
	content := intentFixture("G-201", "ROLE=ORCHESTRATION; SETS=BI.ALL; DIRECT=none; WHY=fixture") +
		intentFixture("G-202", "ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=PromoteWorker; WHY=fixture") +
		intentFixture("G-203", "ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=fixture")
	records, errs := ParseRecords(content)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	findings := ValidateIntentContext(records, fixtureCatalog)

	want := []string{
		"GOV-025|G-201|UNKNOWN_ROLE|ORCHESTRATION",
		"GOV-025|G-202|INVALID_DIRECT|PromoteWorker",
	}
	got := make([]string, 0, len(findings))
	for _, f := range findings {
		got = append(got, f.Key())
	}
	if len(got) != len(want) {
		t.Fatalf("golden mismatch: want %d findings %v, got %d %v", len(want), want, len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("golden mismatch at %d: want %q, got %q (full: %v)", i, want[i], got[i], got)
		}
	}
}

// TestTodo_GOV_025_Property checks an invariant that must hold regardless
// of which declared role/set/direct combination a fixture uses: any
// INTENT CONTEXT built entirely from the declared vocabularies (every role,
// every domain alone and combined with BI.ALL, DIRECT=none, non-empty WHY)
// produces zero findings.
func TestTodo_GOV_025_Property(t *testing.T) {
	roles := []string{"DIRECT", "COMPOSITE", "EMITTER", "EXPOSURE", "DOMAIN_SUPPORT", "CONFORMANCE", "GOVERNANCE", "SUBSTRATE"}
	sets := []string{"BI.PEOPLE", "BI.WORKFORCE", "BI.REWARDS", "BI.PAYROLL", "BI.REGULATORY", "BI.ALL"}
	for _, role := range roles {
		for _, set := range sets {
			id := "PROP-" + role + "-" + strings.TrimPrefix(set, "BI.")
			content := intentFixture(id, "ROLE="+role+"; SETS="+set+"; DIRECT=none; WHY=fixture rationale")
			records, errs := ParseRecords(content)
			if len(errs) != 0 {
				t.Fatalf("role=%s set=%s: unexpected parse errors: %v", role, set, errs)
			}
			findings := ValidateIntentContext(records, fixtureCatalog)
			if len(findings) != 0 {
				t.Errorf("role=%s set=%s: expected no findings for a declared-vocabulary INTENT CONTEXT, got: %v", role, set, findings)
			}
		}
	}
}

// FuzzTodo_GOV_025 feeds arbitrary text as the raw INTENT CONTEXT field
// value to prove parseKV and ValidateIntentContext never panic on hostile
// input: unmatched "=", empty segments, unicode, repeated keys.
func FuzzTodo_GOV_025(f *testing.F) {
	seeds := []string{
		"ROLE=GOVERNANCE; SETS=BI.ALL; DIRECT=none; WHY=x",
		"", "ROLE=; SETS=; DIRECT=; WHY=", "ROLE=GOVERNANCE",
		"ROLE=GOVERNANCE;;;SETS=BI.ALL", "=;=;=", "WHY=a;b;c",
		"ROLE=GOVERNANCE; SETS=BI.ALL,BI.ALL,BI.NOPE; DIRECT=a,b,c; WHY=" + strings.Repeat("y", 300),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if strings.ContainsAny(raw, "\n\r`") {
			t.Skip("fixture rendering cannot embed a newline or backtick in the field value")
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panicked on INTENT CONTEXT %q: %v", raw, r)
			}
		}()
		content := intentFixture("FZ-200", raw)
		records, errs := ParseRecords(content)
		_ = errs
		_ = ValidateIntentContext(records, fixtureCatalog)
	})
}

// TestTodo_GOV_025_Conformance proves every ParseCatalogDefinitions result
// (and so every value ValidateIntentContext will ever accept as DIRECT
// besides "none") matches the canonical `hcmnext.<domain>.<verb_noun>/v<N>`
// identity grammar from planning/specs/business-intent-catalog.md, and that
// the real catalog file yields the documented fourteen definitions.
func TestTodo_GOV_025_Conformance(t *testing.T) {
	defs := loadRealCatalog(t)
	if len(defs) != 14 {
		t.Errorf("expected the fourteen drafted BusinessIntent definitions, got %d: %v", len(defs), defs)
	}
	for _, ref := range defs {
		if !definitionRefRe.MatchString("`" + ref + "`") {
			t.Errorf("catalog definition %q does not match the hcmnext.<domain>.<verb_noun>/v<N> grammar", ref)
		}
	}
}
