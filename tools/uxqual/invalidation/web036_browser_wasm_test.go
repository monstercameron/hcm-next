//go:build js && wasm

package invalidation

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"
)

// TestTodo_WEB_036_Browser exercises reconnect catch-up around the production
// syscall/js WebSocket adapter under Node's real Go js/wasm runtime.
func TestTodo_WEB_036_Browser(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000110")
	socket := newControlledBrowserSocket(t)
	stream, err := openBrowserStream(
		"wss://cell.example/invalidation",
		browserOrigin{protocol: "https:", host: "cell.example"},
		BrowserStreamOptions{MaxMessageBytes: DefaultMaxMessageBytes, MaxQueued: 2},
		func(string) (js.Value, error) { return socket.value, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	var caughtUp = make(chan CatchUpRequest, 1)
	client, err := New(testScope(subject), func(context.Context, Refresh) error { return nil }, Options{})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- client.RunReconnect(context.Background(), func(_ context.Context, cursor Cursor) (CloseStream, error) {
			if cursor.Sequence() != 10 {
				t.Errorf("browser factory cursor = %+v, want 10", cursor)
			}
			return stream, nil
		}, func(_ context.Context, request CatchUpRequest) (CatchUpResult, error) {
			caughtUp <- request
			return CatchUpResult{SourceSequence: request.ToSequence, Watermark: request.ToSequence}, nil
		}, ReconnectOptions{MaxAttempts: 2})
	}()
	socket.fire(t, "message", js.ValueOf(string(testMessage(t, 12, subject))))
	select {
	case request := <-caughtUp:
		if request.From.Sequence() != 10 || request.ToSequence != 11 {
			t.Fatalf("browser catch-up request = %+v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("browser gap did not invoke catch-up")
	}
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case terminal := <-result:
		if !errors.Is(terminal, context.Canceled) {
			t.Fatalf("browser reconnect terminal = %v, want cancellation", terminal)
		}
	case <-time.After(time.Second):
		t.Fatal("browser reconnect did not stop after Close")
	}
	if got := client.Snapshot(); got.LastSourceSequence != 12 || got.CatchUps != 1 {
		t.Fatalf("browser snapshot = %+v, want committed catch-up and hint", got)
	}
	closeCalls, removeCalls, listeners := waitBrowserCleanup(t, socket, 1)
	if closeCalls != 1 || removeCalls != 3 || listeners != 0 {
		t.Fatalf("browser cleanup = close:%d remove:%d listeners:%d", closeCalls, removeCalls, listeners)
	}
}
