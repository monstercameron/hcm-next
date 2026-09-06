package artifacts

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/workflow/timer"
)

// stopAt is a [Barrier] that refuses to enter one named kind. It is the seam
// the fault case drives, and it doubles as the record of which handlers ran.
type stopAt struct {
	kind    Kind
	err     error
	entered []Kind
}

func (b *stopAt) Enter(kind Kind) error {
	b.entered = append(b.entered, kind)
	if kind == b.kind {
		if b.err != nil {
			return b.err
		}
		return errors.New("barrier stop")
	}
	return nil
}

func TestMigrate_RefusesBeforeReadingAnythingWhenTheFrameIsMalformed(t *testing.T) {
	t.Parallel()
	s := newScenario(t)
	s.scope.MigratedBy = ""
	if _, err := s.run(t); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("refused with %q, want %q (err %v)", CodeOf(err), CodeInvalidRequest, err)
	}
	if calls := s.ports.Calls(); len(calls) != 0 {
		t.Fatalf("a malformed frame still touched storage: %v", calls)
	}
}

func TestMigrate_RefusesAPortSetWithAHole(t *testing.T) {
	t.Parallel()
	s := newScenario(t)
	ports := s.ports.ports()
	ports.Continuations = nil
	_, err := Migrate(context.Background(), nil, Request{Scope: s.scope, Ports: ports})
	if CodeOf(err) != CodeMissingPort {
		t.Fatalf("refused with %q, want %q (err %v)", CodeOf(err), CodeMissingPort, err)
	}
}

func TestMigrate_RunsEveryHandlerInDeclaredOrder(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	barrier := &stopAt{kind: "NEVER"}
	if _, err := Migrate(context.Background(), nil, Request{
		Scope: s.scope, Ports: s.ports.ports(), Barrier: barrier,
	}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(barrier.entered) != len(Kinds()) {
		t.Fatalf("the barrier saw %v, want every kind", barrier.entered)
	}
	for i, kind := range Kinds() {
		if barrier.entered[i] != kind {
			t.Fatalf("handler %d was %q, want %q", i, barrier.entered[i], kind)
		}
	}
}

func TestMigrate_ProducesACanonicalDigestedReceipt(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	receipt := s.mustRun(t)

	if receipt.ContractVersion != ContractVersion {
		t.Fatalf("receipt names contract %q, want %q", receipt.ContractVersion, ContractVersion)
	}
	if receipt.TenantID != s.tenant || receipt.InstanceID != s.instance {
		t.Fatalf("receipt names %s/%s", receipt.TenantID, receipt.InstanceID)
	}
	if len(receipt.Digest()) != 64 {
		t.Fatalf("receipt digest = %q", receipt.Digest())
	}
	if receipt.From != s.scope.From || receipt.To != s.scope.To {
		t.Fatalf("receipt epochs = %+v -> %+v", receipt.From, receipt.To)
	}
	last := -1
	for _, e := range receipt.Entries {
		if e.Kind.order() < last {
			t.Fatalf("receipt entries are not in handler order: %v", identities(receipt))
		}
		last = e.Kind.order()
	}
}

func TestMigrate_BarrierStopsTheRunWithATypedRefusal(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	barrier := &stopAt{kind: KindReadyWork}
	_, err := Migrate(context.Background(), nil, Request{
		Scope: s.scope, Ports: s.ports.ports(), Barrier: barrier,
	})
	if CodeOf(err) != CodeBarrierFailed {
		t.Fatalf("refused with %q, want %q (err %v)", CodeOf(err), CodeBarrierFailed, err)
	}
	if KindOf(err) != KindReadyWork {
		t.Fatalf("refused under kind %q, want %q", KindOf(err), KindReadyWork)
	}
	// The handlers before the barrier did run and did write: that is why the
	// caller has to roll back rather than shrug.
	if _, ok := s.ports.timers[timerAt(s, toNode, targetRequirementDigest)]; !ok {
		t.Fatal("the timer handler ran before the barrier but wrote nothing")
	}
	// And the handlers after it did not.
	if totalContinuations(s) != 0 {
		t.Fatal("a handler after the barrier still ran")
	}
}

func TestMigrate_StorageFailureStopsTheRunAndNamesTheKind(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	boom := errors.New("connection reset")
	s.ports.fail["signals.Subscribe"] = boom

	_, err := s.run(t)
	if CodeOf(err) != CodeStorageFailed {
		t.Fatalf("refused with %q, want %q (err %v)", CodeOf(err), CodeStorageFailed, err)
	}
	if KindOf(err) != KindSignal {
		t.Fatalf("refused under kind %q, want %q", KindOf(err), KindSignal)
	}
	if !errors.Is(err, boom) {
		t.Fatal("the storage failure was not wrapped")
	}
	if totalContinuations(s) != 0 {
		t.Fatal("a handler after the failing one still ran")
	}
}

func timerAt(s *scenario, node, key string) uuid.UUID {
	return timer.TimerID(s.tenant, s.instance, node, key)
}
