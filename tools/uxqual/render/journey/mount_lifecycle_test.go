//go:build !js || !wasm

package journey

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"golang.org/x/net/html"
)

func TestTodo_WEB_027(t *testing.T) {
	m := NewMountLifecycle()
	var renders, cleanups int
	if err := m.Mount(RootSelector, func() error { renders++; return nil }, func() { cleanups++ }); err != nil {
		t.Fatal(err)
	}
	if m.State() != MountMounted || renders != 1 {
		t.Fatalf("state=%s renders=%d", m.State(), renders)
	}

	m.Stop()
	if m.State() != MountStopped || cleanups != 1 {
		t.Fatalf("after stop: state=%s cleanups=%d", m.State(), cleanups)
	}

	// Stopped records the previous outcome; it does not poison this lifecycle.
	if err := m.Mount(RootSelector, func() error { renders++; return nil }, func() { cleanups++ }); err != nil {
		t.Fatalf("remount: %v", err)
	}
	if m.State() != MountMounted || renders != 2 {
		t.Fatalf("after remount: state=%s renders=%d", m.State(), renders)
	}
	m.Stop()
}

func TestTodo_WEB_027_Golden(t *testing.T) {
	m := NewMountLifecycle()
	var renders, cleanups int
	snapshot := func(label string) string {
		return fmt.Sprintf("%s state=%s selector=%q renders=%d cleanups=%d", label, m.State(), m.Selector(), renders, cleanups)
	}
	got := []string{snapshot("initial")}

	if err := m.Mount(RootSelector, func() error { renders++; return nil }, func() { cleanups++ }); err != nil {
		t.Fatal(err)
	}
	got = append(got, snapshot("mounted"))
	if err := m.Mount(RootSelector, func() error { renders++; return nil }, func() { cleanups++ }); err != nil {
		t.Fatal(err)
	}
	got = append(got, snapshot("duplicate"))
	m.Stop()
	got = append(got, snapshot("stopped"))

	want := strings.Join([]string{
		`initial state=idle selector="" renders=0 cleanups=0`,
		`mounted state=mounted selector="#app" renders=1 cleanups=0`,
		`duplicate state=mounted selector="#app" renders=1 cleanups=0`,
		`stopped state=stopped selector="#app" renders=1 cleanups=1`,
	}, "\n")
	if joined := strings.Join(got, "\n"); joined != want {
		t.Fatalf("lifecycle golden mismatch\n--- got ---\n%s\n--- want ---\n%s", joined, want)
	}
}

// The actual js/wasm lifecycle oracle is TestTodo_WEB_027_Browser in
// mount_wasm_test.go. This native contract test still parses the rendered DOM
// structure rather than accepting substring matches over an HTML string.
func TestTodo_WEB_027_BrowserContract(t *testing.T) {
	markup, err := ui.RenderToString(Build(SampleListPage()))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}

	var mains, h1s, labelledNavs, liveRegions int
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		switch n.Data {
		case "main":
			mains++
		case "h1":
			h1s++
		case "nav":
			if htmlAttr(n, "aria-label") != "" {
				labelledNavs++
			}
		}
		if htmlAttr(n, "aria-live") != "" || htmlAttr(n, "role") == "status" || htmlAttr(n, "role") == "alert" {
			liveRegions++
		}
	})
	if mains != 1 || h1s != 1 || labelledNavs == 0 || liveRegions == 0 {
		t.Fatalf("semantic DOM: main=%d h1=%d labelled-nav=%d live-regions=%d", mains, h1s, labelledNavs, liveRegions)
	}
}

func TestTodo_WEB_027_Conformance(t *testing.T) {
	t.Run("mounted duplicate is idempotent", func(t *testing.T) {
		m := NewMountLifecycle()
		calls := 0
		render := func() error { calls++; return nil }
		if err := m.Mount(RootSelector, render, nil); err != nil {
			t.Fatal(err)
		}
		if err := m.Mount(RootSelector, render, nil); err != nil {
			t.Fatal("duplicate should be idempotent: ", err)
		}
		if calls != 1 {
			t.Fatalf("duplicate rendered %d times", calls)
		}
	})

	t.Run("concurrent duplicate receives first failure", func(t *testing.T) {
		m := NewMountLifecycle()
		started := make(chan struct{})
		release := make(chan struct{})
		var calls atomic.Int32
		first := make(chan error, 1)
		go func() {
			first <- m.Mount(RootSelector, func() error {
				calls.Add(1)
				close(started)
				<-release
				return errors.New("private adapter detail")
			}, nil)
		}()
		<-started

		second := make(chan error, 1)
		go func() {
			second <- m.Mount(RootSelector, func() error {
				calls.Add(1)
				return nil
			}, nil)
		}()
		select {
		case err := <-second:
			t.Fatalf("duplicate returned before the first outcome: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
		close(release)
		for i, result := range []<-chan error{first, second} {
			if err := receiveWithin(t, result); !errors.Is(err, ErrMountFailed) {
				t.Fatalf("caller %d error=%v, want ErrMountFailed", i, err)
			}
		}
		if calls.Load() != 1 {
			t.Fatalf("render calls=%d, want 1", calls.Load())
		}
	})
}

func TestTodo_WEB_027_Security(t *testing.T) {
	for _, selector := range []string{"", "#other", " #app", "#app ", "#app[data-user='pii']", "#app,body"} {
		m := NewMountLifecycle()
		if err := m.Mount(selector, func() error { return nil }, nil); !errors.Is(err, ErrInvalidSelector) {
			t.Errorf("selector %q error=%v, want ErrInvalidSelector", selector, err)
		}
	}

	secret := "worker=Jane Rivera salary=999 bearer=private"
	for name, render := range map[string]func() error{
		"error": func() error { return errors.New(secret) },
		"panic": func() error { panic(secret) },
	} {
		t.Run(name, func(t *testing.T) {
			m := NewMountLifecycle()
			err := m.Mount(RootSelector, render, nil)
			if !errors.Is(err, ErrMountFailed) {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatal("mount error leaked adapter detail")
			}
		})
	}
}

func TestTodo_WEB_027_Integration(t *testing.T) {
	store := NewStore(SampleListPage())
	m := NewMountLifecycle()
	if err := m.Mount(RootSelector, func() error {
		_, err := ui.RenderToString(LiveComponent(store))
		return err
	}, nil); err != nil {
		t.Fatal(err)
	}
	store.Set(SampleDetailPage())
	if store.Page().Detail == nil || m.State() != MountMounted {
		t.Fatal("live store was not retained")
	}
	m.Stop()
	if err := m.Mount(RootSelector, func() error {
		_, err := ui.RenderToString(LiveComponent(store))
		return err
	}, nil); err != nil {
		t.Fatalf("same lifecycle could not recover after host teardown: %v", err)
	}
}

func TestTodo_WEB_027_Fault(t *testing.T) {
	t.Run("failed render rolls back once and can retry", func(t *testing.T) {
		m := NewMountLifecycle()
		var cleaned atomic.Int32
		if err := m.Mount(RootSelector, func() error { return errors.New("adapter detail") }, func() { cleaned.Add(1) }); !errors.Is(err, ErrMountFailed) {
			t.Fatal(err)
		}
		if m.State() != MountFailed || cleaned.Load() != 1 {
			t.Fatalf("state=%s cleanup=%d", m.State(), cleaned.Load())
		}
		if err := m.Mount(RootSelector, func() error { return nil }, func() { cleaned.Add(1) }); err != nil {
			t.Fatalf("retry after failure: %v", err)
		}
		m.Stop()
		m.Stop()
		if m.State() != MountStopped || cleaned.Load() != 2 {
			t.Fatalf("state=%s cleanup=%d", m.State(), cleaned.Load())
		}
	})

	t.Run("stop wins a rendering race", func(t *testing.T) {
		m := NewMountLifecycle()
		started := make(chan struct{})
		release := make(chan struct{})
		mountResult := make(chan error, 1)
		var cleaned atomic.Int32
		go func() {
			mountResult <- m.Mount(RootSelector, func() error {
				close(started)
				<-release
				return nil
			}, func() { cleaned.Add(1) })
		}()
		<-started
		stopped := make(chan struct{})
		go func() { m.Stop(); close(stopped) }()
		waitForStopRequest(t, m)
		close(release)
		if err := receiveWithin(t, mountResult); !errors.Is(err, ErrMountStopped) {
			t.Fatalf("mount result=%v, want ErrMountStopped", err)
		}
		receiveSignalWithin(t, stopped)
		if m.State() != MountStopped || cleaned.Load() != 1 {
			t.Fatalf("state=%s cleanup=%d", m.State(), cleaned.Load())
		}
	})

	t.Run("remount waits for teardown cleanup", func(t *testing.T) {
		m := NewMountLifecycle()
		cleanupStarted := make(chan struct{})
		cleanupRelease := make(chan struct{})
		if err := m.Mount(RootSelector, func() error { return nil }, func() {
			close(cleanupStarted)
			<-cleanupRelease
		}); err != nil {
			t.Fatal(err)
		}
		stopped := make(chan struct{})
		go func() { m.Stop(); close(stopped) }()
		<-cleanupStarted

		var remountRendered atomic.Bool
		remounted := make(chan error, 1)
		go func() {
			remounted <- m.Mount(RootSelector, func() error {
				remountRendered.Store(true)
				return nil
			}, nil)
		}()
		if remountRendered.Load() {
			t.Fatal("remount rendered before prior cleanup completed")
		}
		close(cleanupRelease)
		receiveSignalWithin(t, stopped)
		if err := receiveWithin(t, remounted); err != nil {
			t.Fatal(err)
		}
		if !remountRendered.Load() || m.State() != MountMounted {
			t.Fatalf("remount rendered=%t state=%s", remountRendered.Load(), m.State())
		}
	})

	t.Run("cleanup panic is contained", func(t *testing.T) {
		m := NewMountLifecycle()
		if err := m.Mount(RootSelector, func() error { return nil }, func() { panic("private cleanup detail") }); err != nil {
			t.Fatal(err)
		}
		m.Stop()
		if m.State() != MountStopped {
			t.Fatalf("state=%s", m.State())
		}
	})
}

func TestMountLifecycleConcurrentStopIsIdempotent(t *testing.T) {
	m := NewMountLifecycle()
	var cleanups atomic.Int32
	if err := m.Mount(RootSelector, func() error { return nil }, func() { cleanups.Add(1) }); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Stop()
		}()
	}
	wg.Wait()
	if cleanups.Load() != 1 || m.State() != MountStopped {
		t.Fatalf("cleanups=%d state=%s", cleanups.Load(), m.State())
	}
}

func TestMountLifecycleAllocationBudget(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		m := NewMountLifecycle()
		if err := m.Mount(RootSelector, func() error { return nil }, nil); err != nil {
			panic(err)
		}
	})
	if allocs > 6 {
		t.Fatalf("mount allocations=%.1f, budget=6", allocs)
	}
}

func BenchmarkMountLifecycle(b *testing.B) {
	for b.Loop() {
		m := NewMountLifecycle()
		if err := m.Mount(RootSelector, func() error { return nil }, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func receiveWithin(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lifecycle result")
		return nil
	}
}

func receiveSignalWithin(t *testing.T, result <-chan struct{}) {
	t.Helper()
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lifecycle signal")
	}
}

func waitForStopRequest(t *testing.T, m *MountLifecycle) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		requested := m.attempt != nil && m.attempt.stopRequested
		m.mu.Unlock()
		if requested {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for Stop to request teardown")
}

func walkHTML(n *html.Node, visit func(*html.Node)) {
	if n == nil {
		return
	}
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walkHTML(child, visit)
	}
}

func htmlAttr(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
