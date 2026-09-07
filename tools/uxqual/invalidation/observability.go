package invalidation

// Event is deterministic, redaction-safe observability. It intentionally has
// no tenant, projection, subject, transport-error, or RPC-error identifiers.
type Event struct {
	Kind           EventKind
	Items          int
	QueueLength    int
	SourceSequence uint64
}

type EventKind string

const (
	EventAccepted     EventKind = "accepted"
	EventRejected     EventKind = "rejected"
	EventRefetched    EventKind = "refetched"
	EventRefetchErr   EventKind = "refetch_error"
	EventQueueFull    EventKind = "queue_full"
	EventClosed       EventKind = "closed"
	EventReconnected  EventKind = "reconnected"
	EventReconnectErr EventKind = "reconnect_error"
	EventCaughtUp     EventKind = "caught_up"
	EventCatchUpErr   EventKind = "catch_up_error"
)

// Snapshot is a race-free diagnostic view of one client.
type Snapshot struct {
	Running            bool
	Queued             int
	Accepted           uint64
	Rejected           uint64
	Refetched          uint64
	RefetchErrors      uint64
	QueueFull          uint64
	CatchUps           uint64
	CatchUpErrors      uint64
	ObserverDrops      uint64
	LastSourceSequence uint64
}

func (c *Client) observe(event Event) {
	if c == nil || c.options.Observe == nil {
		return
	}
	c.eventMu.Lock()
	if len(c.events) == maxObserverQueue {
		c.eventMu.Unlock()
		c.observerDrops.Add(1)
		return
	}
	c.events = append(c.events, event)
	if c.dispatching {
		c.eventMu.Unlock()
		return
	}
	c.dispatching = true
	c.eventMu.Unlock()
	go c.dispatchEvents()
}

func (c *Client) dispatchEvents() {
	for {
		c.eventMu.Lock()
		if len(c.events) == 0 {
			c.dispatching = false
			c.eventMu.Unlock()
			return
		}
		event := c.events[0]
		c.events[0] = Event{}
		c.events = c.events[1:]
		c.eventMu.Unlock()
		func() {
			defer func() { _ = recover() }()
			c.options.Observe(event)
		}()
	}
}

// Snapshot returns bounded counters and current stream state without payloads.
func (c *Client) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	queued := 0
	if c.run != nil {
		queued = len(c.run.jobs)
	}
	return Snapshot{
		Running: c.run != nil || c.reconnecting, Queued: queued, Accepted: c.accepted, Rejected: c.rejected,
		Refetched: c.refetched, RefetchErrors: c.refetchErrors, QueueFull: c.queueFull,
		CatchUps: c.catchUps, CatchUpErrors: c.catchUpErrors,
		ObserverDrops: c.observerDrops.Load(), LastSourceSequence: c.scope.SourceSequence,
	}
}
