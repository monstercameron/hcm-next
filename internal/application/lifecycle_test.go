package application

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

// blockingWorkload returns a workload that runs until released, plus its
// release function and a channel closed once it has actually started.
func blockingWorkload(name string, err error) (bootstrap.Workload, func(), <-chan struct{}) {
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	return bootstrap.Workload{
			Name: name,
			Run: func(context.Context) error {
				close(started)
				<-release
				return err
			},
		},
		func() { once.Do(func() { close(release) }) },
		started
}

// TestAppLifecycleRunsTheSameWorkloadsBootstrapRuns is the point of having a
// Lifecycle at all: Start/Stop and bootstrap.Runtime must be two views of one
// composition, not two lists that can drift.
func TestAppLifecycleRunsTheSameWorkloadsBootstrapRuns(t *testing.T) {
	workload, release, started := blockingWorkload("one", nil)
	defer release()
	stopped := false
	application := &App{
		role:      RoleServe,
		workloads: []bootstrap.Workload{workload},
		shutdown: []bootstrap.ShutdownStep{
			{Name: "release", Run: func(context.Context) error {
				stopped = true
				release()
				return nil
			}},
			{Name: "no-op with no Run"},
		},
	}

	runtime := application.Runtime()
	if len(runtime.Workloads) != 1 || runtime.Workloads[0].Name != "one" {
		t.Fatalf("Runtime().Workloads = %v, want the composed workload", runtime.Workloads)
	}
	if len(runtime.Shutdown) != 2 {
		t.Fatalf("Runtime().Shutdown has %d steps, want the composed 2", len(runtime.Shutdown))
	}
	runtime.Workloads[0].Name = "mutated"
	if application.Runtime().Workloads[0].Name != "one" {
		t.Fatal("Runtime() handed out the composition's own slice")
	}

	if application.Role() != RoleServe {
		t.Errorf("Role() = %q, want %q", application.Role(), RoleServe)
	}
	if err := application.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the workload never started")
	}
	if err := application.Start(context.Background()); err == nil {
		t.Error("Start accepted a second start")
	}
	if err := application.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !stopped {
		t.Error("Stop did not run the ordered shutdown step")
	}
	if err := application.Stop(context.Background()); err != nil {
		t.Errorf("second Stop = %v, want it to be a no-op", err)
	}
	if err := application.Start(context.Background()); err == nil {
		t.Error("Start accepted a start after Stop")
	}
}

// TestAppStopRunsEveryStepAndReportsEveryFailure proves shutdown does not
// abandon the remaining steps on the first error: a shutdown that stops early
// leaves exactly the resources it was supposed to release.
func TestAppStopRunsEveryStepAndReportsEveryFailure(t *testing.T) {
	first := errors.New("first step failed")
	second := errors.New("second step failed")
	ran := 0
	workloadErr := errors.New("workload failed")
	workload, release, started := blockingWorkload("failing", workloadErr)

	application := &App{
		role:      RoleServe,
		workloads: []bootstrap.Workload{workload},
		shutdown: []bootstrap.ShutdownStep{
			{Name: "one", Run: func(context.Context) error { ran++; return first }},
			{Name: "two", Run: func(context.Context) error { ran++; release(); return second }},
			{Name: "three", Run: func(context.Context) error { ran++; return nil }},
		},
	}
	if err := application.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started
	err := application.Stop(context.Background())
	if ran != 3 {
		t.Errorf("Stop ran %d steps, want all 3", ran)
	}
	for _, want := range []error{first, second, workloadErr} {
		if !errors.Is(err, want) {
			t.Errorf("Stop error %v does not report %v", err, want)
		}
	}
	if !strings.Contains(err.Error(), "failing") {
		t.Errorf("Stop error %q does not name the failing workload", err)
	}
}

// TestAppStopReleasesListenersItNeverServed covers the composition that was
// built and then abandoned: the ports it bound have to come back even though
// no workload ever ran.
func TestAppStopReleasesListenersItNeverServed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	application := &App{role: RoleServe, listeners: []net.Listener{listener, nil}}
	if err := application.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	reopened, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the abandoned composition held %s: %v", addr, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// An already-closed listener is not a shutdown failure: in the deployed
	// process the ordered steps close it first and this is a no-op.
	if err := (&App{role: RoleServe, listeners: []net.Listener{listener}}).Stop(context.Background()); err != nil {
		t.Errorf("Stop on an already-closed listener = %v, want nil", err)
	}
}

// TestAppExposesWhatWasComposed pins the read-back surface a caller
// legitimately needs: which cell, which addresses, which graph.
func TestAppExposesWhatWasComposed(t *testing.T) {
	graph := Graph{Role: RoleServe, Components: []Component{{Name: "cell", Kind: KindRegistry}}}
	application := &App{role: RoleServe, graph: graph, grpcAddr: "127.0.0.1:1", httpAddr: "127.0.0.1:2"}
	if application.GRPCAddr() != "127.0.0.1:1" || application.HTTPAddr() != "127.0.0.1:2" {
		t.Errorf("addresses = %q/%q, want the bound ones", application.GRPCAddr(), application.HTTPAddr())
	}
	if application.Cell() != nil {
		t.Error("Cell() invented a cell this composition never built")
	}
	if got := application.Graph(); got.Digest() != graph.Digest() {
		t.Errorf("Graph() digest = %s, want %s", got.Digest(), graph.Digest())
	}
	var lifecycle Lifecycle = application
	if err := lifecycle.Stop(context.Background()); err != nil {
		t.Errorf("Stop through the Lifecycle interface: %v", err)
	}
}
