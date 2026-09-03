package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// discardLogger is a *slog.Logger writing to io.Discard, used wherever a
// test needs a real Logger but not its output.
func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// freeLoopbackAddr binds an ephemeral loopback port, closes it immediately,
// and returns "127.0.0.1:<port>" for a Spec under test to bind moments
// later. The reuse window is short enough to be reliable in practice and is
// the standard way to hand a test a real, addressable TCP endpoint.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a loopback port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return addr
}

// TestTodo_SVC_002_Integration proves SVC-002's end-to-end contract through
// bootstrap.Run itself: a full starting/ready/draining/stopped lifecycle
// including the loopback health endpoint and the database pool factory, a
// panicking workload producing a non-zero exit without hanging, a database
// failure blocking Build entirely, config validation blocking everything,
// the shutdown deadline enforced through Run (not just runShutdown), and
// the Role vocabulary matching definitions/architecture/process-roles.yaml.
func TestTodo_SVC_002_Integration(t *testing.T) {
	t.Run("full_lifecycle_via_context_cancellation", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		fakePool := NewFakeDBPool()
		addr := freeLoopbackAddr(t)

		started := make(chan struct{})
		var mu sync.Mutex
		var shutdownOrder []string
		record := func(name string) {
			mu.Lock()
			shutdownOrder = append(shutdownOrder, name)
			mu.Unlock()
		}

		spec := Spec{
			Role:             RoleWorker,
			Args:             []string{"-database-url=postgres://fake"},
			Getenv:           func(string) (string, bool) { return "", false },
			Stdout:           &stdout,
			Stderr:           &stderr,
			Logger:           discardLogger(),
			ConfigFields:     []Field{{Name: "database-url", Secret: true}},
			DatabaseURLField: "database-url",
			DBPoolFactory:    NewFakeDBPoolFactory(fakePool),
			HealthAddr:       addr,
			Build: func(_ context.Context, deps Deps) (Runtime, error) {
				if deps.DB == nil {
					t.Error("Deps.DB was nil even though DatabaseURLField was set")
				}
				if deps.Identity == "" {
					t.Error("Deps.Identity was empty")
				}
				wl := Workload{Name: "loop", Run: func(ctx context.Context) error {
					close(started)
					<-ctx.Done()
					record("workload-stopped")
					return nil
				}}
				return Runtime{
					Workloads: []Workload{wl},
					Shutdown: []ShutdownStep{
						{Name: "custom", Run: func(context.Context) error { record("custom-step"); return nil }},
					},
				}, nil
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		go func() { done <- Run(ctx, spec) }()

		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("workload never started")
		}

		if !pollUntilReady(t, addr, 5*time.Second) {
			t.Fatal("health endpoint never reported READY")
		}

		cancel()

		var code int
		select {
		case code = <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return after its context was canceled")
		}

		if code != ExitOK {
			t.Fatalf("exit code = %d, want ExitOK; stderr=%s", code, stderr.String())
		}
		if !fakePool.Closed() {
			t.Fatal("expected the database pool to be closed during shutdown")
		}
		if fakePool.Pings() == 0 {
			t.Fatal("expected the database pool factory's Ping to have been exercised")
		}

		mu.Lock()
		gotOrder := append([]string(nil), shutdownOrder...)
		mu.Unlock()
		wantOrder := []string{"workload-stopped", "custom-step"}
		if !reflect.DeepEqual(gotOrder, wantOrder) {
			t.Fatalf("shutdown order = %v, want %v (workload must drain before the custom step, which must run before the pool closes)", gotOrder, wantOrder)
		}
	})

	t.Run("panicking_workload_yields_nonzero_exit_without_hanging", func(t *testing.T) {
		spec := Spec{
			Role:   RoleProjector,
			Logger: discardLogger(),
			Build: func(context.Context, Deps) (Runtime, error) {
				return Runtime{Workloads: []Workload{
					{Name: "boom", Run: func(context.Context) error { panic("integration-boom") }},
				}}, nil
			},
		}

		done := make(chan int, 1)
		go func() { done <- Run(context.Background(), spec) }()

		select {
		case code := <-done:
			if code != ExitRuntimeError {
				t.Fatalf("exit code = %d, want ExitRuntimeError", code)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run hung after a workload panicked")
		}
	})

	t.Run("database_unavailable_fails_before_build_or_any_workload", func(t *testing.T) {
		buildCalled := false
		spec := Spec{
			Role:             RoleMigrate,
			Logger:           discardLogger(),
			ConfigFields:     []Field{{Name: "database-url", Default: "postgres://fake"}},
			DatabaseURLField: "database-url",
			DBPoolFactory: func(context.Context, string) (DBPool, error) {
				return nil, errors.New("connection refused")
			},
			Build: func(context.Context, Deps) (Runtime, error) {
				buildCalled = true
				return Runtime{}, nil
			},
		}

		code := Run(context.Background(), spec)
		if code != ExitRuntimeError {
			t.Fatalf("exit code = %d, want ExitRuntimeError", code)
		}
		if buildCalled {
			t.Fatal("Build must not run when the database pool cannot be established")
		}
	})

	t.Run("config_validation_failure_stops_before_anything_starts", func(t *testing.T) {
		buildCalled := false
		spec := Spec{
			Role:   RoleAdmin,
			Logger: discardLogger(),
			Validate: func(*Values) error {
				return errors.New("bad config")
			},
			Build: func(context.Context, Deps) (Runtime, error) {
				buildCalled = true
				return Runtime{}, nil
			},
		}

		code := Run(context.Background(), spec)
		if code != ExitConfigError {
			t.Fatalf("exit code = %d, want ExitConfigError", code)
		}
		if buildCalled {
			t.Fatal("Build must not run when config validation fails")
		}
	})

	t.Run("invalid_role_is_a_config_error", func(t *testing.T) {
		buildCalled := false
		spec := Spec{
			Role:   Role("not-a-real-role"),
			Logger: discardLogger(),
			Build: func(context.Context, Deps) (Runtime, error) {
				buildCalled = true
				return Runtime{}, nil
			},
		}
		code := Run(context.Background(), spec)
		if code != ExitConfigError {
			t.Fatalf("exit code = %d, want ExitConfigError", code)
		}
		if buildCalled {
			t.Fatal("Build must not run when the role itself is invalid")
		}
	})

	t.Run("shutdown_deadline_enforced_end_to_end", func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)

		spec := Spec{
			Role:             RoleWorker,
			Logger:           discardLogger(),
			ShutdownDeadline: 100 * time.Millisecond,
			Build: func(context.Context, Deps) (Runtime, error) {
				wl := Workload{Name: "stubborn", Run: func(context.Context) error {
					<-release // deliberately ignores ctx cancellation
					return nil
				}}
				return Runtime{Workloads: []Workload{wl}}, nil
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		go func() { done <- Run(ctx, spec) }()

		// Give the workload a moment to actually start before triggering
		// shutdown.
		time.Sleep(50 * time.Millisecond)
		start := time.Now()
		cancel()

		select {
		case code := <-done:
			elapsed := time.Since(start)
			if code != ExitShutdownTimeout {
				t.Fatalf("exit code = %d, want ExitShutdownTimeout", code)
			}
			if elapsed > 5*time.Second {
				t.Fatalf("Run took %s to honor a 100ms shutdown deadline", elapsed)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run hung past its shutdown deadline")
		}
	})

	t.Run("role_vocabulary_matches_process_roles_manifest", func(t *testing.T) {
		path := filepath.Join("..", "..", "..", "definitions", "architecture", "process-roles.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}

		var manifest struct {
			Processes []struct {
				Command string `yaml:"command"`
			} `yaml:"processes"`
		}
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		declared := map[string]bool{}
		for _, p := range manifest.Processes {
			declared[p.Command] = true
		}
		for _, r := range roleVocabulary {
			if !declared[string(r)] {
				t.Errorf("bootstrap.Role %q has no matching command row in %s", r, path)
			}
		}
		if len(declared) != len(roleVocabulary) {
			t.Errorf("%s declares %d commands but bootstrap's role vocabulary has %d entries; keep role.go's roleVocabulary in sync with the manifest", path, len(declared), len(roleVocabulary))
		}
	})
}

// pollUntilReady polls the health endpoint at addr until it reports HTTP
// 200 (READY) or the deadline elapses.
func pollUntilReady(t *testing.T, addr string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://%s/healthz", addr))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
