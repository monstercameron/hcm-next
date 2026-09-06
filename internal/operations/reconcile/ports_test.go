package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/workflow/lease"
)

// TestPorts_LeaseManagerSatisfiesFenceVerifier proves the doc.go and ports.go
// claim that a production lease.Manager can be handed to Coordinator.Fences
// with no adapter: the port's signature is declared here to match, not the
// other way round.
func TestPorts_LeaseManagerSatisfiesFenceVerifier(t *testing.T) {
	var _ FenceVerifier = lease.Manager{}
}

// fakeObserver and fakeComparer (used across this package's other test files)
// prove Observer and Comparer are ordinary, mockable interfaces: a coordinator
// under test never has to reach a real external system or a real comparison
// policy engine.
type fakeObserver struct {
	result ObservationResult
	err    error
	calls  int
}

func (f *fakeObserver) Observe(ctx context.Context, ex Executor, job Job, now time.Time) (ObservationResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeComparer struct {
	verdict Verdict
	err     error
	calls   int
}

func (f *fakeComparer) Compare(ctx context.Context, ex Executor, job Job, obs ObservationResult, now time.Time) (Verdict, error) {
	f.calls++
	return f.verdict, f.err
}

func TestPorts_FakesSatisfyTheDeclaredInterfaces(t *testing.T) {
	var _ Observer = &fakeObserver{}
	var _ Comparer = &fakeComparer{}
}
