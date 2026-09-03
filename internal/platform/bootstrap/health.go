package bootstrap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// HealthState is one state in the process health/readiness state machine.
type HealthState int

const (
	// StateStarting is the initial state: the process is resolving
	// configuration and dependencies, not yet accepting work.
	StateStarting HealthState = iota
	// StateReady means every declared workload has started and the
	// process should be considered live and ready for traffic/work.
	StateReady
	// StateDraining means shutdown has begun: the process is finishing or
	// checkpointing in-flight work and should be taken out of rotation.
	StateDraining
	// StateStopped is the terminal state: shutdown has completed.
	StateStopped
)

// String renders the state the way it appears in logs and the health HTTP
// endpoint.
func (s HealthState) String() string {
	switch s {
	case StateStarting:
		return "STARTING"
	case StateReady:
		return "READY"
	case StateDraining:
		return "DRAINING"
	case StateStopped:
		return "STOPPED"
	default:
		return "UNKNOWN"
	}
}

// validHealthTransitions enumerates every transition Health.Set accepts.
// The machine is one-directional (STARTING -> READY -> DRAINING ->
// STOPPED); STARTING may also fail straight to DRAINING or STOPPED without
// ever reaching READY (a dependency failed before any workload started).
var validHealthTransitions = map[HealthState]map[HealthState]bool{
	StateStarting: {StateReady: true, StateDraining: true, StateStopped: true},
	StateReady:    {StateDraining: true, StateStopped: true},
	StateDraining: {StateStopped: true},
	StateStopped:  {},
}

// ErrInvalidHealthTransition is returned by Health.Set for any transition
// not in validHealthTransitions.
type ErrInvalidHealthTransition struct {
	From, To HealthState
}

func (e *ErrInvalidHealthTransition) Error() string {
	return fmt.Sprintf("bootstrap: invalid health transition %s -> %s", e.From, e.To)
}

// Health is a concurrency-safe STARTING/READY/DRAINING/STOPPED state
// machine. The zero value is not usable; construct one with NewHealth.
type Health struct {
	mu    sync.RWMutex
	state HealthState
}

// NewHealth returns a Health starting in StateStarting.
func NewHealth() *Health {
	return &Health{state: StateStarting}
}

// Get returns the current state.
func (h *Health) Get() HealthState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

// Set transitions to next, returning ErrInvalidHealthTransition (and
// leaving the state unchanged) if that transition is not allowed. Setting
// the current state to itself is always a no-op success.
func (h *Health) Set(next HealthState) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state == next {
		return nil
	}
	if !validHealthTransitions[h.state][next] {
		return &ErrInvalidHealthTransition{From: h.state, To: next}
	}
	h.state = next
	return nil
}

// healthResponse is the exact, stable JSON shape the loopback health
// endpoint serves.
type healthResponse struct {
	State string `json:"state"`
}

// Handler returns an http.Handler serving the current state as JSON:
// 200 while StateReady, 503 in every other state (STARTING and DRAINING
// are both "not ready"; STOPPED should never be observed by a live probe).
// Bootstrap binds this handler to a loopback-only address when
// Spec.HealthAddr is set; it never listens on a non-loopback interface.
func (h *Health) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := h.Get()
		w.Header().Set("Content-Type", "application/json")
		if state != StateReady {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(healthResponse{State: state.String()})
	})
}
