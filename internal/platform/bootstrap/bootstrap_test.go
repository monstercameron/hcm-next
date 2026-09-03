package bootstrap

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTodo_SVC_002 proves the SVC-002 unit-level contract: config
// precedence and redaction, the health state machine, the run-group's
// first-error cancellation and panic-to-error conversion, ordered shutdown,
// its hard deadline, role validation and exit-code mapping.
func TestTodo_SVC_002(t *testing.T) {
	t.Run("config_precedence_flag_beats_env_beats_default", func(t *testing.T) {
		fields := []Field{
			{Name: "database-url", Env: "TEST_DATABASE_URL", Default: "default-url", Secret: true},
			{Name: "poll-interval", Env: "TEST_POLL_INTERVAL", Default: "2s", Kind: KindDuration},
		}
		noEnv := func(string) (string, bool) { return "", false }

		t.Run("default_when_neither_flag_nor_env_set", func(t *testing.T) {
			v, err := ParseConfig(nil, noEnv, fields)
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			if got := v.String("database-url"); got != "default-url" {
				t.Fatalf("database-url = %q, want default-url", got)
			}
			if got := v.Source("database-url"); got != "default" {
				t.Fatalf("source = %q, want default", got)
			}
		})

		t.Run("env_overrides_default", func(t *testing.T) {
			env := func(k string) (string, bool) {
				if k == "TEST_DATABASE_URL" {
					return "env-url", true
				}
				return "", false
			}
			v, err := ParseConfig(nil, env, fields)
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			if got := v.String("database-url"); got != "env-url" {
				t.Fatalf("database-url = %q, want env-url", got)
			}
			if got := v.Source("database-url"); got != "env" {
				t.Fatalf("source = %q, want env", got)
			}
		})

		t.Run("flag_overrides_env_and_default", func(t *testing.T) {
			env := func(k string) (string, bool) {
				if k == "TEST_DATABASE_URL" {
					return "env-url", true
				}
				return "", false
			}
			v, err := ParseConfig([]string{"-database-url=flag-url"}, env, fields)
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			if got := v.String("database-url"); got != "flag-url" {
				t.Fatalf("database-url = %q, want flag-url", got)
			}
			if got := v.Source("database-url"); got != "flag" {
				t.Fatalf("source = %q, want flag", got)
			}
		})

		t.Run("duration_field_parses_typed", func(t *testing.T) {
			v, err := ParseConfig([]string{"-poll-interval=5s"}, noEnv, fields)
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			d, err := v.Duration("poll-interval")
			if err != nil {
				t.Fatalf("Duration: %v", err)
			}
			if d != 5*time.Second {
				t.Fatalf("poll-interval = %s, want 5s", d)
			}
		})

		t.Run("unregistered_flag_fails_before_anything_starts", func(t *testing.T) {
			if _, err := ParseConfig([]string{"-does-not-exist=x"}, noEnv, fields); err == nil {
				t.Fatal("expected an error for an unregistered flag")
			}
		})
	})

	t.Run("secret_fields_are_redacted_everywhere", func(t *testing.T) {
		fields := []Field{
			{Name: "database-url", Default: "unused", Secret: true},
			{Name: "poll-interval", Default: "2s"},
		}
		noEnv := func(string) (string, bool) { return "", false }

		v1, err := ParseConfig([]string{"-database-url=secret-A", "-poll-interval=5s"}, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if strings.Contains(v1.Effective(), "secret-A") {
			t.Fatalf("Effective leaked the secret value: %s", v1.Effective())
		}
		if !strings.Contains(v1.Effective(), RedactedValue) {
			t.Fatalf("Effective did not redact database-url: %s", v1.Effective())
		}
		for _, kv := range v1.LogAttrs() {
			if s, ok := kv.(string); ok && s == "secret-A" {
				t.Fatalf("LogAttrs leaked the secret value: %v", v1.LogAttrs())
			}
		}

		v2, err := ParseConfig([]string{"-database-url=secret-B", "-poll-interval=5s"}, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if v1.Fingerprint() != v2.Fingerprint() {
			t.Fatalf("fingerprint changed when only a Secret field's value changed: %s vs %s", v1.Fingerprint(), v2.Fingerprint())
		}

		v3, err := ParseConfig([]string{"-database-url=secret-A", "-poll-interval=9s"}, noEnv, fields)
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		if v1.Fingerprint() == v3.Fingerprint() {
			t.Fatal("fingerprint did not change when a non-secret field's value changed")
		}
	})

	t.Run("role_validation", func(t *testing.T) {
		for _, r := range roleVocabulary {
			if err := r.Validate(); err != nil {
				t.Errorf("Role(%q).Validate() = %v, want nil", r, err)
			}
		}
		for _, bad := range []Role{"", "bogus", "HCMNext"} {
			if err := bad.Validate(); err == nil {
				t.Errorf("Role(%q).Validate() = nil, want an error", bad)
			}
		}
	})

	t.Run("health_state_machine", func(t *testing.T) {
		h := NewHealth()
		if got := h.Get(); got != StateStarting {
			t.Fatalf("initial state = %s, want STARTING", got)
		}
		for _, next := range []HealthState{StateReady, StateDraining, StateStopped} {
			if err := h.Set(next); err != nil {
				t.Fatalf("Set(%s): %v", next, err)
			}
		}
		if err := h.Set(StateStopped); err != nil {
			t.Fatalf("re-setting the same state should be a no-op: %v", err)
		}

		invalid := NewHealth()
		if err := invalid.Set(StateStopped); err != nil {
			t.Fatalf("STARTING -> STOPPED should be allowed (fail before ready): %v", err)
		}
		if err := invalid.Set(StateReady); err == nil {
			t.Fatal("STOPPED -> READY must be rejected")
		}

		backwards := NewHealth()
		_ = backwards.Set(StateReady)
		if err := backwards.Set(StateStarting); err == nil {
			t.Fatal("READY -> STARTING must be rejected")
		}
	})

	t.Run("health_handler_reflects_state", func(t *testing.T) {
		h := NewHealth()
		req := httptest.NewRequest("GET", "/healthz", nil)

		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		if rec.Code != 503 {
			t.Fatalf("STARTING status = %d, want 503", rec.Code)
		}

		_ = h.Set(StateReady)
		rec = httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("READY status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "READY") {
			t.Fatalf("body = %q, want it to mention READY", rec.Body.String())
		}
	})

	t.Run("group_first_error_cancels_siblings", func(t *testing.T) {
		g := newGroup(context.Background())
		observed := make(chan struct{})

		g.goRun(Workload{Name: "failer", Run: func(context.Context) error {
			return errors.New("boom")
		}})
		g.goRun(Workload{Name: "watcher", Run: func(ctx context.Context) error {
			<-ctx.Done()
			close(observed)
			return ctx.Err()
		}})

		select {
		case <-observed:
		case <-time.After(2 * time.Second):
			t.Fatal("sibling workload was never canceled after the first failure")
		}

		err := g.wait()
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("group.wait() = %v, want an error mentioning \"boom\"", err)
		}
	})

	t.Run("group_panic_converts_to_error_without_hanging", func(t *testing.T) {
		g := newGroup(context.Background())
		g.goRun(Workload{Name: "boom", Run: func(context.Context) error {
			panic("kaboom")
		}})

		done := make(chan error, 1)
		go func() { done <- g.wait() }()

		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "kaboom") {
				t.Fatalf("group.wait() = %v, want an error mentioning the panic value", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("group.wait() hung after a workload panicked")
		}
	})

	t.Run("shutdown_runs_steps_in_declared_order", func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		record := func(name string) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		}
		steps := []ShutdownStep{
			{Name: "a", Run: func(context.Context) error { record("a"); return nil }},
			{Name: "b", Run: func(context.Context) error { record("b"); return nil }},
			{Name: "c", Run: func(context.Context) error { record("c"); return nil }},
		}

		if err := runShutdown(context.Background(), nil, steps, time.Second); err != nil {
			t.Fatalf("runShutdown: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"a", "b", "c"}) {
			t.Fatalf("shutdown order = %v, want [a b c]", order)
		}
	})

	t.Run("shutdown_stops_early_on_step_error", func(t *testing.T) {
		var ran []string
		steps := []ShutdownStep{
			{Name: "a", Run: func(context.Context) error { ran = append(ran, "a"); return nil }},
			{Name: "b", Run: func(context.Context) error { ran = append(ran, "b"); return errors.New("b failed") }},
			{Name: "c", Run: func(context.Context) error { ran = append(ran, "c"); return nil }},
		}
		err := runShutdown(context.Background(), nil, steps, time.Second)
		if err == nil || !strings.Contains(err.Error(), "b failed") {
			t.Fatalf("runShutdown error = %v, want it to mention step b's failure", err)
		}
		if !reflect.DeepEqual(ran, []string{"a", "b"}) {
			t.Fatalf("ran = %v, want [a b] (c must not run after b fails)", ran)
		}
	})

	t.Run("shutdown_hard_deadline_returns_without_hanging", func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)

		steps := []ShutdownStep{
			{Name: "stubborn", Run: func(ctx context.Context) error {
				// Deliberately ignores ctx to prove the deadline still
				// makes runShutdown return promptly.
				<-release
				return nil
			}},
		}

		start := time.Now()
		err := runShutdown(context.Background(), nil, steps, 50*time.Millisecond)
		elapsed := time.Since(start)

		var deadlineErr *ErrShutdownDeadlineExceeded
		if !errors.As(err, &deadlineErr) {
			t.Fatalf("runShutdown error = %v, want *ErrShutdownDeadlineExceeded", err)
		}
		if elapsed > 2*time.Second {
			t.Fatalf("runShutdown took %s to honor a 50ms deadline", elapsed)
		}
	})

	t.Run("exit_code_mapping", func(t *testing.T) {
		cases := []struct {
			name string
			err  error
			want int
		}{
			{"nil", nil, ExitOK},
			{"config_error", &ConfigError{Err: errors.New("bad flag")}, ExitConfigError},
			{"wrapped_config_error", errWrap(&ConfigError{Err: errors.New("bad flag")}), ExitConfigError},
			{"shutdown_deadline", &ErrShutdownDeadlineExceeded{Deadline: time.Second}, ExitShutdownTimeout},
			{"generic_error", errors.New("something else"), ExitRuntimeError},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if got := ExitCodeFor(c.err); got != c.want {
					t.Fatalf("ExitCodeFor(%v) = %d, want %d", c.err, got, c.want)
				}
			})
		}
	})
}

// errWrap wraps err the way a real call site would (fmt.Errorf("...: %w",
// err)), without importing fmt just for this one test helper.
func errWrap(err error) error { return &wrapped{err} }

type wrapped struct{ err error }

func (w *wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w *wrapped) Unwrap() error { return w.err }
