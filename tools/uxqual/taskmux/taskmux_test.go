package taskmux

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestSubmitIsNonBlockingAndBoundsConcurrentWork(t *testing.T) {
	scheduler := New(Options{MaxRunning: 2, MaxQueued: 8})
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var running, peak atomic.Int32
	for range 3 {
		_, err := scheduler.Submit(context.Background(), Spec{}, func(context.Context) error {
			current := running.Add(1)
			for {
				old := peak.Load()
				if current <= old || peak.CompareAndSwap(old, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			running.Add(-1)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	waitSignal(t, started)
	waitSignal(t, started)
	select {
	case <-started:
		t.Fatal("third task started before capacity was released")
	case <-time.After(20 * time.Millisecond):
	}
	if got := scheduler.Snapshot(); got.Running != 2 || got.Queued != 1 {
		t.Fatalf("snapshot = %+v, want 2 running and 1 queued", got)
	}
	close(release)
	waitSignal(t, started)
	if peak.Load() != 2 {
		t.Fatalf("peak concurrency = %d, want 2", peak.Load())
	}
}

func TestReplaceExistingCancelsRunningAndQueuedWork(t *testing.T) {
	scheduler := New(Options{MaxRunning: 1, MaxQueued: 8})
	started := make(chan struct{}, 2)
	first, err := scheduler.Submit(context.Background(), Spec{Key: "route", Duplicate: ReplaceExisting}, func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, started)
	second, err := scheduler.Submit(context.Background(), Spec{Key: "route", Duplicate: ReplaceExisting}, func(context.Context) error {
		started <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, started)
	if result := waitResult(t, first); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("first result = %v, want canceled", result.Err)
	}
	if result := waitResult(t, second); result.Err != nil {
		t.Fatalf("second result = %v", result.Err)
	}
}

func TestKeepExistingCoalescesDuplicateMutation(t *testing.T) {
	scheduler := New(Options{MaxRunning: 1})
	release := make(chan struct{})
	var calls atomic.Int32
	first, err := scheduler.Submit(context.Background(), Spec{Key: "approve:42", Duplicate: KeepExisting}, func(context.Context) error {
		calls.Add(1)
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := scheduler.Submit(context.Background(), Spec{Key: "approve:42", Duplicate: KeepExisting}, func(context.Context) error {
		calls.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Done() != second.Done() {
		t.Fatal("duplicate did not share the existing completion")
	}
	close(release)
	if result := waitResult(t, first); result.Err != nil {
		t.Fatal(result.Err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestQueuedTaskHonorsCallerCancellation(t *testing.T) {
	scheduler := New(Options{MaxRunning: 1})
	release := make(chan struct{})
	_, err := scheduler.Submit(context.Background(), Spec{}, func(context.Context) error {
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	handle, err := scheduler.Submit(ctx, Spec{}, func(context.Context) error {
		t.Fatal("canceled queued work ran")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	close(release)
	if result := waitResult(t, handle); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("result = %v, want canceled", result.Err)
	}
}

func TestInteractiveWorkPassesAQueuedBackgroundRefresh(t *testing.T) {
	scheduler := New(Options{MaxRunning: 1, MaxQueued: 8})
	release := make(chan struct{})
	started := make(chan string, 3)
	_, err := scheduler.Submit(context.Background(), Spec{}, func(context.Context) error {
		started <- "blocker"
		<-release
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitString(t, started); got != "blocker" {
		t.Fatalf("first task = %q", got)
	}
	_, _ = scheduler.Submit(context.Background(), Spec{Priority: Background}, func(context.Context) error {
		started <- "background"
		return nil
	})
	_, _ = scheduler.Submit(context.Background(), Spec{Priority: Interactive}, func(context.Context) error {
		started <- "interactive"
		return nil
	})
	close(release)
	if got := waitString(t, started); got != "interactive" {
		t.Fatalf("next task = %q, want interactive", got)
	}
	if got := waitString(t, started); got != "background" {
		t.Fatalf("last task = %q, want background", got)
	}
}

func TestQueueRefusesWorkBeyondItsBound(t *testing.T) {
	scheduler := New(Options{MaxRunning: 1, MaxQueued: 1})
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	_, _ = scheduler.Submit(context.Background(), Spec{}, func(context.Context) error {
		started <- struct{}{}
		<-release
		return nil
	})
	waitSignal(t, started)
	_, _ = scheduler.Submit(context.Background(), Spec{}, func(context.Context) error { return nil })
	if _, err := scheduler.Submit(context.Background(), Spec{}, func(context.Context) error { return nil }); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third submission = %v, want queue-full refusal", err)
	}
	close(release)
}

func waitSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task")
	}
}

func waitResult(t *testing.T, handle *Handle) Result {
	t.Helper()
	select {
	case <-handle.Done():
		return handle.Result()
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
		return Result{}
	}
}

func waitString(t *testing.T, signal <-chan string) string {
	t.Helper()
	select {
	case value := <-signal:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task label")
		return ""
	}
}
