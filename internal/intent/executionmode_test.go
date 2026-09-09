package intent_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// mustContract resolves one mode contract.
func mustContract(t *testing.T, mode intent.Mode, env intent.Environment) intent.ModeContract {
	t.Helper()
	c, err := intent.ModeContractFor(mode, env)
	if err != nil {
		t.Fatalf("contract for %s/%s: %v", mode, env, err)
	}
	return c
}

// executableDefinition is a definition whose declared profile and effect class
// permit a real commit, so that the mode contract is the only thing standing
// between an attempt and an effect. Every checked-in P1A definition is
// zero-effect, which is the right answer for the release and the wrong fixture
// for testing the mode axis on its own.
func executableDefinition() intent.Definition {
	return intent.Definition{
		Ref:          intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1},
		DisplayName:  "PromoteWorker",
		Description:  "A definition scheduled for the bounded write release.",
		OwnerDomain:  "PEOPLE",
		Family:       intent.FamilyChangeRequest,
		Maturity:     intent.MaturityDraftContract,
		SideEffect:   intent.SideEffectIrreversibleExternalMutation,
		EffectClass:  intent.EffectClassIrreversibleExternal,
		Release:      intent.ReleaseP1B,
		InputSchema:  schemaOf("hcmnext.people.v1.PromoteWorkerRequest"),
		ResultSchema: schemaOf("hcmnext.people.v1.PromoteWorkerResult"),
		PhaseDepth:   "GATE_B_IMPLEMENT",
		AllowedInitiators: []intent.Initiator{
			intent.InitiatorHuman, intent.InitiatorService,
		},
		AllowedModes: []intent.Mode{
			intent.ModeSimulate, intent.ModeShadow, intent.ModeReplay,
			intent.ModeExecute, intent.ModeRepair,
		},
		SubjectKinds:   []string{"EMPLOYMENT"},
		CorrectionRule: "people.promotion_correction/v1",
	}
}

// TestIntentExecutionModesCannotEscalateEffects is the PRIMARY test for
// INTENT-023.
//
// RED: no simulation, shadow, replay, conformance or test-environment run may
// commit domain truth, cause an external effect or consume a live approval, and
// a replay may not be told apart from a new action by its request alone.
//
// GREEN: mode and environment bind capability permissions, adapters, clock,
// evidence namespace and the zero-effect guarantee; execute requires a
// separately authorized current intent; and mode comparisons report
// deterministic semantic differences.
func TestIntentExecutionModesCannotEscalateEffects(t *testing.T) {
	def := executableDefinition()
	if err := def.Validate(); err != nil {
		t.Fatalf("the fixture definition is invalid: %v", err)
	}

	t.Run("RED: only production EXECUTE and REPAIR reach the real world", func(t *testing.T) {
		for _, mode := range intent.Modes() {
			for _, env := range intent.Environments() {
				c := mustContract(t, mode, env)
				wantCommit := (mode == intent.ModeExecute && (env == intent.EnvironmentProduction || env == intent.EnvironmentSandbox)) ||
					(mode == intent.ModeRepair && env == intent.EnvironmentProduction)
				wantEffect := env == intent.EnvironmentProduction &&
					(mode == intent.ModeExecute || mode == intent.ModeRepair)
				wantApproval := env == intent.EnvironmentProduction && mode == intent.ModeExecute

				if c.AllowsDomainCommit != wantCommit {
					t.Fatalf("%s/%s commits domain truth = %v, want %v", mode, env, c.AllowsDomainCommit, wantCommit)
				}
				if c.AllowsExternalEffect != wantEffect {
					t.Fatalf("%s/%s causes external effects = %v, want %v", mode, env, c.AllowsExternalEffect, wantEffect)
				}
				if c.AllowsApprovalConsumption != wantApproval {
					t.Fatalf("%s/%s consumes approvals = %v, want %v", mode, env, c.AllowsApprovalConsumption, wantApproval)
				}
				if !wantEffect && c.Adapters == intent.AdapterLive {
					t.Fatalf("%s/%s guarantees no external effect and is bound to live adapters", mode, env)
				}
			}
		}
	})

	t.Run("RED: a zero-effect contract refuses every material attempt", func(t *testing.T) {
		for _, mode := range []intent.Mode{intent.ModeSimulate, intent.ModeShadow, intent.ModeReplay} {
			for _, env := range intent.Environments() {
				c := mustContract(t, mode, env)
				if !c.ZeroEffect() {
					t.Fatalf("%s/%s is not zero-effect", mode, env)
				}
				for _, attempt := range []intent.Attempt{
					intent.AttemptDomainCommit, intent.AttemptExternalEffect,
					intent.AttemptApprovalConsumption,
				} {
					if err := c.Permit(def, attempt); !errors.Is(err, intent.ErrEffectEscalation) {
						t.Fatalf("%s/%s permitted %s: %v", mode, env, attempt, err)
					}
				}
				if err := c.Permit(def, intent.AttemptGovernedRead); err != nil {
					t.Fatalf("%s/%s refused a governed read: %v", mode, env, err)
				}
			}
		}
	})

	t.Run("RED: a test or conformance run never commits", func(t *testing.T) {
		for _, env := range []intent.Environment{intent.EnvironmentTest, intent.EnvironmentConformance} {
			for _, mode := range intent.Modes() {
				c := mustContract(t, mode, env)
				if !c.ZeroEffect() {
					t.Fatalf("%s in %s is not zero-effect", mode, env)
				}
				if c.Adapters != intent.AdapterNull {
					t.Fatalf("%s in %s is bound to %s adapters", mode, env, c.Adapters)
				}
				if c.Clock != intent.ClockPinned {
					t.Fatalf("%s in %s does not pin its clock: %s", mode, env, c.Clock)
				}
			}
		}
	})

	t.Run("RED: a replay must name the run it re-derives", func(t *testing.T) {
		replay := mustContract(t, intent.ModeReplay, intent.EnvironmentProduction)
		if !replay.RequiresHistoricalCausation {
			t.Fatal("replay does not require historical causation")
		}
		inst := intent.Instance{IntentID: "intent:2", ExecutionMode: intent.ModeReplay}
		if err := intent.CausalSeparation(replay, inst); !errors.Is(err, intent.ErrCausalSeparation) {
			t.Fatalf("a replay that names no history was accepted: %v", err)
		}
		self := "intent:2"
		inst.CausationID = &self
		if err := intent.CausalSeparation(replay, inst); !errors.Is(err, intent.ErrCausalSeparation) {
			t.Fatalf("a replay that re-derives itself was accepted: %v", err)
		}
		historical := "intent:1"
		inst.CausationID = &historical
		if err := intent.CausalSeparation(replay, inst); err != nil {
			t.Fatalf("a well-formed replay was refused: %v", err)
		}

		// The same request run as a new action is a different record: it does
		// not claim to be its own cause, and its mode must match the contract
		// it is being run under.
		execute := mustContract(t, intent.ModeExecute, intent.EnvironmentProduction)
		fresh := intent.Instance{IntentID: "intent:3", ExecutionMode: intent.ModeExecute}
		if err := intent.CausalSeparation(execute, fresh); err != nil {
			t.Fatalf("a new action was refused: %v", err)
		}
		fresh.CausationID = &[]string{"intent:3"}[0]
		if err := intent.CausalSeparation(execute, fresh); !errors.Is(err, intent.ErrCausalSeparation) {
			t.Fatalf("a new action named itself as its own cause: %v", err)
		}
		if err := intent.CausalSeparation(execute, inst); !errors.Is(err, intent.ErrCausalSeparation) {
			t.Fatalf("a replay envelope was run under the execute contract: %v", err)
		}
	})

	t.Run("GREEN: execute needs an authorization of its own", func(t *testing.T) {
		execute := mustContract(t, intent.ModeExecute, intent.EnvironmentProduction)
		repair := mustContract(t, intent.ModeRepair, intent.EnvironmentProduction)
		for _, c := range []intent.ModeContract{execute, repair} {
			if !c.RequiresSeparateAuthorization {
				t.Fatalf("%s runs on the intent's authorization alone", c.Mode)
			}
		}
		for _, mode := range []intent.Mode{intent.ModeSimulate, intent.ModeShadow, intent.ModeReplay} {
			c := mustContract(t, mode, intent.EnvironmentProduction)
			if c.RequiresSeparateAuthorization {
				t.Fatalf("%s demands a second authorization for a zero-effect run", mode)
			}
		}
		if err := execute.Permit(def, intent.AttemptDomainCommit); err != nil {
			t.Fatalf("production execute refused a commit for an executable definition: %v", err)
		}
		if err := repair.Permit(def, intent.AttemptApprovalConsumption); !errors.Is(err, intent.ErrEffectEscalation) {
			t.Fatalf("repair consumed a business approval: %v", err)
		}
	})

	t.Run("GREEN: each mode has its own evidence namespace", func(t *testing.T) {
		seen := map[string]string{}
		for _, c := range intent.ModeContracts() {
			if c.EvidenceNamespace == "" {
				t.Fatalf("%s/%s has no evidence namespace", c.Mode, c.Environment)
			}
			key := c.Mode.String() + "/" + c.Environment.String()
			if other, ok := seen[c.EvidenceNamespace]; ok {
				t.Fatalf("%s and %s share the evidence namespace %q", other, key, c.EvidenceNamespace)
			}
			seen[c.EvidenceNamespace] = key
		}
	})

	t.Run("GREEN: mode comparison is deterministic and semantic", func(t *testing.T) {
		simulate := mustContract(t, intent.ModeSimulate, intent.EnvironmentProduction)
		execute := mustContract(t, intent.ModeExecute, intent.EnvironmentProduction)
		diffs := intent.CompareContracts(simulate, execute)
		if len(diffs) == 0 {
			t.Fatal("simulate and execute report no semantic difference")
		}
		want := map[string]bool{
			"mode": true, "allows_domain_commit": true, "allows_external_effect": true,
			"allows_approval_consumption": true, "requires_separate_authorization": true,
			"adapters": true, "effect_ceiling": true, "evidence_namespace": true,
		}
		got := map[string]bool{}
		for _, d := range diffs {
			got[d.Field] = true
		}
		for field := range want {
			if !got[field] {
				t.Fatalf("the comparison does not report %q: %v", field, diffs)
			}
		}
		// Same input, same answer, in the same order.
		again := intent.CompareContracts(simulate, execute)
		if len(again) != len(diffs) {
			t.Fatalf("comparison is not deterministic: %d then %d", len(diffs), len(again))
		}
		for i := range diffs {
			if diffs[i] != again[i] {
				t.Fatalf("comparison order drifted at %d: %v vs %v", i, diffs[i], again[i])
			}
		}
		if len(intent.CompareContracts(simulate, simulate)) != 0 {
			t.Fatal("a contract differs from itself")
		}
	})
}

// TestTodo_INTENT_023_Golden pins the whole mode-by-environment matrix. A
// widening anywhere in the table - one more mode that commits, one more
// environment bound to live adapters - moves this vector, which is the point:
// the escalation surface is reviewed as a diff rather than discovered later.
func TestTodo_INTENT_023_Golden(t *testing.T) {
	type row struct {
		Mode                          string
		Environment                   string
		AllowsDomainCommit            bool
		AllowsExternalEffect          bool
		AllowsApprovalConsumption     bool
		RequiresSeparateAuthorization bool
		RequiresHistoricalCausation   bool
		Adapters                      string
		Clock                         string
		EffectCeiling                 string
		EvidenceNamespace             string
	}
	contracts := intent.ModeContracts()
	rows := make([]row, 0, len(contracts))
	for _, c := range contracts {
		rows = append(rows, row{
			Mode:                          c.Mode.String(),
			Environment:                   c.Environment.String(),
			AllowsDomainCommit:            c.AllowsDomainCommit,
			AllowsExternalEffect:          c.AllowsExternalEffect,
			AllowsApprovalConsumption:     c.AllowsApprovalConsumption,
			RequiresSeparateAuthorization: c.RequiresSeparateAuthorization,
			RequiresHistoricalCausation:   c.RequiresHistoricalCausation,
			Adapters:                      c.Adapters.String(),
			Clock:                         c.Clock.String(),
			EffectCeiling:                 c.EffectCeiling.String(),
			EvidenceNamespace:             c.EvidenceNamespace,
		})
	}
	if len(rows) != len(intent.Modes())*len(intent.Environments()) {
		t.Fatalf("the matrix holds %d rows, want %d", len(rows), len(intent.Modes())*len(intent.Environments()))
	}
	goldenJSON(t, "intent_023_mode_matrix.json", rows)
}

// FuzzTodo_INTENT_023 drives arbitrary mode, environment and attempt bytes at
// the contract table. Whatever comes out, an attempt that is not one of the
// named production rows must never be permitted, and no input may panic.
func FuzzTodo_INTENT_023(f *testing.F) {
	f.Add(uint8(1), uint8(1), uint8(2))
	f.Add(uint8(2), uint8(1), uint8(3))
	f.Add(uint8(3), uint8(4), uint8(4))
	f.Add(uint8(0), uint8(0), uint8(0))
	f.Add(uint8(200), uint8(200), uint8(200))

	def := executableDefinition()
	f.Fuzz(func(t *testing.T, modeByte, envByte, attemptByte uint8) {
		mode := intent.Mode(modeByte)
		env := intent.Environment(envByte)
		attempt := intent.Attempt(attemptByte)

		c, err := intent.ModeContractFor(mode, env)
		if err != nil {
			if !errors.Is(err, intent.ErrInvalidModeContract) {
				t.Fatalf("an undeclared mode or environment failed for an untyped reason: %v", err)
			}
			if c != (intent.ModeContract{}) {
				t.Fatalf("a refused contract returned a populated value: %+v", c)
			}
			return
		}
		if c.Mode != mode || c.Environment != env {
			t.Fatalf("the table returned %s/%s for %s/%s", c.Mode, c.Environment, mode, env)
		}

		permitted := c.Permit(def, attempt) == nil
		switch attempt {
		case intent.AttemptDomainCommit:
			if permitted && !c.AllowsDomainCommit {
				t.Fatalf("%s/%s permitted a commit it does not allow", mode, env)
			}
		case intent.AttemptExternalEffect:
			if permitted && env != intent.EnvironmentProduction {
				t.Fatalf("%s/%s permitted an external effect outside production", mode, env)
			}
		case intent.AttemptApprovalConsumption:
			if permitted && !(mode == intent.ModeExecute && env == intent.EnvironmentProduction) {
				t.Fatalf("%s/%s consumed a live approval", mode, env)
			}
		case intent.AttemptGovernedRead:
			if !permitted {
				t.Fatalf("%s/%s refused a governed read", mode, env)
			}
		default:
			if permitted {
				t.Fatalf("%s/%s permitted the undeclared attempt %d", mode, env, attemptByte)
			}
		}
	})
}

// TestTodo_INTENT_023_Integration runs the contract table against the
// checked-in catalog.
//
// Two facts are checked against every published definition in every mode it
// allows. First, a zero-effect contract - which is every mode outside
// production EXECUTE and REPAIR, and every mode at all outside production and
// sandbox - refuses a commit and an external effect whatever the definition
// declares. Second, a definition scheduled for the zero-effect release is held
// to zero even under the contract that does reach the real world, so the two
// ceilings compose rather than one overriding the other.
func TestTodo_INTENT_023_Integration(t *testing.T) {
	reg := mustRegistry(t)
	for _, def := range reg.Definitions() {
		for _, mode := range def.AllowedModes {
			for _, env := range intent.Environments() {
				c := mustContract(t, mode, env)
				ceiling := c.EffectiveCeiling(def)
				if ceiling > def.EffectClass || ceiling > c.EffectCeiling {
					t.Fatalf("%s under %s/%s has ceiling %s, above one of its two bounds (%s, %s)",
						def.Ref, mode, env, ceiling, def.EffectClass, c.EffectCeiling)
				}
				if c.ZeroEffect() || def.EffectClass == intent.EffectClassZero {
					for _, attempt := range []intent.Attempt{
						intent.AttemptDomainCommit, intent.AttemptExternalEffect,
					} {
						if err := c.Permit(def, attempt); !errors.Is(err, intent.ErrEffectEscalation) {
							t.Fatalf("%s under %s/%s permitted %s: %v", def.Ref, mode, env, attempt, err)
						}
					}
					continue
				}
				// A definition that may mutate, under a contract that may
				// commit: the commit is permitted, and an external effect is
				// permitted only when both the contract and the definition's
				// own ceiling reach that far.
				if err := c.Permit(def, intent.AttemptDomainCommit); err != nil {
					t.Fatalf("%s under %s/%s could not commit: %v", def.Ref, mode, env, err)
				}
				wantExternal := c.AllowsExternalEffect && ceiling >= intent.EffectClassExternalMutation
				if err := c.Permit(def, intent.AttemptExternalEffect); (err == nil) != wantExternal {
					t.Fatalf("%s under %s/%s external effect permitted = %v, want %v (%v)",
						def.Ref, mode, env, err == nil, wantExternal, err)
				}
			}
			// A mode a definition does not allow is refused before the effect
			// question is even asked.
			for _, other := range intent.Modes() {
				if def.AllowsMode(other) {
					continue
				}
				c := mustContract(t, other, intent.EnvironmentProduction)
				if err := c.Permit(def, intent.AttemptGovernedRead); !errors.Is(err, intent.ErrModeNotAllowed) {
					t.Fatalf("%s ran under %s, which it does not allow: %v", def.Ref, other, err)
				}
			}
		}
	}
}

// TestTodo_INTENT_023_Fault covers the paths a harness hits when it is wired
// wrong: an undeclared mode, an undeclared environment, an undeclared attempt,
// and an instance with no identity.
func TestTodo_INTENT_023_Fault(t *testing.T) {
	def := executableDefinition()

	if _, err := intent.ModeContractFor(intent.ModeUnspecified, intent.EnvironmentProduction); !errors.Is(err, intent.ErrInvalidModeContract) {
		t.Fatalf("an unspecified mode resolved a contract: %v", err)
	}
	if _, err := intent.ModeContractFor(intent.ModeExecute, intent.EnvironmentUnspecified); !errors.Is(err, intent.ErrInvalidModeContract) {
		t.Fatalf("an unspecified environment resolved a contract: %v", err)
	}
	if _, err := intent.ModeContractFor(intent.Mode(99), intent.EnvironmentProduction); !errors.Is(err, intent.ErrInvalidModeContract) {
		t.Fatalf("an unknown mode resolved a contract: %v", err)
	}

	c := mustContract(t, intent.ModeExecute, intent.EnvironmentProduction)
	if err := c.Permit(def, intent.AttemptUnspecified); !errors.Is(err, intent.ErrInvalidModeContract) {
		t.Fatalf("an unspecified attempt was decided: %v", err)
	}
	if err := intent.CausalSeparation(c, intent.Instance{ExecutionMode: intent.ModeExecute}); !errors.Is(err, intent.ErrCausalSeparation) {
		t.Fatalf("an instance with no identity passed causal separation: %v", err)
	}
}

// TestTodo_INTENT_023_Conformance asserts the shape of the contract table
// itself: every declared pair resolves, exactly one row per pair, and the set
// of rows that reach the real world is exactly the set named in the contract's
// own documentation.
func TestTodo_INTENT_023_Conformance(t *testing.T) {
	seen := map[string]bool{}
	var reaching []string
	for _, mode := range intent.Modes() {
		for _, env := range intent.Environments() {
			c := mustContract(t, mode, env)
			key := mode.String() + "/" + env.String()
			if seen[key] {
				t.Fatalf("%s resolved twice", key)
			}
			seen[key] = true
			if c.Adapters == intent.AdapterLive || c.AllowsExternalEffect {
				reaching = append(reaching, key)
			}
			if c.Adapters == intent.AdapterUnspecified || c.Clock == intent.ClockUnspecified {
				t.Fatalf("%s binds no adapter profile or clock source", key)
			}
			if !c.EffectCeiling.Valid() {
				t.Fatalf("%s declares no effect ceiling", key)
			}
		}
	}
	if len(seen) != len(intent.Modes())*len(intent.Environments()) {
		t.Fatalf("the table resolved %d pairs, want %d", len(seen), len(intent.Modes())*len(intent.Environments()))
	}
	want := map[string]bool{"EXECUTE/PRODUCTION": true, "REPAIR/PRODUCTION": true}
	if len(reaching) != len(want) {
		t.Fatalf("%v reach the real world, want exactly EXECUTE/PRODUCTION and REPAIR/PRODUCTION", reaching)
	}
	for _, key := range reaching {
		if !want[key] {
			t.Fatalf("%s reaches the real world and should not", key)
		}
	}
}

// TestTodo_INTENT_023_Recovery checks that a refused escalation leaves nothing
// behind: the contract is a value read from a fixed table, so re-deriving it
// after a refusal returns exactly the same contract and the same answers.
func TestTodo_INTENT_023_Recovery(t *testing.T) {
	def := executableDefinition()
	before := mustContract(t, intent.ModeSimulate, intent.EnvironmentProduction)

	for range 10 {
		if err := before.Permit(def, intent.AttemptExternalEffect); !errors.Is(err, intent.ErrEffectEscalation) {
			t.Fatalf("a refused escalation stopped being refused: %v", err)
		}
	}

	after := mustContract(t, intent.ModeSimulate, intent.EnvironmentProduction)
	if before != after {
		t.Fatalf("the contract changed after refusals: %+v then %+v", before, after)
	}
	if diffs := intent.CompareContracts(before, after); len(diffs) != 0 {
		t.Fatalf("the contract drifted: %v", diffs)
	}
	// Execution remains available afterwards for a run that is entitled to it.
	execute := mustContract(t, intent.ModeExecute, intent.EnvironmentProduction)
	if err := execute.Permit(def, intent.AttemptExternalEffect); err != nil {
		t.Fatalf("a legitimate execution was refused after unrelated refusals: %v", err)
	}
}

// TestTodo_INTENT_023_Mutation flips one declared property of a contract at a
// time and requires the decision to move with it. A property that can be
// changed without changing any answer is a property the contract does not
// actually bind.
func TestTodo_INTENT_023_Mutation(t *testing.T) {
	def := executableDefinition()
	base := mustContract(t, intent.ModeExecute, intent.EnvironmentProduction)

	cases := []struct {
		name    string
		break_  func(*intent.ModeContract)
		attempt intent.Attempt
	}{
		{"commit withdrawn", func(c *intent.ModeContract) { c.AllowsDomainCommit = false }, intent.AttemptDomainCommit},
		{"external effect withdrawn", func(c *intent.ModeContract) { c.AllowsExternalEffect = false }, intent.AttemptExternalEffect},
		{"approval withdrawn", func(c *intent.ModeContract) { c.AllowsApprovalConsumption = false }, intent.AttemptApprovalConsumption},
		{"ceiling lowered to zero", func(c *intent.ModeContract) { c.EffectCeiling = intent.EffectClassZero }, intent.AttemptDomainCommit},
		{"ceiling lowered to internal", func(c *intent.ModeContract) { c.EffectCeiling = intent.EffectClassInternalMutation }, intent.AttemptExternalEffect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := base.Permit(def, tc.attempt); err != nil {
				t.Fatalf("the unmutated contract refused %s: %v", tc.attempt, err)
			}
			c := base
			tc.break_(&c)
			if err := c.Permit(def, tc.attempt); !errors.Is(err, intent.ErrEffectEscalation) {
				t.Fatalf("mutation %q did not change the answer: %v", tc.name, err)
			}
		})
	}

	t.Run("a definition that forbids the mode overrides every permission", func(t *testing.T) {
		narrowed := def
		narrowed.AllowedModes = []intent.Mode{intent.ModeSimulate}
		for _, attempt := range intent.Attempts() {
			if err := base.Permit(narrowed, attempt); !errors.Is(err, intent.ErrModeNotAllowed) {
				t.Fatalf("a definition that forbids EXECUTE permitted %s: %v", attempt, err)
			}
		}
	})

	t.Run("a zero-effect definition cannot be lifted by its mode", func(t *testing.T) {
		zero := def
		zero.EffectClass = intent.EffectClassZero
		zero.SideEffect = intent.SideEffectReadOnly
		zero.Family = intent.FamilyAnalyticalRequest
		zero.CorrectionRule = ""
		if err := zero.Validate(); err != nil {
			t.Fatalf("the zero-effect fixture is invalid: %v", err)
		}
		for _, attempt := range []intent.Attempt{intent.AttemptDomainCommit, intent.AttemptExternalEffect} {
			if err := base.Permit(zero, attempt); !errors.Is(err, intent.ErrEffectEscalation) {
				t.Fatalf("production execute lifted a zero-effect definition into %s: %v", attempt, err)
			}
		}
	})
}
