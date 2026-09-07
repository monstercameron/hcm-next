//go:build js && wasm

package invalidation

import (
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"syscall/js"
)

var (
	ErrBrowserSocketUnavailable = errors.New("invalidation: browser socket is unavailable")
	ErrBrowserSocketURL         = errors.New("invalidation: browser socket URL is invalid")
	ErrBrowserSocketFrame       = errors.New("invalidation: browser socket frame is invalid")
	ErrBrowserReceiveQueueFull  = errors.New("invalidation: browser receive queue is full")
)

// BrowserStreamOptions bounds the browser-side queue before messages reach
// Client's independently bounded refresh queue.
type BrowserStreamOptions struct {
	MaxMessageBytes int
	MaxQueued       int
}

type browserOrigin struct {
	protocol string
	host     string
}

type browserSocketConstructor func(string) (js.Value, error)

// OpenBrowserStream opens an explicit same-origin WebSocket transport. This
// function deliberately accepts no Scope, credential, or authority material:
// the URL selects transport only, each message remains an untrusted hint, and
// Client plus the authoritative refetch enforce the security boundary.
//
// WEB-035 does not call this function from the product composition because the
// repository has no canonical product-invalidation endpoint yet. A later
// composition may inject the reviewed endpoint without changing this adapter.
func OpenBrowserStream(rawURL string, options BrowserStreamOptions) (CloseStream, error) {
	origin, err := currentBrowserOrigin()
	if err != nil {
		return nil, err
	}
	return openBrowserStream(rawURL, origin, options, constructBrowserSocket)
}

func openBrowserStream(rawURL string, origin browserOrigin, options BrowserStreamOptions, construct browserSocketConstructor) (*browserStream, error) {
	if err := validateBrowserSocketURL(rawURL, origin); err != nil {
		return nil, err
	}
	if construct == nil {
		return nil, ErrBrowserSocketUnavailable
	}
	maxBytes := options.MaxMessageBytes
	if maxBytes <= 0 || maxBytes > DefaultMaxMessageBytes {
		maxBytes = DefaultMaxMessageBytes
	}
	maxQueued := options.MaxQueued
	if maxQueued <= 0 || maxQueued > MaxQueue {
		maxQueued = DefaultMaxQueue
	}
	socket, err := construct(rawURL)
	if err != nil || socket.Type() != js.TypeObject || socket.IsNull() {
		return nil, ErrBrowserSocketUnavailable
	}
	stream := &browserStream{
		socket:   socket,
		maxBytes: maxBytes,
		messages: make(chan []byte, maxQueued),
		terminal: make(chan struct{}),
	}
	if err := stream.bind(); err != nil {
		stream.shutdown(ErrBrowserSocketUnavailable, true)
		stream.releaseListeners()
		return nil, ErrBrowserSocketUnavailable
	}
	return stream, nil
}

type browserStream struct {
	socket   js.Value
	maxBytes int
	messages chan []byte
	terminal chan struct{}

	mu          sync.Mutex
	terminalErr error
	listeners   map[string]js.Func

	terminalOnce sync.Once
	socketClosed atomic.Bool
	releaseOnce  sync.Once
}

func (s *browserStream) bind() error {
	if !browserMethod(s.socket, "addEventListener") || !browserMethod(s.socket, "removeEventListener") || !browserMethod(s.socket, "close") {
		return ErrBrowserSocketUnavailable
	}
	s.listeners = map[string]js.Func{}
	s.listeners["message"] = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer s.releaseAfterTerminalCallback()
		if len(args) != 1 {
			s.shutdown(ErrBrowserSocketFrame, true)
			return nil
		}
		data, ok := browserProperty(args[0], "data")
		if !ok || data.Type() != js.TypeString {
			s.shutdown(ErrBrowserSocketFrame, true)
			return nil
		}
		text := data.String()
		if text == "" || len(text) > s.maxBytes {
			s.shutdown(ErrBrowserSocketFrame, true)
			return nil
		}
		raw := []byte(text)
		select {
		case <-s.terminal:
		case s.messages <- raw:
		default:
			s.shutdown(ErrBrowserReceiveQueueFull, true)
		}
		return nil
	})
	s.listeners["error"] = js.FuncOf(func(js.Value, []js.Value) any {
		defer s.releaseAfterTerminalCallback()
		s.shutdown(ErrBrowserSocketUnavailable, true)
		return nil
	})
	s.listeners["close"] = js.FuncOf(func(js.Value, []js.Value) any {
		defer s.releaseAfterTerminalCallback()
		// A remote close has already closed the underlying socket.
		s.socketClosed.Store(true)
		s.shutdown(io.EOF, false)
		return nil
	})
	for _, name := range []string{"message", "error", "close"} {
		if !browserCall(s.socket, "addEventListener", name, s.listeners[name]) {
			return ErrBrowserSocketUnavailable
		}
	}
	return nil
}

// Recv returns exactly one complete text frame. Once any unsupported,
// oversized, or overflow frame is observed, queued frames are discarded and
// the generic terminal error wins so recovery must reconnect and catch up.
func (s *browserStream) Recv() ([]byte, error) {
	if s == nil {
		return nil, ErrBrowserSocketUnavailable
	}
	select {
	case <-s.terminal:
		return nil, s.err()
	default:
	}
	select {
	case <-s.terminal:
		return nil, s.err()
	case raw := <-s.messages:
		return append([]byte(nil), raw...), nil
	}
}

// Close is idempotent, unblocks Recv, closes the JavaScript socket at most
// once, and removes/releases every registered event callback.
func (s *browserStream) Close() error {
	if s == nil {
		return nil
	}
	s.shutdown(io.EOF, true)
	s.releaseListeners()
	return nil
}

func (s *browserStream) shutdown(err error, closeSocket bool) {
	if s == nil {
		return
	}
	s.terminalOnce.Do(func() {
		s.mu.Lock()
		s.terminalErr = err
		s.mu.Unlock()
		close(s.terminal)
	})
	// Never invoke JavaScript while terminalOnce is executing. A hostile or
	// synchronous close implementation may immediately deliver a close event,
	// which re-enters shutdown; terminal publication must already be complete.
	if closeSocket {
		s.closeSocket()
	}
}

func (s *browserStream) closeSocket() {
	if s.socketClosed.CompareAndSwap(false, true) {
		_ = browserCall(s.socket, "close")
	}
}

func (s *browserStream) releaseAfterTerminalCallback() {
	select {
	case <-s.terminal:
		// js.Func.Release must not release the function whose invocation is
		// still on the JavaScript stack. The Go/WASM scheduler runs this cleanup
		// after the event callback yields.
		go s.releaseListeners()
	default:
	}
}

func (s *browserStream) releaseListeners() {
	s.releaseOnce.Do(func() {
		for _, name := range []string{"message", "error", "close"} {
			listener, ok := s.listeners[name]
			if !ok {
				continue
			}
			_ = browserCall(s.socket, "removeEventListener", name, listener)
			listener.Release()
		}
		s.listeners = nil
	})
}

func (s *browserStream) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminalErr == nil {
		return io.EOF
	}
	return s.terminalErr
}

func validateBrowserSocketURL(rawURL string, origin browserOrigin) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || parsed.Path == "" {
		return ErrBrowserSocketURL
	}
	wantScheme := ""
	switch strings.ToLower(origin.protocol) {
	case "https:":
		wantScheme = "wss"
	case "http:":
		wantScheme = "ws"
	default:
		return ErrBrowserSocketURL
	}
	if strings.ToLower(parsed.Scheme) != wantScheme || !strings.EqualFold(parsed.Host, origin.host) {
		return ErrBrowserSocketURL
	}
	return nil
}

func currentBrowserOrigin() (browserOrigin, error) {
	location, ok := browserProperty(js.Global(), "location")
	if !ok {
		return browserOrigin{}, ErrBrowserSocketURL
	}
	protocol, protocolOK := browserProperty(location, "protocol")
	host, hostOK := browserProperty(location, "host")
	if !protocolOK || !hostOK || protocol.Type() != js.TypeString || host.Type() != js.TypeString {
		return browserOrigin{}, ErrBrowserSocketURL
	}
	return browserOrigin{protocol: protocol.String(), host: host.String()}, nil
}

func constructBrowserSocket(rawURL string) (socket js.Value, err error) {
	defer func() {
		if recover() != nil {
			socket = js.Undefined()
			err = ErrBrowserSocketUnavailable
		}
	}()
	constructor := js.Global().Get("WebSocket")
	if constructor.Type() != js.TypeFunction {
		return js.Undefined(), ErrBrowserSocketUnavailable
	}
	return constructor.New(rawURL), nil
}

func browserProperty(object js.Value, name string) (value js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			value = js.Undefined()
			ok = false
		}
	}()
	value = object.Get(name)
	return value, value.Type() != js.TypeUndefined && value.Type() != js.TypeNull
}

func browserMethod(object js.Value, name string) bool {
	value, ok := browserProperty(object, name)
	return ok && value.Type() == js.TypeFunction
}

func browserCall(object js.Value, method string, args ...any) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	object.Call(method, args...)
	return true
}
