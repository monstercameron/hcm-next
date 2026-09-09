package replay

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// Admit checks that a run may proceed under the contract, definition and
// instance it was handed, before any record is read.
//
// It is the whole of WF-RUN-013's "runs only under the REPLAY ModeContract"
// clause, and it is deliberately paranoid in both directions. It refuses a
// contract that is not REPLAY, which is what makes "Permit refuses EXECUTE"
// true at this boundary rather than only inside [intent.ModeContract.Permit].
// And it refuses a REPLAY contract whose guarantees do not hold -- one that
// would permit a domain commit, an external effect or a live approval, that
// reads a clock other than the historical one, that binds anything but the
// null adapter profile, or that does not require historical causation. A
// contract is a value a caller supplies; a replay that trusted whatever
// arrived would have no guarantee at all, only a name.
//
// The definition matters as well as the contract: [intent.ModeContract.Permit]
// composes the mode's ceiling with the definition's own effect class, and a
// definition that does not allow REPLAY cannot be replayed no matter which
// contract is presented.
func Admit(contract intent.ModeContract, def intent.Definition, inst intent.Instance) error {
	if contract.Mode != intent.ModeReplay {
		return refuse(CodeModeRefused, "",
			"a replay runs only under the REPLAY mode contract; %s was supplied", contract.Mode)
	}
	if err := contract.Permit(def, intent.AttemptGovernedRead); err != nil {
		return wrap(CodeModeRefused, "", err,
			"the REPLAY contract does not admit a governed read of %s", def.Ref)
	}
	// Each of the three below must be refused. Asserting the refusal, rather
	// than assuming it, is what catches a contract table that has been edited
	// to loosen REPLAY: the run stops here instead of discovering it at the
	// node that writes.
	for _, guarantee := range []struct {
		attempt intent.Attempt
		claim   string
	}{
		{intent.AttemptDomainCommit, "commit domain truth"},
		{intent.AttemptExternalEffect, "cause an external effect"},
		{intent.AttemptApprovalConsumption, "consume a live approval"},
	} {
		if err := contract.Permit(def, guarantee.attempt); err == nil {
			return refuse(CodeEffectForbidden, "",
				"the supplied REPLAY contract would permit %s (%s); a replay re-derives history and commits nothing",
				guarantee.attempt, guarantee.claim)
		}
	}
	if !contract.ZeroEffect() {
		return refuse(CodeEffectForbidden, "",
			"the supplied REPLAY contract is not zero-effect")
	}
	// HISTORICAL is the REPLAY row's own clock in PRODUCTION and SANDBOX.
	// TEST and CONFORMANCE pin the clock for every mode, REPLAY included
	// (internal/intent.contractFor's last clause), so PINNED is admitted here
	// too: what a replay must never do is read the wall clock, and both of
	// these are the harness supplying an instant rather than the machine
	// reading one.
	if contract.Clock != intent.ClockHistorical && contract.Clock != intent.ClockPinned {
		return refuse(CodeModeRefused, "",
			"a replay never reads the wall clock; the supplied contract reads %s", contract.Clock)
	}
	if contract.Adapters != intent.AdapterNull {
		return refuse(CodeModeRefused, "",
			"a replay binds the null adapter profile; the supplied contract binds %s", contract.Adapters)
	}
	if !contract.RequiresHistoricalCausation {
		return refuse(CodeModeRefused, "",
			"a replay must name the historical intent it re-derives; the supplied contract does not require it")
	}
	if err := intent.CausalSeparation(contract, inst); err != nil {
		return wrap(CodeCausalSeparation, "", err, "replay identity")
	}
	return nil
}

// ContractFor returns the REPLAY contract for one environment, so a caller
// composing a replay does not have to remember which of the twenty rows of
// [intent.ModeContracts] it wants.
func ContractFor(env intent.Environment) (intent.ModeContract, error) {
	c, err := intent.ModeContractFor(intent.ModeReplay, env)
	if err != nil {
		return intent.ModeContract{}, wrap(CodeModeRefused, "", err,
			"resolve the REPLAY contract for %s", env)
	}
	return c, nil
}
