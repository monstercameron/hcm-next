package workitem

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// QueuedItem is one schedulable unit. DeadlineTick is the legal/SLA
// deadline: reassignment and scheduling never move it. LegalAuthority
// marks items no workload priority may override.
type QueuedItem struct {
	ID             string
	Tenant         string
	Priority       int
	EnqueuedTick   int64
	DeadlineTick   int64
	Assignee       string
	LegalAuthority bool
}

// ItemState is one bulk item's independent state: authorization,
// evidence and resumability travel per item, and denied or failed
// subjects stay visible.
type ItemState struct {
	ID         string
	Authorized bool
	Evidenced  bool
	Resumable  bool
	Denied     bool
	Failed     bool
	Done       bool
}

func queueKey(item QueuedItem) string {
	return strings.Join([]string{item.Tenant, fmt.Sprint(item.Priority, item.EnqueuedTick, item.DeadlineTick), item.ID, fmt.Sprint(item.LegalAuthority)}, "\x01")
}

// Order returns the deterministic schedule: legal-authority items first
// (workload priority never overrides legal authority), then effective
// priority with aging, then enqueue tick, then identity. Every item
// appears exactly once: no tenant or priority starves.
func Order(queue []QueuedItem, nowTick int64, agingDivisor int64) ([]QueuedItem, error) {
	if len(queue) == 0 {
		return nil, fmt.Errorf("workitem: queue is empty")
	}
	if agingDivisor <= 0 {
		return nil, fmt.Errorf("workitem: aging divisor must be positive")
	}
	seen := make(map[string]bool, len(queue))
	ordered := append([]QueuedItem(nil), queue...)
	for _, item := range ordered {
		if strings.TrimSpace(item.ID) == "" || seen[item.ID] {
			return nil, fmt.Errorf("workitem: queued identities must be unique and non-empty")
		}
		seen[item.ID] = true
		if strings.TrimSpace(item.Tenant) == "" {
			return nil, fmt.Errorf("workitem: queued item %s names no tenant", item.ID)
		}
	}
	score := func(item QueuedItem) int64 {
		age := nowTick - item.EnqueuedTick
		if age < 0 {
			age = 0
		}
		return int64(item.Priority) + age/agingDivisor
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].LegalAuthority != ordered[j].LegalAuthority {
			return ordered[i].LegalAuthority
		}
		if score(ordered[i]) != score(ordered[j]) {
			return score(ordered[i]) > score(ordered[j])
		}
		if ordered[i].EnqueuedTick != ordered[j].EnqueuedTick {
			return ordered[i].EnqueuedTick < ordered[j].EnqueuedTick
		}
		return ordered[i].ID < ordered[j].ID
	})
	return ordered, nil
}

// SelectTop fills a bulk window of k items with reserved capacity: every
// reserved tenant keeps at least its slots, so one tenant's flood never
// starves another. The remainder follows the deterministic order.
func SelectTop(queue []QueuedItem, nowTick int64, agingDivisor int64, window int, reserved map[string]int) ([]QueuedItem, error) {
	ordered, err := Order(queue, nowTick, agingDivisor)
	if err != nil {
		return nil, err
	}
	if window <= 0 || window > len(ordered) {
		return nil, fmt.Errorf("workitem: window %d is outside the queue", window)
	}
	var selected []QueuedItem
	taken := make(map[string]bool, len(ordered))
	for _, item := range ordered {
		if len(selected) >= window {
			break
		}
		slots, ok := reserved[item.Tenant]
		if !ok || slots <= 0 {
			continue
		}
		count := 0
		for _, picked := range selected {
			if picked.Tenant == item.Tenant {
				count++
			}
		}
		if count < slots {
			selected = append(selected, item)
			taken[item.ID] = true
		}
	}
	for _, item := range ordered {
		if len(selected) >= window {
			break
		}
		if !taken[item.ID] {
			selected = append(selected, item)
			taken[item.ID] = true
		}
	}
	return selected, nil
}

// Reassign moves one item without touching its legal/SLA deadline.
func Reassign(item QueuedItem, assignee string) (QueuedItem, error) {
	if strings.TrimSpace(assignee) == "" {
		return QueuedItem{}, fmt.Errorf("workitem: reassignment needs an assignee")
	}
	moved := item
	moved.Assignee = assignee
	if moved.DeadlineTick != item.DeadlineTick {
		return QueuedItem{}, fmt.Errorf("workitem: reassignment moved the deadline")
	}
	return moved, nil
}

// BulkSummary is the honest bulk rollup: overall success only when every
// item is done without denial or failure; otherwise the denied and
// failed subjects stay named.
type BulkSummary struct {
	Complete bool
	Denied   []string
	Failed   []string
	Open     []string
}

// Summarize folds per-item states without hiding any subject.
func Summarize(states []ItemState) (BulkSummary, error) {
	if len(states) == 0 {
		return BulkSummary{}, fmt.Errorf("workitem: bulk summary needs at least one item")
	}
	summary := BulkSummary{Complete: true}
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		if strings.TrimSpace(state.ID) == "" || seen[state.ID] {
			return BulkSummary{}, fmt.Errorf("workitem: bulk identities must be unique and non-empty")
		}
		seen[state.ID] = true
		switch {
		case state.Denied:
			summary.Denied = append(summary.Denied, state.ID)
			summary.Complete = false
		case state.Failed:
			summary.Failed = append(summary.Failed, state.ID)
			summary.Complete = false
		case !state.Done:
			summary.Open = append(summary.Open, state.ID)
			summary.Complete = false
		}
	}
	sort.Strings(summary.Denied)
	sort.Strings(summary.Failed)
	sort.Strings(summary.Open)
	return summary, nil
}

// QueueRegistry guards queues for concurrent scheduling.
type QueueRegistry struct {
	mu     sync.Mutex
	queues map[string][]QueuedItem
}

// NewQueueRegistry starts an empty registry.
func NewQueueRegistry() *QueueRegistry {
	return &QueueRegistry{queues: make(map[string][]QueuedItem)}
}

// Publish replaces one named queue.
func (registry *QueueRegistry) Publish(name string, queue []QueuedItem) error {
	if registry == nil {
		return fmt.Errorf("workitem: nil queue registry")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("workitem: queue name is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.queues[name] = append([]QueuedItem(nil), queue...)
	return nil
}

// Schedule orders one published queue.
func (registry *QueueRegistry) Schedule(name string, nowTick int64, agingDivisor int64) ([]QueuedItem, error) {
	if registry == nil {
		return nil, fmt.Errorf("workitem: nil queue registry")
	}
	registry.mu.Lock()
	queue := append([]QueuedItem(nil), registry.queues[name]...)
	registry.mu.Unlock()
	return Order(queue, nowTick, agingDivisor)
}
