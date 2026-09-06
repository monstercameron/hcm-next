package termination

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestSmoke_Scenarios is a fast sanity check across every scenario variant,
// before the full CONF-005 matrix in conformance_test.go exercises each in
// detail.
func TestSmoke_Scenarios(t *testing.T) {
	cases := []struct {
		name   string
		env    *Environment
		params Params
		want   string
	}{
		{"golden", GoldenEnvironment(), Params{}, NodeEndPendingObligations},
		{"legal_hold", LegalHoldEnvironment(), Params{}, NodeEndLegalHoldBlocked},
		{"sod_violation", GoldenEnvironment(), Params{RequesterID: "mgr-001"}, NodeEndSoDViolationBlocked},
		{"reinstatement", AlreadyEndedEnvironment(), Params{CancelRequested: true}, NodeEndRequiresReinstate},
		{"already_ended_invalid", AlreadyEndedEnvironment(), Params{}, NodeEndRejectedInvalid},
		{"degraded_observation", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair},
		{"missing_legal_context", GoldenEnvironment(), Params{OmitLegalContext: true}, NodeEndUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setup, err := NewSetupWithParams(c.env, c.params)
			if err != nil {
				t.Fatalf("NewSetupWithParams: %v", err)
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if receipt.Terminal.NodeID != c.want {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.want, receipt.NodeIDs())
			}
		})
	}
}
