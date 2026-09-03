package authzsim_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/operations/authzsim"
	"github.com/monstercameron/hcm-next/internal/trust/authz"
)

// TestTodo_ADMIN_003_Mutation proves two boundary properties: perturbing
// only the policyVersion argument changes exactly PolicyVersion,
// PolicyVersionMatch and Digest and nothing about the evaluated Decision or
// Explanation (policyVersion is a comparison label, never an evaluation
// input); and the simulator never mutates policy state - interleaving
// Simulate calls under many different, even nonsensical, policyVersion
// arguments never changes what the same request evaluates to, proving
// nothing about evaluation is cached or perturbed by a prior call.
func TestTodo_ADMIN_003_Mutation(t *testing.T) {
	req := managerRequest(t)

	t.Run("perturbing policyVersion changes only the comparison fields", func(t *testing.T) {
		a, err := authzsim.Simulate(req, authz.PolicyVersion)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		b, err := authzsim.Simulate(req, "a-completely-different-version-string")
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}

		if a.Decision.InputsDigest != b.Decision.InputsDigest {
			t.Fatalf("Decision.InputsDigest changed with policyVersion: %s vs %s", a.Decision.InputsDigest, b.Decision.InputsDigest)
		}
		if a.Explanation != b.Explanation {
			t.Fatalf("Explanation changed with policyVersion: %q vs %q", a.Explanation, b.Explanation)
		}
		if a.PolicyVersionMatch == b.PolicyVersionMatch {
			t.Fatal("PolicyVersionMatch did not flip between a matching and a non-matching policyVersion")
		}
		if a.Digest == b.Digest {
			t.Fatal("Digest did not change despite PolicyVersion/PolicyVersionMatch changing")
		}
	})

	t.Run("the simulator never mutates policy state across interleaved calls", func(t *testing.T) {
		baseline, err := authzsim.Simulate(req, authz.PolicyVersion)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}

		noise := []string{"", "v0", "authz.p1a.bootstrap.v1", "!!!not-a-version!!!", authz.PolicyVersion}
		for _, v := range noise {
			if _, err := authzsim.Simulate(req, v); err != nil {
				t.Fatalf("Simulate(%q): %v", v, err)
			}
		}

		again, err := authzsim.Simulate(req, authz.PolicyVersion)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if again.Decision.InputsDigest != baseline.Decision.InputsDigest {
			t.Fatal("evaluating the same request after interleaved Simulate calls under other policy versions produced a different decision")
		}

		// authz.Simulate/Enforce called directly, bypassing this package
		// entirely, must still agree: nothing this package did leaked into
		// authz's own evaluator.
		direct, err := authz.Simulate(req)
		if err != nil {
			t.Fatalf("authz.Simulate: %v", err)
		}
		if direct.InputsDigest != baseline.Decision.InputsDigest {
			t.Fatal("authz.Simulate called directly disagrees with authzsim.Simulate after this package's prior calls")
		}
	})

	t.Run("DiffDecisions of a decision against itself reports no changes", func(t *testing.T) {
		dec, err := authz.Simulate(req)
		if err != nil {
			t.Fatalf("authz.Simulate: %v", err)
		}
		diff := authzsim.DiffDecisions(dec, dec)
		if diff.SubjectDisclosableChanged {
			t.Error("SubjectDisclosableChanged = true comparing a decision against itself")
		}
		for _, d := range diff.FieldDeltas {
			if d.Changed {
				t.Errorf("field %s reported changed comparing a decision against itself", d.Field)
			}
		}
	})
}
