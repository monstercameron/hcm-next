package replay

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// TestContractFor_ResolvesOnlyReplayRows checks the convenience resolver: it
// returns the REPLAY row for a declared environment and refuses an undeclared
// one rather than handing back a zero contract.
func TestContractFor_ResolvesOnlyReplayRows(t *testing.T) {
	for _, env := range intent.Environments() {
		c, err := ContractFor(env)
		if err != nil {
			t.Fatalf("%s: %v", env, err)
		}
		if c.Mode != intent.ModeReplay || c.Environment != env {
			t.Fatalf("%s resolved %s/%s", env, c.Mode, c.Environment)
		}
		if !c.ZeroEffect() || !c.RequiresHistoricalCausation {
			t.Fatalf("%s: REPLAY row is not zero-effect or requires no causation: %+v", env, c)
		}
	}
	if _, err := ContractFor(intent.EnvironmentUnspecified); CodeOf(err) != CodeModeRefused {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeModeRefused)
	}
}

// TestAdmit_RefusesADefinitionThatDoesNotAllowReplay is the half of the gate
// the definition owns: [intent.ModeContract.Permit] composes the contract's
// ceiling with the definition's own allowed modes, and a definition that never
// admitted REPLAY cannot be replayed under any contract.
func TestAdmit_RefusesADefinitionThatDoesNotAllowReplay(t *testing.T) {
	def := replayDefinition()
	def.AllowedModes = []intent.Mode{intent.ModeExecute}
	err := Admit(replayContract(t), def, replayInstance())
	if CodeOf(err) != CodeModeRefused {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeModeRefused)
	}
}

// TestAdmit_AcceptsAPinnedClockButNeverAWallClock is the reading the TEST and
// CONFORMANCE rows force: those planes pin the clock for every mode, REPLAY
// included, so PINNED is admitted and only WALL is refused.
func TestAdmit_AcceptsAPinnedClockButNeverAWallClock(t *testing.T) {
	base := replayContract(t)
	def, inst := replayDefinition(), replayInstance()

	for _, clock := range []intent.ClockSource{intent.ClockHistorical, intent.ClockPinned} {
		c := base
		c.Clock = clock
		if err := Admit(c, def, inst); err != nil {
			t.Fatalf("%s clock was refused: %v", clock, err)
		}
	}
	for _, clock := range []intent.ClockSource{intent.ClockWall, intent.ClockUnspecified} {
		c := base
		c.Clock = clock
		if err := Admit(c, def, inst); CodeOf(err) != CodeModeRefused {
			t.Fatalf("%s clock: code = %q (%v)", clock, CodeOf(err), err)
		}
	}
}

// TestAdmit_RefusesAContractThatIsNotZeroEffect covers the belt-and-braces
// check beside the three Permit assertions: a contract that reports itself
// non-zero-effect is refused even if each individual attempt happened to be
// denied.
func TestAdmit_RefusesAContractThatIsNotZeroEffect(t *testing.T) {
	c := replayContract(t)
	c.AllowsDomainCommit = true
	def := replayDefinition()
	// A zero-effect definition would make Permit deny the commit anyway on the
	// ceiling, so give the definition room and let the contract be the thing
	// under test.
	def.EffectClass = intent.EffectClassInternalMutation
	if err := Admit(c, def, replayInstance()); CodeOf(err) != CodeEffectForbidden {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeEffectForbidden)
	}
}

// TestAdmit_RunsBeforeAnyRecordIsRead is the ordering that matters: New
// refuses an inadmissible contract without ever calling the source.
func TestAdmit_RunsBeforeAnyRecordIsRead(t *testing.T) {
	plan := promotionPlan(t)
	loaded := 0
	src := sourceFunc(func(context.Context) (Record, error) {
		loaded++
		return promotionRecord(t, plan), nil
	})
	execContract, err := intent.ModeContractFor(intent.ModeExecute, intent.EnvironmentProduction)
	if err != nil {
		t.Fatalf("EXECUTE contract: %v", err)
	}
	if _, err := New(Options{
		Plan: plan, Source: src, Contract: execContract,
		Definition: replayDefinition(), Instance: replayInstance(),
	}); CodeOf(err) != CodeModeRefused {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeModeRefused)
	}
	if loaded != 0 {
		t.Fatalf("the source was read %d times before admission", loaded)
	}
}
