//go:build js && wasm

package invalidation

import (
	"context"
	"errors"
	"io"
	"sync"
	"syscall/js"
	"testing"
	"time"
)

type controlledBrowserSocket struct {
	mu          sync.Mutex
	value       js.Value
	handlers    map[string]js.Value
	add         js.Func
	remove      js.Func
	close       js.Func
	closeCalls  int
	removeCalls int
	syncClose   bool
}

func newControlledBrowserSocket(t *testing.T) *controlledBrowserSocket {
	t.Helper()
	socket := &controlledBrowserSocket{handlers: map[string]js.Value{}}
	socket.value = js.Global().Get("Object").New()
	socket.add = js.FuncOf(func(_ js.Value, args []js.Value) any {
		socket.mu.Lock()
		socket.handlers[args[0].String()] = args[1]
		socket.mu.Unlock()
		return nil
	})
	socket.remove = js.FuncOf(func(_ js.Value, args []js.Value) any {
		socket.mu.Lock()
		name := args[0].String()
		if current, ok := socket.handlers[name]; ok && current.Equal(args[1]) {
			delete(socket.handlers, name)
			socket.removeCalls++
		}
		socket.mu.Unlock()
		return nil
	})
	socket.close = js.FuncOf(func(js.Value, []js.Value) any {
		socket.mu.Lock()
		socket.closeCalls++
		handler, fire := socket.handlers["close"]
		fire = fire && socket.syncClose
		socket.mu.Unlock()
		if fire {
			handler.Invoke(js.Global().Get("Object").New())
		}
		return nil
	})
	socket.value.Set("addEventListener", socket.add)
	socket.value.Set("removeEventListener", socket.remove)
	socket.value.Set("close", socket.close)
	t.Cleanup(func() {
		socket.add.Release()
		socket.remove.Release()
		socket.close.Release()
	})
	return socket
}

func (s *controlledBrowserSocket) fire(t *testing.T, name string, data js.Value) {
	t.Helper()
	s.mu.Lock()
	handler, ok := s.handlers[name]
	s.mu.Unlock()
	if !ok {
		t.Fatalf("socket has no %q listener", name)
	}
	event := js.Global().Get("Object").New()
	if data.Type() != js.TypeUndefined {
		event.Set("data", data)
	}
	handler.Invoke(event)
}

func (s *controlledBrowserSocket) counts() (closeCalls, removeCalls, listeners int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeCalls, s.removeCalls, len(s.handlers)
}

// TestTodo_WEB_035_Browser runs under Node's real Go js/wasm runtime and
// drives the production syscall/js adapter through a controlled WebSocket API
// object. No DOM, dynamic code execution, network endpoint, or
// authority-bearing URL is used.
func TestTodo_WEB_035_Browser(t *testing.T) {
	subject := testSubject("00000000-0000-4000-8000-000000000022")
	raw := testMessage(t, 11, subject)
	socket := newControlledBrowserSocket(t)
	var openedURL string
	stream, err := openBrowserStream(
		"wss://cell.example/invalidation",
		browserOrigin{protocol: "https:", host: "cell.example"},
		BrowserStreamOptions{MaxMessageBytes: len(raw), MaxQueued: 2},
		func(rawURL string) (js.Value, error) {
			openedURL = rawURL
			return socket.value, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var refreshed Refresh
	client, err := New(testScope(subject), func(_ context.Context, refresh Refresh) error {
		refreshed = refresh
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	done, err := client.Start(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	socket.fire(t, "message", js.ValueOf(string(raw)))
	waitSnapshot(t, client, func(snapshot Snapshot) bool { return snapshot.Refetched == 1 })
	socket.fire(t, "close", js.Undefined())
	select {
	case terminal := <-done:
		if terminal != nil {
			t.Fatalf("remote close = %v, want clean EOF", terminal)
		}
	case <-time.After(time.Second):
		t.Fatal("browser stream did not finish")
	}
	if openedURL != "wss://cell.example/invalidation" {
		t.Fatalf("opened URL = %q", openedURL)
	}
	if refreshed.SourceSequence != 11 || len(refreshed.Subjects) != 1 || refreshed.Subjects[0] != subject {
		t.Fatalf("authoritative refetch hint = %+v", refreshed)
	}
	closeCalls, removeCalls, listeners := waitBrowserCleanup(t, socket, 0)
	if closeCalls != 0 || removeCalls != 3 || listeners != 0 {
		t.Fatalf("remote cleanup = close:%d remove:%d listeners:%d, want 0/3/0", closeCalls, removeCalls, listeners)
	}
}

func TestBrowserStreamRejectsUnsafeURLBeforeConstruction(t *testing.T) {
	for _, rawURL := range []string{
		"wss://other.example/invalidation",
		"ws://cell.example/invalidation",
		"wss://user:secret@cell.example/invalidation",
		"wss://cell.example/invalidation?bearer=secret",
		"wss://cell.example/invalidation#subject",
		"wss://cell.example",
	} {
		called := false
		_, err := openBrowserStream(rawURL, browserOrigin{protocol: "https:", host: "cell.example"}, BrowserStreamOptions{}, func(string) (js.Value, error) {
			called = true
			return js.Undefined(), nil
		})
		if !errors.Is(err, ErrBrowserSocketURL) || err.Error() != ErrBrowserSocketURL.Error() {
			t.Errorf("openBrowserStream(%q) error = %v, want exact URL sentinel", rawURL, err)
		}
		if called {
			t.Errorf("unsafe URL %q reached socket constructor", rawURL)
		}
	}
}

func TestBrowserStreamFailsClosedOnUnsupportedOversizedAndOverflowFrames(t *testing.T) {
	tests := []struct {
		name string
		fire func(*testing.T, *controlledBrowserSocket)
		want error
	}{
		{
			name: "binary",
			fire: func(t *testing.T, socket *controlledBrowserSocket) {
				socket.fire(t, "message", js.Global().Get("Uint8Array").New(3))
			},
			want: ErrBrowserSocketFrame,
		},
		{
			name: "oversized",
			fire: func(t *testing.T, socket *controlledBrowserSocket) {
				socket.fire(t, "message", js.ValueOf("12345"))
			},
			want: ErrBrowserSocketFrame,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			socket := newControlledBrowserSocket(t)
			stream, err := openBrowserStream("wss://cell.example/invalidation", browserOrigin{protocol: "https:", host: "cell.example"}, BrowserStreamOptions{MaxMessageBytes: 4, MaxQueued: 1}, func(string) (js.Value, error) {
				return socket.value, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			tc.fire(t, socket)
			if _, err := stream.Recv(); !errors.Is(err, tc.want) || err.Error() != tc.want.Error() {
				t.Fatalf("Recv error = %v, want exact %v", err, tc.want)
			}
			closeCalls, removeCalls, listeners := waitBrowserCleanup(t, socket, 1)
			if closeCalls != 1 || removeCalls != 3 || listeners != 0 {
				t.Fatalf("failure cleanup = close:%d remove:%d listeners:%d", closeCalls, removeCalls, listeners)
			}
		})
	}

	socket := newControlledBrowserSocket(t)
	stream, err := openBrowserStream("wss://cell.example/invalidation", browserOrigin{protocol: "https:", host: "cell.example"}, BrowserStreamOptions{MaxMessageBytes: 16, MaxQueued: 1}, func(string) (js.Value, error) {
		return socket.value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	socket.fire(t, "message", js.ValueOf(`{"one":1}`))
	socket.fire(t, "message", js.ValueOf(`{"two":2}`))
	if _, err := stream.Recv(); !errors.Is(err, ErrBrowserReceiveQueueFull) {
		t.Fatalf("overflow Recv error = %v, want %v", err, ErrBrowserReceiveQueueFull)
	}
}

func TestBrowserStreamCloseIsIdempotentAndUnblocksRecv(t *testing.T) {
	socket := newControlledBrowserSocket(t)
	stream, err := openBrowserStream("ws://localhost:8080/invalidation", browserOrigin{protocol: "http:", host: "localhost:8080"}, BrowserStreamOptions{}, func(string) (js.Value, error) {
		return socket.value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	socket.mu.Lock()
	socket.syncClose = true
	socket.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		_, recvErr := stream.Recv()
		result <- recvErr
	}()
	closed := make(chan error, 1)
	go func() { closed <- stream.Close() }()
	select {
	case closeErr := <-closed:
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("synchronous close event re-entered shutdown")
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case recvErr := <-result:
		if !errors.Is(recvErr, io.EOF) {
			t.Fatalf("blocked Recv error = %v, want EOF", recvErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock Recv")
	}
	closeCalls, removeCalls, listeners := waitBrowserCleanup(t, socket, 1)
	if closeCalls != 1 || removeCalls != 3 || listeners != 0 {
		t.Fatalf("local cleanup = close:%d remove:%d listeners:%d, want 1/3/0", closeCalls, removeCalls, listeners)
	}
}

func waitBrowserCleanup(t *testing.T, socket *controlledBrowserSocket, wantClose int) (int, int, int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		closeCalls, removeCalls, listeners := socket.counts()
		if closeCalls == wantClose && removeCalls == 3 && listeners == 0 {
			return closeCalls, removeCalls, listeners
		}
		time.Sleep(time.Millisecond)
	}
	return socket.counts()
}

func TestBrowserStreamOptionsStayWithinCanonicalBounds(t *testing.T) {
	socket := newControlledBrowserSocket(t)
	stream, err := openBrowserStream("wss://cell.example/invalidation", browserOrigin{protocol: "https:", host: "cell.example"}, BrowserStreamOptions{MaxMessageBytes: DefaultMaxMessageBytes + 1, MaxQueued: MaxQueue + 1}, func(string) (js.Value, error) {
		return socket.value, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if stream.maxBytes != DefaultMaxMessageBytes || cap(stream.messages) != DefaultMaxQueue {
		t.Fatalf("browser bounds = bytes:%d queue:%d", stream.maxBytes, cap(stream.messages))
	}
}
