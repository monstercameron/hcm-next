package ephemeralenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
)

// TestEnvironmentIsolation is TOOL-014's non-PostgreSQL primary proof. The
// pgtest package proves the relational half; this test proves objects, queues,
// and provider fakes cannot share mutable state across two concurrent runs.
func TestEnvironmentIsolation(t *testing.T) {
	base := t.TempDir()
	first, err := Provision(Config{BaseDir: base})
	if err != nil {
		t.Fatalf("provision first environment: %v", err)
	}
	defer closeEnvironment(t, first)
	second, err := Provision(Config{BaseDir: base})
	if err != nil {
		t.Fatalf("provision second environment: %v", err)
	}
	defer closeEnvironment(t, second)

	if first.RunID == second.RunID || first.ObjectNamespace == second.ObjectNamespace || first.QueueNamespace == second.QueueNamespace || first.ProviderNamespace == second.ProviderNamespace {
		t.Fatalf("two environments share a namespace: first=%+v second=%+v", first.Evidence(), second.Evidence())
	}
	if err := first.PutObject("worker/1.json", []byte(`{"id":"first"}`)); err != nil {
		t.Fatalf("write first object: %v", err)
	}
	if err := first.Enqueue("first-message"); err != nil {
		t.Fatalf("enqueue first message: %v", err)
	}
	if err := first.SetProviderResponse("worker/1", "first-response"); err != nil {
		t.Fatalf("set first provider response: %v", err)
	}

	if _, err := second.GetObject("worker/1.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second environment read first object's state: %v", err)
	}
	if n, err := second.QueueLen(); err != nil || n != 0 {
		t.Fatalf("second environment queue = %d, %v; want empty isolated queue", n, err)
	}
	if response, ok, err := second.ProviderResponse("worker/1"); err != nil || ok || response != "" {
		t.Fatalf("second environment provider = %q, %v, %v; want no first response", response, ok, err)
	}
}

// TestTodo_TOOL_014_Golden pins the opaque namespace convention and the
// cleanup evidence fields consumed by diagnostics after a run disappears.
func TestTodo_TOOL_014_Golden(t *testing.T) {
	env, err := Provision(Config{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatalf("provision environment: %v", err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(env.RunID) {
		t.Fatalf("run ID %q is not 32 lowercase hex characters", env.RunID)
	}
	if want := "object-" + env.RunID; env.ObjectNamespace != want {
		t.Fatalf("object namespace = %q, want %q", env.ObjectNamespace, want)
	}
	if want := "queue-" + env.RunID; env.QueueNamespace != want {
		t.Fatalf("queue namespace = %q, want %q", env.QueueNamespace, want)
	}
	if want := "provider-" + env.RunID; env.ProviderNamespace != want {
		t.Fatalf("provider namespace = %q, want %q", env.ProviderNamespace, want)
	}
	if err := env.PutObject("one", []byte("1")); err != nil {
		t.Fatalf("put first object: %v", err)
	}
	if err := env.PutObject("nested/two", []byte("2")); err != nil {
		t.Fatalf("put second object: %v", err)
	}
	if err := env.Enqueue("message"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := env.SetProviderResponse("a", "one"); err != nil {
		t.Fatalf("set first fake response: %v", err)
	}
	if err := env.SetProviderResponse("b", "two"); err != nil {
		t.Fatalf("set second fake response: %v", err)
	}
	root := env.Root()
	if err := env.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	evidence := env.Evidence()
	if evidence.ObjectFileCount != 2 || evidence.QueueMessageCount != 1 || evidence.ProviderResponseCount != 2 || !evidence.RootRemoved || !evidence.ClaimsReleased || evidence.Preserved || evidence.CleanupError != "" || evidence.CleanedAt.IsZero() {
		t.Fatalf("cleanup evidence = %+v, want two objects/one queue/two provider calls and successful removal", evidence)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run root %s exists after cleanup: %v", root, err)
	}
}

// TestTodo_TOOL_014_Race provisions and removes many environments at once.
// It stresses both the live-namespace registry and filesystem ownership
// without involving PostgreSQL's already-qualified pgtest harness.
func TestTodo_TOOL_014_Race(t *testing.T) {
	base := t.TempDir()
	const workers = 32
	var wg sync.WaitGroup
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			env, err := Provision(Config{BaseDir: base})
			if err != nil {
				errs <- fmt.Errorf("provision worker %d: %w", i, err)
				return
			}
			if err := env.PutObject("result", []byte(fmt.Sprintf("worker-%d", i))); err != nil {
				errs <- fmt.Errorf("write worker %d object: %w", i, err)
				return
			}
			if err := env.Enqueue(fmt.Sprintf("queue-%d", i)); err != nil {
				errs <- fmt.Errorf("enqueue worker %d: %w", i, err)
				return
			}
			if err := env.SetProviderResponse("result", fmt.Sprintf("provider-%d", i)); err != nil {
				errs <- fmt.Errorf("set worker %d provider response: %w", i, err)
				return
			}
			root := env.Root()
			if err := env.Close(); err != nil {
				errs <- fmt.Errorf("close worker %d: %w", i, err)
				return
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				errs <- fmt.Errorf("worker %d root remains after cleanup: %v", i, err)
				return
			}
			ids <- env.RunID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("run ID %q was allocated twice", id)
		}
		seen[id] = true
	}
	if len(seen) != workers {
		t.Fatalf("completed %d isolated environments, want %d", len(seen), workers)
	}
}

// TestTodo_TOOL_014_Integration exercises the testing.TB convenience API and
// proves it captures evidence before deleting its private filesystem state.
func TestTodo_TOOL_014_Integration(t *testing.T) {
	var env *Environment
	t.Run("owned test cleanup", func(t *testing.T) {
		env = New(t, Config{BaseDir: t.TempDir()})
		if err := env.PutObject("receipt.json", []byte(`{"receipt":"ok"}`)); err != nil {
			t.Fatalf("put object: %v", err)
		}
		if err := env.Enqueue("outbox-entry"); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if err := env.SetProviderResponse("send", "accepted"); err != nil {
			t.Fatalf("set provider response: %v", err)
		}
	})
	if env == nil {
		t.Fatal("child test did not create an environment")
	}
	evidence := env.Evidence()
	if !evidence.RootRemoved || !evidence.ClaimsReleased || evidence.ObjectFileCount != 1 || evidence.QueueMessageCount != 1 || evidence.ProviderResponseCount != 1 {
		t.Fatalf("testing cleanup evidence = %+v", evidence)
	}
}

// TestTodo_TOOL_014_Security proves shared-name configuration and filesystem
// escape attempts fail closed, while cleanup is confined to a harness-owned
// child directory and can never remove an unrelated sibling.
func TestTodo_TOOL_014_Security(t *testing.T) {
	t.Run("shared live object namespace is refused", func(t *testing.T) {
		base := t.TempDir()
		first, err := Provision(Config{BaseDir: base, ObjectNamespace: "object-shared"})
		if err != nil {
			t.Fatalf("provision first environment: %v", err)
		}
		defer closeEnvironment(t, first)
		if _, err := Provision(Config{BaseDir: base, ObjectNamespace: "object-shared"}); !errors.Is(err, ErrSharedNamespace) {
			t.Fatalf("shared namespace provision error = %v, want ErrSharedNamespace", err)
		}
	})

	t.Run("path-shaped namespace is refused before creating a run root", func(t *testing.T) {
		base := t.TempDir()
		if _, err := Provision(Config{BaseDir: base, ObjectNamespace: "../outside"}); !errors.Is(err, ErrInvalidNamespace) {
			t.Fatalf("unsafe namespace provision error = %v, want ErrInvalidNamespace", err)
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatalf("read base directory: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("unsafe namespace created state under %s: %v", base, entries)
		}
	})

	t.Run("object keys cannot escape the private namespace", func(t *testing.T) {
		env, err := Provision(Config{BaseDir: t.TempDir()})
		if err != nil {
			t.Fatalf("provision environment: %v", err)
		}
		defer closeEnvironment(t, env)
		for _, key := range []string{"../outside", `..\\outside`, "/outside"} {
			if err := env.PutObject(key, []byte("forbidden")); !errors.Is(err, ErrInvalidObjectKey) {
				t.Errorf("PutObject(%q) error = %v, want ErrInvalidObjectKey", key, err)
			}
		}
	})

	t.Run("cleanup removes only its owned root", func(t *testing.T) {
		base := t.TempDir()
		sentinel := filepath.Join(base, "do-not-delete.txt")
		if err := os.WriteFile(sentinel, []byte("sibling"), 0o600); err != nil {
			t.Fatalf("write sentinel: %v", err)
		}
		env, err := Provision(Config{BaseDir: base})
		if err != nil {
			t.Fatalf("provision environment: %v", err)
		}
		if err := env.PutObject("object", []byte("private")); err != nil {
			t.Fatalf("put private object: %v", err)
		}
		if err := env.Close(); err != nil {
			t.Fatalf("close environment: %v", err)
		}
		if got, err := os.ReadFile(sentinel); err != nil || string(got) != "sibling" {
			t.Fatalf("sentinel after cleanup = %q, %v; cleanup reached a sibling", got, err)
		}
	})
}

func closeEnvironment(t *testing.T, env *Environment) {
	t.Helper()
	if err := env.Close(); err != nil {
		t.Errorf("close environment %s: %v", env.RunID, err)
	}
}
