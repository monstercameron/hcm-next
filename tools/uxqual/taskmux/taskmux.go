// Package taskmux bounds and coordinates finite frontend work.
//
// Browser-hosted Go code must leave the event-handler goroutine immediately:
// an RPC made inline prevents the same browser event loop from delivering its
// response. Scheduler.Submit is therefore non-blocking, while a small worker
// budget keeps rapid navigation and repeated clicks from creating an
// unbounded number of goroutines and network requests.
package taskmux

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Priority controls which queued task starts next. It never interrupts work
// which has already started.
type Priority uint8

const (
	Interactive Priority = iota
	UserVisible
	Background
)

// DuplicatePolicy controls what Submit does when a task with the same key is
// already queued or running.
type DuplicatePolicy uint8

const (
	// Enqueue permits independent work with the same key.
	Enqueue DuplicatePolicy = iota
	// KeepExisting returns the existing handle and does not run the new task.
	// This is appropriate for non-idempotent button actions.
	KeepExisting
	// ReplaceExisting cancels older work and schedules the new task. This is
	// appropriate for route and search reads where only the latest answer is
	// useful.
	ReplaceExisting
)

var ErrQueueFull = errors.New("frontend task queue is full")

// Options configures one finite-work lane.
type Options struct {
	MaxRunning int
	MaxQueued  int
	// PriorityBurst is the number of consecutive higher-priority selections
	// allowed while lower-priority work is waiting. It prevents background
	// refreshes from starving under sustained interaction.
	PriorityBurst int
}

// Spec describes one submitted unit of work. An empty Key is never treated as
// a duplicate.
type Spec struct {
	Key       string
	Priority  Priority
	Duplicate DuplicatePolicy
}

// Result is delivered exactly once when a task finishes or is canceled.
type Result struct {
	Err error
}

// Handle represents queued or running work. Done is shared when
// KeepExisting coalesces a duplicate submission.
type Handle struct {
	id         uint64
	completion *completion
	cancel     func()
}

type completion struct {
	done   chan struct{}
	result Result
}

// Done closes when the task finishes. Unlike a result-bearing channel, a
// close broadcasts to every caller coalesced through KeepExisting.
func (h *Handle) Done() <-chan struct{} { return h.completion.done }

// Result is safe to read after Done closes.
func (h *Handle) Result() Result { return h.completion.result }

// Cancel is safe to call more than once.
func (h *Handle) Cancel() {
	if h != nil && h.cancel != nil {
		h.cancel()
	}
}

// Snapshot is an instantaneous, race-free view suitable for diagnostics and
// loading indicators.
type Snapshot struct {
	Running    int
	Queued     int
	ByPriority [3]int
}

type task struct {
	id         uint64
	spec       Spec
	ctx        context.Context
	cancel     context.CancelFunc
	run        func(context.Context) error
	completion *completion
	running    bool
	finished   bool
}

// Scheduler multiplexes finite frontend tasks over a bounded number of
// goroutines. Streaming subscriptions intentionally belong outside this pool.
type Scheduler struct {
	mu sync.Mutex

	maxRunning    int
	maxQueued     int
	priorityBurst int
	nextID        uint64
	running       int
	highBurst     int
	queue         []*task
	byID          map[uint64]*task
}

func New(options Options) *Scheduler {
	if options.MaxRunning < 1 {
		options.MaxRunning = 4
	}
	if options.MaxQueued < 1 {
		options.MaxQueued = 64
	}
	if options.PriorityBurst < 1 {
		options.PriorityBurst = 8
	}
	return &Scheduler{
		maxRunning: options.MaxRunning, maxQueued: options.MaxQueued,
		priorityBurst: options.PriorityBurst, byID: make(map[uint64]*task),
	}
}

// Submit accepts work without waiting for capacity. The function is invoked
// later on its own goroutine and must honor ctx for prompt cancellation.
func (s *Scheduler) Submit(ctx context.Context, spec Spec, run func(context.Context) error) (*Handle, error) {
	if run == nil {
		return nil, errors.New("frontend task function is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if spec.Priority > Background {
		return nil, fmt.Errorf("frontend task priority %d is invalid", spec.Priority)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if spec.Key != "" {
		matches := s.matchingLocked(spec.Key)
		switch spec.Duplicate {
		case KeepExisting:
			if len(matches) > 0 {
				return s.handleLocked(matches[len(matches)-1]), nil
			}
		case ReplaceExisting:
			for _, existing := range matches {
				s.cancelLocked(existing)
			}
		}
	}
	if len(s.queue) >= s.maxQueued && s.running >= s.maxRunning {
		return nil, ErrQueueFull
	}

	s.nextID++
	taskCtx, cancel := context.WithCancel(ctx)
	t := &task{id: s.nextID, spec: spec, ctx: taskCtx, cancel: cancel, run: run, completion: &completion{done: make(chan struct{})}}
	s.byID[t.id] = t
	s.queue = append(s.queue, t)
	handle := s.handleLocked(t)
	s.dispatchLocked()
	return handle, nil
}

func (s *Scheduler) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := Snapshot{Running: s.running, Queued: len(s.queue)}
	for _, t := range s.byID {
		if !t.finished {
			snapshot.ByPriority[t.spec.Priority]++
		}
	}
	return snapshot
}

func (s *Scheduler) handleLocked(t *task) *Handle {
	return &Handle{id: t.id, completion: t.completion, cancel: func() { s.cancel(t.id) }}
}

func (s *Scheduler) matchingLocked(key string) []*task {
	var matches []*task
	for _, t := range s.byID {
		if !t.finished && t.spec.Key == key {
			matches = append(matches, t)
		}
	}
	return matches
}

func (s *Scheduler) cancel(id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.byID[id]; t != nil {
		s.cancelLocked(t)
		s.dispatchLocked()
	}
}

func (s *Scheduler) cancelLocked(t *task) {
	if t.finished {
		return
	}
	t.cancel()
	if t.running {
		return
	}
	for index, queued := range s.queue {
		if queued == t {
			s.queue = append(s.queue[:index], s.queue[index+1:]...)
			break
		}
	}
	s.finishLocked(t, context.Canceled)
}

func (s *Scheduler) dispatchLocked() {
	for s.running < s.maxRunning && len(s.queue) > 0 {
		index := s.nextIndexLocked()
		t := s.queue[index]
		s.queue = append(s.queue[:index], s.queue[index+1:]...)
		if err := t.ctx.Err(); err != nil {
			s.finishLocked(t, err)
			continue
		}
		t.running = true
		s.running++
		go s.execute(t)
	}
}

func (s *Scheduler) nextIndexLocked() int {
	lowest := Background
	hasLower := false
	for _, t := range s.queue {
		if t.spec.Priority < lowest {
			lowest = t.spec.Priority
		}
		if t.spec.Priority > Interactive {
			hasLower = true
		}
	}
	selected := lowest
	if hasLower && s.highBurst >= s.priorityBurst {
		selected = Background
		for _, t := range s.queue {
			if t.spec.Priority > lowest && t.spec.Priority < selected {
				selected = t.spec.Priority
			}
		}
		s.highBurst = 0
	} else if selected < Background {
		s.highBurst++
	} else {
		s.highBurst = 0
	}
	for index, t := range s.queue {
		if t.spec.Priority == selected {
			return index
		}
	}
	return 0
}

func (s *Scheduler) execute(t *task) {
	err := t.run(t.ctx)
	if err == nil {
		err = t.ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.finished {
		return
	}
	s.running--
	t.running = false
	s.finishLocked(t, err)
	s.dispatchLocked()
}

func (s *Scheduler) finishLocked(t *task, err error) {
	if t.finished {
		return
	}
	t.finished = true
	delete(s.byID, t.id)
	t.completion.result = Result{Err: err}
	close(t.completion.done)
	t.cancel()
}
