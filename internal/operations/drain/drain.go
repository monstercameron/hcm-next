// Package drain coordinates workload drains at declared safe points
// with epoch fencing (OPS-006): one contract shared by node, shard, cell
// and capability drains. Beginning a drain rejects new leases; finishing
// partitions in-flight work into exact safe and ambiguous counts with
// reconciliation obligations for the ambiguous effects; resuming requires
// a new epoch so stale workers can never slip back in. Kernel-pure
// deterministic state machine, mutex-guarded for concurrent workers.
package drain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrDrainFenced reports a new lease starting after the drain fence.
	ErrDrainFenced = errors.New("drain: drain fence rejects new leases")

	// ErrUnknownLease reports work the drainer never admitted.
	ErrUnknownLease = errors.New("drain: unknown lease")

	// ErrStaleEpoch reports a worker epoch that is not the current one.
	ErrStaleEpoch = errors.New("drain: stale worker epoch")

	// ErrUnsafeComplete reports completing a lease parked in an unsafe
	// region: unsafe work reaches a safe point first.
	ErrUnsafeComplete = errors.New("drain: lease is not at a safe point")

	// ErrDrainState reports a lifecycle step taken in the wrong state.
	ErrDrainState = errors.New("drain: wrong drain state")

	// ErrDuplicateLease reports admitting a lease ID twice.
	ErrDuplicateLease = errors.New("drain: duplicate lease")
)

// Region names where a lease sits: safe points drain immediately, unsafe
// regions reconcile as ambiguous.
type Region string

const (
	// RegionSafe marks work parked at a declared safe point.
	RegionSafe Region = "SAFE"
	// RegionUnsafe marks work inside a region with in-flight effects.
	RegionUnsafe Region = "UNSAFE"
)

// AmbiguousLease is one unsafe lease at finish time with its in-flight
// effects and reconciliation obligation.
type AmbiguousLease struct {
	ID         string
	Effects    []string
	Obligation string
}

// Report is the exact finish partition.
type Report struct {
	Fence     uint64
	Safe      []string
	Ambiguous []AmbiguousLease
}

type lease struct {
	region  Region
	effects []string
	done    bool
}

// Drainer is one fenced drain lifecycle: RUNNING to DRAINING to DRAINED
// to RUNNING under a strictly increasing epoch.
type Drainer struct {
	mu     sync.Mutex
	epoch  uint64
	fence  uint64
	state  string
	leases map[string]*lease
}

const (
	stateRunning  = "RUNNING"
	stateDraining = "DRAINING"
	stateDrained  = "DRAINED"
)

// NewDrainer starts one workload at epoch 1.
func NewDrainer() *Drainer {
	return &Drainer{epoch: 1, state: stateRunning, leases: make(map[string]*lease)}
}

// Epoch reports the current worker epoch.
func (d *Drainer) Epoch() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.epoch
}

// Acquire admits one lease with its in-flight effect IDs. After the fence
// it is refused: nothing new starts draining.
func (d *Drainer) Acquire(id string, effects ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("drain: Acquire: %w", ErrUnknownLease)
	}
	if d.state != stateRunning {
		return fmt.Errorf("drain: Acquire %s: %w", id, ErrDrainFenced)
	}
	if _, dup := d.leases[id]; dup {
		return fmt.Errorf("drain: Acquire %s: %w", id, ErrDuplicateLease)
	}
	d.leases[id] = &lease{region: RegionUnsafe, effects: append([]string(nil), effects...)}
	return nil
}

// Heartbeat moves one lease to its current region under the current
// epoch. Stale epochs and foreign leases are refused.
func (d *Drainer) Heartbeat(id string, epoch uint64, region Region) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	l, ok := d.leases[id]
	if !ok || l.done {
		return fmt.Errorf("drain: Heartbeat %s: %w", id, ErrUnknownLease)
	}
	if epoch != d.epoch {
		return fmt.Errorf("drain: Heartbeat %s epoch %d: %w", id, epoch, ErrStaleEpoch)
	}
	if region != RegionSafe && region != RegionUnsafe {
		return fmt.Errorf("drain: Heartbeat %s region %q: %w", id, string(region), ErrUnknownLease)
	}
	l.region = region
	return nil
}

// Begin raises the drain fence at the current epoch: new leases stop,
// in-flight work keeps heartbeating toward safe points.
func (d *Drainer) Begin() (uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != stateRunning {
		return 0, fmt.Errorf("drain: Begin: %w", ErrDrainState)
	}
	d.state = stateDraining
	d.fence = d.epoch
	return d.fence, nil
}

// Complete retires one lease parked at a safe point.
func (d *Drainer) Complete(id string, epoch uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	l, ok := d.leases[id]
	if !ok || l.done {
		return fmt.Errorf("drain: Complete %s: %w", id, ErrUnknownLease)
	}
	if epoch != d.epoch {
		return fmt.Errorf("drain: Complete %s epoch %d: %w", id, epoch, ErrStaleEpoch)
	}
	if d.state != stateDraining {
		return fmt.Errorf("drain: Complete %s: %w", id, ErrDrainState)
	}
	if l.region != RegionSafe {
		return fmt.Errorf("drain: Complete %s in %s region: %w", id, l.region, ErrUnsafeComplete)
	}
	l.done = true
	return nil
}

// Finish partitions the drain: safe-point leases retire exact, unsafe
// leases turn ambiguous with one reconciliation obligation per lease
// naming its in-flight effects. Unsafe work is never called paused.
func (d *Drainer) Finish() (Report, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != stateDraining {
		return Report{}, fmt.Errorf("drain: Finish: %w", ErrDrainState)
	}
	report := Report{Fence: d.fence}
	for id, l := range d.leases {
		if l.done {
			continue
		}
		if l.region == RegionSafe {
			l.done = true
			report.Safe = append(report.Safe, id)
			continue
		}
		report.Ambiguous = append(report.Ambiguous, AmbiguousLease{
			ID:         id,
			Effects:    append([]string(nil), l.effects...),
			Obligation: "reconcile/" + id,
		})
	}
	sort.Strings(report.Safe)
	sort.Slice(report.Ambiguous, func(i, j int) bool { return report.Ambiguous[i].ID < report.Ambiguous[j].ID })
	d.state = stateDrained
	return report, nil
}

// Resume reopens the workload under a new epoch after a drained finish.
// The epoch must advance past the fence: stale workers stay out.
func (d *Drainer) Resume(newEpoch uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state != stateDrained {
		return fmt.Errorf("drain: Resume: %w", ErrDrainState)
	}
	if newEpoch <= d.fence {
		return fmt.Errorf("drain: Resume epoch %d: %w", newEpoch, ErrStaleEpoch)
	}
	d.epoch = newEpoch
	d.state = stateRunning
	d.leases = make(map[string]*lease)
	return nil
}
