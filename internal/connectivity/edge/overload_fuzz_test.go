package edge

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

// FuzzTodo_EDGE_007 drives arbitrary sequences of 429/503/timeout/success
// events through one tenant's dependency and checks the oracle GREEN
// demands: the outcome is always one of the five declared values, and the
// number of effects actually performed within one logical operation episode
// never exceeds the configured retry budget -- proving no retry storm
// regardless of how the fuzzer orders or repeats failures.
func FuzzTodo_EDGE_007(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte{0, 0, 0, 0, 0})
	f.Add([]byte{3, 3, 3})
	f.Add([]byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		const allowed = 3
		coordinator := &Coordinator{
			Breaker: NewCircuitBreaker(CircuitConfig{FailureThreshold: 4, Cooldown: time.Minute}),
			Retry:   RetryLedger{Provisioner: admission.NewProvisioner(), Allowed: allowed, Version: "fuzz-v1"},
		}
		now := time.Now()
		const tenant, dependency = "fuzz-tenant", "fuzz-dependency"

		episode := 0
		attempt := 0
		episodeEffects := 0
		newEpisode := func() string {
			episode++
			attempt = 0
			episodeEffects = 0
			return fmt.Sprintf("fuzz-op-%d", episode)
		}
		logicalOp := newEpisode()

		for _, b := range data {
			switch b % 4 {
			case 0, 1, 2:
				var failure FailureSignal
				switch b % 4 {
				case 0:
					failure = FailureRateLimited
				case 1:
					failure = FailureUnavailable
				default:
					failure = FailureTimeout
				}
				attempt++
				req := OverloadRequest{
					TenantID: tenant, Dependency: dependency, LogicalOperationID: logicalOp,
					OperationKind: "fuzz", Attempt: attempt, Failure: failure,
					Signal: admission.BackpressureSignal{Source: "edge", Dependency: dependency, State: admission.BackpressureHealthy},
				}
				d, err := coordinator.Decide(now, req)
				if err != nil {
					t.Fatalf("unexpected error deciding attempt %d of %s: %v", attempt, logicalOp, err)
				}
				switch d.Outcome {
				case admission.Admit, admission.Queue, admission.Defer, admission.Degrade, admission.Reject:
				default:
					t.Fatalf("outcome %q is not one of the five declared outcomes", d.Outcome)
				}
				if d.PerformEffect {
					episodeEffects++
					if episodeEffects > allowed {
						t.Fatalf("retry storm: %d effects performed in one episode exceeds the budget of %d", episodeEffects, allowed)
					}
					coordinator.Breaker.Report(tenant, dependency, now, false)
				}
				if d.Outcome == admission.Reject {
					// Permanently refused (budget exhaustion or an open
					// circuit): start a fresh logical operation so the
					// fuzzer keeps exercising the ledger instead of
					// looping forever on one dead attempt.
					logicalOp = newEpisode()
				}
			default:
				// A success completes the in-flight attempt.
				coordinator.Breaker.Report(tenant, dependency, now, true)
				logicalOp = newEpisode()
			}
		}
	})
}
