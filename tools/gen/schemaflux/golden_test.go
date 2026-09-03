package schemaflux_test

import (
	"testing"

	sfx "github.com/monstercameron/hcm-next/tools/gen/schemaflux"
)

// goldenDefinition is the frozen expectation TestTodo_TOOL_004_Golden checks
// the compiled catalog against: the fourteen definitions' identity and
// kernel family. A change here must be a deliberate, reviewed change to
// schema/schemaflux/business_intents/v1, not an accidental drift.
type goldenDefinition struct {
	intentTypeID string
	version      uint32
	kernelFamily string
}

var goldenCatalog = []goldenDefinition{
	{"hcmnext.intelligence.explain_transaction", 1, "ANALYTICAL_REQUEST"},
	{"hcmnext.operations.create_repair_plan", 1, "ANALYTICAL_REQUEST"},
	{"hcmnext.operations.detect_drift", 1, "ANALYTICAL_REQUEST"},
	{"hcmnext.operations.simulate_repair", 1, "CALCULATION_REQUEST"},
	{"hcmnext.people.change_manager", 1, "CHANGE_REQUEST"},
	{"hcmnext.people.explain_worker_state", 1, "ANALYTICAL_REQUEST"},
	{"hcmnext.people.promote_worker", 1, "CHANGE_REQUEST"},
	{"hcmnext.rewards.change_base_pay", 1, "CHANGE_REQUEST"},
	{"hcmnext.rewards.evaluate_pay_band_position", 1, "CALCULATION_REQUEST"},
	{"hcmnext.rewards.release_compensation_budget", 1, "CHANGE_REQUEST"},
	{"hcmnext.rewards.reserve_compensation_budget", 1, "CHANGE_REQUEST"},
	{"hcmnext.rewards.simulate_compensation", 1, "CALCULATION_REQUEST"},
	{"hcmnext.work.approve_proposal", 1, "CHANGE_REQUEST"},
	{"hcmnext.work.reject_proposal", 1, "CHANGE_REQUEST"},
}

// TestTodo_TOOL_004_Golden is TOOL-004's golden test: it fails if the source
// YAML under schema/schemaflux/business_intents/v1 gains, loses or
// reclassifies a definition without a deliberate update to this file.
func TestTodo_TOOL_004_Golden(t *testing.T) {
	catalog := loadValidCatalog(t)

	if len(catalog.Definitions) != len(goldenCatalog) {
		t.Fatalf("compiled %d definitions, golden expects %d", len(catalog.Definitions), len(goldenCatalog))
	}

	// catalog.Definitions is sorted by intent_type_id (Compile's contract);
	// keep the golden table sorted the same way so this loop is a direct
	// positional comparison rather than a lookup.
	for i, want := range goldenCatalog {
		got := catalog.Definitions[i]
		if got.IntentTypeID != want.intentTypeID {
			t.Errorf("index %d: intent_type_id = %q, want %q", i, got.IntentTypeID, want.intentTypeID)
			continue
		}
		if got.Version != want.version {
			t.Errorf("%s: version = %d, want %d", want.intentTypeID, got.Version, want.version)
		}
		if got.KernelFamily != want.kernelFamily {
			t.Errorf("%s: kernel_family = %s, want %s", want.intentTypeID, got.KernelFamily, want.kernelFamily)
		}
	}
}

// TestTodo_TOOL_004_Conformance cross-checks the generated catalog against
// internal/intent/definitions, the hand-authored compiled-in P1A registry
// (TOOL-002/TOOL-003, frozen and out of this package's lane). Any mismatch —
// a definition present in only one source, or a disagreeing family or
// version — is a real drift between the two sources of truth and must be
// reported, not silently tolerated.
func TestTodo_TOOL_004_Conformance(t *testing.T) {
	catalog := loadValidCatalog(t)

	mismatches, err := sfx.CrossCheckCompiled(catalog)
	if err != nil {
		t.Fatalf("CrossCheckCompiled: %v", err)
	}
	for _, m := range mismatches {
		t.Errorf("cross-check mismatch: %s", m)
	}
	if len(mismatches) == 0 {
		t.Logf("generated catalog agrees with internal/intent/definitions on all %d definitions (name, family, version)", len(catalog.Definitions))
	}
}
