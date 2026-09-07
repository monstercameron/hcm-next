// Package invalidation receives bounded product-query invalidation hints.
//
// An invalidation is deliberately only a hint: it contains no display data
// and never changes a view. Accepted hints schedule a fresh, authorized
// projection read through the injected Refetch function. The stream is an
// independent lane from finite RPC capacity and this package owns no
// reconnect or catch-up policy (WEB-036).
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
}

type runState struct {
	generation uint64
	ctx        context.Context
	cancel     context.CancelFunc
	stream     CloseStream
	closeOnce  sync.Once
	jobs       chan refreshJob
	workerDone chan struct{}
	done       chan error
}

// Client is one bounded invalidation subscription. It supports one live
// connection only; reconnect/catch-up sequencing belongs to WEB-036.
type Client struct {
	mu      sync.Mutex
	scope   Scope
	options Options
	refetch Refetch
	run     *runState

	nextGeneration uint64
	nextJobID      uint64
	pending        []refreshJob

	accepted, rejected, refetched, refetchErrors, queueFull uint64

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
	if c.run != nil {
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
	if c == nil {
		return nil, errors.New("invalidation: nil client")
	}
	if isNilInterface(stream) {
		return nil, ErrNoStream
	}
	closable, ok := stream.(CloseStream)
	if !ok || isNilInterface(closable) {
		return nil, ErrStreamNotCloseable
	}
	if parent == nil {
		parent = context.Background()
	}
	c.mu.Lock()
	if c.run != nil {
		c.mu.Unlock()
		return nil, ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(parent)
	c.nextGeneration++
	run := &runState{
		generation: c.nextGeneration,
		ctx:        ctx,
		cancel:     cancel,
		stream:     closable,
		jobs:       make(chan refreshJob, c.options.MaxQueue),
		workerDone: make(chan struct{}),
		done:       make(chan error, 1),
	}
	c.run = run
	c.pending = nil
	c.mu.Unlock()

	go c.worker(run)
	go c.read(run)
	go func() {
		<-ctx.Done()
		c.interrupt(run)
	}()
	return run.done, nil
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
	c.mu.Unlock()
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
			defer func() { _ = recover() }()
			_ = run.stream.Close()
		}()
	})
}
