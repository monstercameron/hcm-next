package config_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
)

func mustEntry(t *testing.T, key string, kind config.ValueKind, value string, explicit bool, semantic config.SemanticClass, refs config.Refs) config.Entry {
	t.Helper()
	e, err := config.NewEntry(key, kind, value, explicit, semantic, refs)
	if err != nil {
		t.Fatalf("NewEntry(%q): %v", key, err)
	}
	return e
}

func mustSecretEntry(t *testing.T, key, fingerprint string, explicit bool, semantic config.SemanticClass) config.Entry {
	t.Helper()
	e, err := config.NewSecretEntry(key, fingerprint, explicit, semantic, config.Refs{})
	if err != nil {
		t.Fatalf("NewSecretEntry(%q): %v", key, err)
	}
	return e
}

func mustSnapshot(t *testing.T, name, version string, entries []config.Entry) config.Snapshot {
	t.Helper()
	s, err := config.NewSnapshot(name, version, entries)
	if err != nil {
		t.Fatalf("NewSnapshot(%s): %v", name, err)
	}
	return s
}

func findChange(changes []config.Change, key string) (config.Change, bool) {
	for _, c := range changes {
		if c.Key == key {
			return c, true
		}
	}
	return config.Change{}, false
}

// TestTodo_CONFIG_001 proves the RED and GREEN clauses for the semantic
// configuration diff: a reordered but otherwise equivalent snapshot never
// reports a change, a change to a workflow/schema/mapping/policy-classed
// key is never hidden as compatible, added/removed/changed sets are stable
// and correctly classified, referenced capability/workflow/tenant impacts
// are surfaced, and repeated diffing of the same input is stable.
func TestTodo_CONFIG_001(t *testing.T) {
	t.Run("RED_reordered_equivalent_snapshot_reports_no_change", func(t *testing.T) {
		e1 := mustEntry(t, "a.one", config.KindString, "1", true, config.SemanticGeneric, config.Refs{})
		e2 := mustEntry(t, "a.two", config.KindInt, "2", false, config.SemanticGeneric, config.Refs{})
		e3 := mustEntry(t, "a.three", config.KindBool, "true", true, config.SemanticGeneric, config.Refs{})

		forward := mustSnapshot(t, "env", "v1", []config.Entry{e1, e2, e3})
		reversed := mustSnapshot(t, "env", "v1", []config.Entry{e3, e2, e1})

		result := config.Diff(forward, reversed)
		if len(result.Added) != 0 || len(result.Removed) != 0 || len(result.Changed) != 0 {
			t.Fatalf("reordered-only snapshots reported a change: %+v", result)
		}
		if result.Digest == "" {
			t.Fatalf("digest must not be empty")
		}
	})

	t.Run("RED_semantic_workflow_change_is_never_hidden", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustEntry(t, "wf.onboarding", config.KindString, "workflow://onboarding/v1", true, config.SemanticWorkflow, config.Refs{}),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustEntry(t, "wf.onboarding", config.KindString, "workflow://onboarding/v2", true, config.SemanticWorkflow, config.Refs{}),
		})

		result := config.Diff(before, after)
		c, ok := findChange(result.Changed, "wf.onboarding")
		if !ok {
			t.Fatalf("workflow-classed value change was hidden: %+v", result)
		}
		if c.Compatibility != config.CompatibilityBreaking {
			t.Fatalf("workflow-classed change classified %s, want BREAKING", c.Compatibility)
		}
	})

	t.Run("GREEN_generic_value_change_is_compatible", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustEntry(t, "log.level", config.KindString, "info", true, config.SemanticGeneric, config.Refs{}),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustEntry(t, "log.level", config.KindString, "debug", true, config.SemanticGeneric, config.Refs{}),
		})

		result := config.Diff(before, after)
		c, ok := findChange(result.Changed, "log.level")
		if !ok {
			t.Fatalf("expected a change for log.level")
		}
		if c.Compatibility != config.CompatibilityCompatible {
			t.Fatalf("generic value change classified %s, want COMPATIBLE", c.Compatibility)
		}
		if c.BeforeValue != "info" || c.AfterValue != "debug" {
			t.Fatalf("before/after values = %q/%q", c.BeforeValue, c.AfterValue)
		}
	})

	t.Run("GREEN_type_change_is_always_breaking", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustEntry(t, "retry.count", config.KindInt, "3", true, config.SemanticGeneric, config.Refs{}),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustEntry(t, "retry.count", config.KindString, "3", true, config.SemanticGeneric, config.Refs{}),
		})

		result := config.Diff(before, after)
		c, ok := findChange(result.Changed, "retry.count")
		if !ok {
			t.Fatalf("expected a change for retry.count")
		}
		if c.Compatibility != config.CompatibilityBreaking {
			t.Fatalf("type change classified %s, want BREAKING", c.Compatibility)
		}
	})

	t.Run("GREEN_ref_list_change_is_breaking_even_for_generic_key", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustEntry(t, "feature.flag", config.KindBool, "true", true, config.SemanticGeneric,
				config.Refs{Workflows: []string{"workflow:onboarding"}}),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustEntry(t, "feature.flag", config.KindBool, "true", true, config.SemanticGeneric,
				config.Refs{Workflows: []string{"workflow:onboarding", "workflow:offboarding"}}),
		})

		result := config.Diff(before, after)
		c, ok := findChange(result.Changed, "feature.flag")
		if !ok {
			t.Fatalf("expected a change for feature.flag (ref list changed)")
		}
		if c.Compatibility != config.CompatibilityBreaking {
			t.Fatalf("ref-list-only change classified %s, want BREAKING", c.Compatibility)
		}
		if len(c.ImpactedWorkflows) != 2 {
			t.Fatalf("impacted workflows = %v, want the union of both sides", c.ImpactedWorkflows)
		}
	})

	t.Run("GREEN_added_and_removed_classification", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustEntry(t, "explicit.override", config.KindString, "x", true, config.SemanticGeneric, config.Refs{}),
			mustEntry(t, "defaulted.value", config.KindString, "y", false, config.SemanticGeneric, config.Refs{}),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustEntry(t, "defaulted.value", config.KindString, "y", false, config.SemanticGeneric, config.Refs{}),
			mustEntry(t, "new.generic", config.KindString, "z", false, config.SemanticGeneric, config.Refs{}),
			mustEntry(t, "new.workflow", config.KindString, "workflow://x", true, config.SemanticWorkflow, config.Refs{}),
		})

		result := config.Diff(before, after)

		removed, ok := findChange(result.Removed, "explicit.override")
		if !ok || removed.Compatibility != config.CompatibilityBreaking {
			t.Fatalf("removing an explicit override = %+v, want present and BREAKING", removed)
		}

		added, ok := findChange(result.Added, "new.generic")
		if !ok || added.Compatibility != config.CompatibilityCompatible {
			t.Fatalf("adding a generic key = %+v, want present and COMPATIBLE", added)
		}

		addedWorkflow, ok := findChange(result.Added, "new.workflow")
		if !ok || addedWorkflow.Compatibility != config.CompatibilityBreaking {
			t.Fatalf("adding a workflow-classed key = %+v, want present and BREAKING", addedWorkflow)
		}

		if _, ok := findChange(result.Changed, "defaulted.value"); ok {
			t.Fatalf("unchanged key defaulted.value reported as changed")
		}
	})

	t.Run("GREEN_digest_stable_across_repeated_diff_calls", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustEntry(t, "a", config.KindString, "1", true, config.SemanticGeneric, config.Refs{}),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustEntry(t, "a", config.KindString, "2", true, config.SemanticGeneric, config.Refs{}),
		})

		first := config.Diff(before, after)
		second := config.Diff(before, after)
		if first.Digest != second.Digest {
			t.Fatalf("digest not stable across calls: %s vs %s", first.Digest, second.Digest)
		}
	})

	t.Run("GREEN_secret_entries_compare_by_fingerprint_only", func(t *testing.T) {
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustSecretEntry(t, "db.password", "fingerprint-aaa", true, config.SemanticGeneric),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustSecretEntry(t, "db.password", "fingerprint-bbb", true, config.SemanticGeneric),
		})

		result := config.Diff(before, after)
		c, ok := findChange(result.Changed, "db.password")
		if !ok {
			t.Fatalf("expected a change for the rotated secret")
		}
		if !c.SecretFingerprintChanged {
			t.Fatalf("SecretFingerprintChanged = false, want true")
		}
		if c.BeforeValue != "" || c.AfterValue != "" {
			t.Fatalf("secret change leaked a value: before=%q after=%q", c.BeforeValue, c.AfterValue)
		}
	})
}

// TestTodo_CONFIG_001_Golden proves stable digest output for a fixed pair
// of snapshots.
func TestTodo_CONFIG_001_Golden(t *testing.T) {
	before := mustSnapshot(t, "prod", "v41", []config.Entry{
		mustEntry(t, "audit.retention_days", config.KindInt, "365", true, config.SemanticGeneric, config.Refs{}),
		mustEntry(t, "wf.termination", config.KindString, "workflow://termination/v3", true, config.SemanticWorkflow,
			config.Refs{Workflows: []string{"workflow:termination"}}),
		mustSecretEntry(t, "smtp.password", "fp-2026-06-01", true, config.SemanticGeneric),
	})
	after := mustSnapshot(t, "prod", "v42", []config.Entry{
		mustEntry(t, "audit.retention_days", config.KindInt, "400", true, config.SemanticGeneric, config.Refs{}),
		mustEntry(t, "wf.termination", config.KindString, "workflow://termination/v4", true, config.SemanticWorkflow,
			config.Refs{Workflows: []string{"workflow:termination"}}),
		mustSecretEntry(t, "smtp.password", "fp-2026-06-01", true, config.SemanticGeneric),
		mustEntry(t, "wf.rehire", config.KindString, "workflow://rehire/v1", true, config.SemanticWorkflow, config.Refs{}),
	})

	result := config.Diff(before, after)

	if len(result.Added) != 1 || len(result.Removed) != 0 || len(result.Changed) != 2 {
		t.Fatalf("shape = added=%d removed=%d changed=%d, want 1/0/2", len(result.Added), len(result.Removed), len(result.Changed))
	}

	const wantDigest = "ceaf657bffc7348f28202563938f05619413778377763c0618f3641b7952098e"
	if result.Digest != wantDigest {
		t.Fatalf("digest = %s, want %s", result.Digest, wantDigest)
	}

	repeat := config.Diff(before, after)
	if repeat.Digest != result.Digest {
		t.Fatalf("digest changed across identical calls: %s vs %s", repeat.Digest, result.Digest)
	}
}

// TestTodo_CONFIG_001_Security proves that a secret can never reach a Diff
// output by value: neither through the normal constructor path, nor through
// a caller building an Entry struct literal directly and handing it to
// NewSnapshot.
func TestTodo_CONFIG_001_Security(t *testing.T) {
	t.Run("NewSecretEntry_rejects_a_smuggled_value", func(t *testing.T) {
		e, err := config.NewSecretEntry("api.key", "fp-1", true, config.SemanticGeneric, config.Refs{})
		if err != nil {
			t.Fatalf("NewSecretEntry: %v", err)
		}
		e.Value = "sk_live_should_never_appear"
		if _, err := config.NewSnapshot("env", "v1", []config.Entry{e}); err == nil {
			t.Fatalf("NewSnapshot accepted a secret entry smuggling a literal value")
		} else if !errors.Is(err, config.ErrSecretValueLeak) {
			t.Fatalf("error = %v, want ErrSecretValueLeak", err)
		}
	})

	t.Run("diff_output_never_contains_the_secret_value", func(t *testing.T) {
		const secretPlaintext = "correct-horse-battery-staple"
		before := mustSnapshot(t, "env", "v1", []config.Entry{
			mustSecretEntry(t, "vault.token", "fingerprint-of-"+secretPlaintext, true, config.SemanticGeneric),
		})
		after := mustSnapshot(t, "env", "v2", []config.Entry{
			mustSecretEntry(t, "vault.token", "fingerprint-of-rotated", true, config.SemanticGeneric),
		})

		result := config.Diff(before, after)
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if strings.Contains(string(encoded), secretPlaintext) {
			t.Fatalf("diff result leaked secret material: %s", encoded)
		}
	})
}
