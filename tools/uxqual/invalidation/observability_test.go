package invalidation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvalidationObserverIsolation(t *testing.T) {
	t.Run("panic is contained and ordering continues", func(t *testing.T) {
		subject := testSubject("00000000-0000-4000-8000-000000000015")
		seen := make(chan EventKind, 3)
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{Observe: func(event Event) {
			seen <- event.Kind
			if event.Kind == EventAccepted {
				panic("hostile observer")
			}
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Run(context.Background(), &sliceStream{values: [][]byte{testMessage(t, 11, subject)}}); err != nil {
			t.Fatal(err)
		}
		for i, want := range []EventKind{EventAccepted, EventRefetched, EventClosed} {
			select {
			case got := <-seen:
				if got != want {
					t.Fatalf("event[%d] = %q, want %q", i, got, want)
				}
			case <-time.After(time.Second):
				t.Fatalf("event[%d] was not delivered after observer panic", i)
			}
		}
	})

	t.Run("block is serial bounded and does not hold lifecycle", func(t *testing.T) {
		subject := testSubject("00000000-0000-4000-8000-000000000016")
		observerStarted := make(chan struct{})
		releaseObserver := make(chan struct{})
		var calls atomic.Int32
		var active atomic.Int32
		var concurrent atomic.Bool
		client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{Observe: func(Event) {
			if active.Add(1) != 1 {
				concurrent.Store(true)
			}
			if calls.Add(1) == 1 {
				close(observerStarted)
				<-releaseObserver
			}
			active.Add(-1)
		}})
		if err != nil {
			t.Fatal(err)
		}
		stream := newChannelStream(maxObserverQueue + 32)
		done, err := client.Start(context.Background(), stream)
		if err != nil {
			t.Fatal(err)
		}
		stream.messages <- []byte(`{}`)
		select {
		case <-observerStarted:
		case <-time.After(time.Second):
			t.Fatal("observer did not start")
		}
		for i := 0; i < maxObserverQueue+20; i++ {
			stream.messages <- []byte(`{}`)
		}
		waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Rejected == maxObserverQueue+21 })
		waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.ObserverDrops > 0 })
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("blocked observer held subscription shutdown")
		}
		if concurrent.Load() {
			t.Fatal("observer was invoked concurrently")
		}
		close(releaseObserver)
	})
}
