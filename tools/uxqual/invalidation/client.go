// Package invalidation receives bounded product-query invalidation hints.
//
// An invalidation is deliberately only a hint: it contains no display data
// and never changes a view. Accepted hints schedule a fresh, authorized
// projection read through the injected Refetch function. The stream is an
// independent lane from finite RPC capacity. RunReconnect adds bounded
// transport retry and sequence catch-up without changing this authority.
package invalidation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type refreshJob struct {
	id         uint64
	generation uint64
	refresh    Refresh
	revisions  map[string]uint64
	ready      chan struct{}
	result     chan error
}

type runState struct {
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	stream     CloseStream
	managed    bool
	closeOnce  sync.Once
	closeDone  chan struct{}
	jobs       chan refreshJob
	workerDone chan struct{}
	done       chan error
}

// Client is one bounded invalidation subscription. Start owns one live
// connection; RunReconnect owns generations with bounded consecutive failure.
type Client struct {
	mu      sync.Mutex
	scope   Scope
	options Options
	refetch Refetch
	run     *runState

	nextGeneration uint64
	nextJobID      uint64
	pending        []refreshJob

	reconnecting    bool
	reconnectCancel context.CancelFunc

	accepted, rejected, refetched, refetchErrors, queueFull uint64
	catchUps, catchUpErrors                                 uint64

	eventMu       sync.Mutex
	events        []Event
	dispatching   bool
	observerDrops atomic.Uint64
}

// New validates the active authorized scope and returns a client with bounded
// resources. No stream is opened until Start.
func New(scope Scope, refetch Refetch, options Options) (*Client, error) {
	if refetch == nil {
		return nil, errors.New("invalidation: refetch function is nil")
	}
	options = normalizeOptions(options)
	normalized, err := normalizeScope(scope)
	if err != nil {
		return nil, err
	}
	return &Client{scope: normalized, options: options, refetch: refetch}, nil
}

// UpdateScope replaces the stopped client's authorized allow-list. A live
// scope change must cancel and close the old subscription first; silently
// retargeting an in-flight refetch could apply an answer from the prior route.
func (c *Client) UpdateScope(scope Scope) error {
	if c == nil {
		return errors.New("invalidation: nil client")
	}
	normalized, err := normalizeScope(scope)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.run != nil || c.reconnecting {
		return ErrAlreadyRunning
	}
	c.scope = normalized
	c.pending = nil
	return nil
}

// Start starts the reader and bounded refetch worker. The returned channel
// receives exactly one terminal error and is then closed. EOF is a clean end;
// no reconnect is attempted. A stream must be closeable so cancellation can
// interrupt Recv.
func (c *Client) Start(parent context.Context, stream Stream) (<-chan error, error) {
	done, _, err := c.start(parent, stream, false)
	return done, err
}

func (c *Client) start(parent context.Context, stream Stream, managed bool) (<-chan error, *runState, error) {
	if c == nil {
		return nil, nil, errors.New("invalidation: nil client")
	}
	if isNilInterface(stream) {
		return nil, nil, ErrNoStream
	}
	closable, ok := stream.(CloseStream)
	if !ok || isNilInterface(closable) {
		return nil, nil, ErrStreamNotCloseable
	}
	if parent == nil {
		parent = context.Background()
	}
	c.mu.Lock()
	if c.run != nil {
		c.mu.Unlock()
		return nil, nil, ErrAlreadyRunning
	}
	if c.reconnecting && !managed {
		c.mu.Unlock()
		return nil, nil, ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(parent)
	c.nextGeneration++
	run := &runState{
		generation: c.nextGeneration,
		ctx:        ctx,
		cancel:     cancel,
		stream:     closable,
		managed:    managed,
		closeDone:  make(chan struct{}),
		jobs:       make(chan refreshJob, c.options.MaxQueue),
		workerDone: make(chan struct{}),
		done:       make(chan error, 1),
	}
	c.run = run
	c.pending = nil
	c.mu.Unlock()
	if generationAware, ok := stream.(interface{ setGeneration(uint64) }); ok {
		generationAware.setGeneration(run.generation)
	}
	if managed {
		// Publish the connection before its reader can publish catch-up,
		// acceptance, completion, or closure. This makes the serialized event
		// stream deterministic even for a transport that returns immediately.
		cursor := c.currentCursor()
		c.observe(Event{Kind: EventReconnected, SourceSequence: cursor.Sequence()})
	}

	go c.worker(run)
	go c.read(run)
	go func() {
		<-ctx.Done()
		c.interrupt(run)
	}()
	return run.done, run, nil
}

// Run is the blocking counterpart to Start and is convenient for native
// tests and small composition roots.
func (c *Client) Run(ctx context.Context, stream Stream) error {
	done, err := c.Start(ctx, stream)
	if err != nil {
		return err
	}
	return <-done
}

// Close cancels the live connection and interrupts its Recv exactly once. A
// hostile or broken transport Close cannot block the caller or be invoked a
// second time by a racing parent-context cancellation.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	run := c.run
	reconnectCancel := c.reconnectCancel
	c.mu.Unlock()
	if reconnectCancel != nil {
		reconnectCancel()
	}
	if run == nil {
		return nil
	}
	run.cancel()
	c.interrupt(run)
	return nil
}

func (c *Client) interrupt(run *runState) {
	if run == nil {
		return
	}
	run.closeOnce.Do(func() {
		// Transport code is outside this package's trust boundary. Run it away
		// from cancellation callers and contain both blocking and panic.
		go func() {
			defer close(run.closeDone)
			defer func() { _ = recover() }()
			_ = run.stream.Close()
		}()
	})
}
