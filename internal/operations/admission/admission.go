// Package admission owns the deterministic tenant-aware overload decision.
// It has no storage or runtime dependencies: callers provide one coherent
// control-plane snapshot and receive a value-only decision receipt.
package admission

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

const contractVersion = 1

// Version returns the version of the overload decision contract.
func Version() int { return contractVersion }

// Explain describes the contract without including tenant or request data.
func Explain() string { return "tenant-aware deterministic admission and bounded retry contract" }

type Criticality string

const (
	P0 Criticality = "P0"
	P1 Criticality = "P1"
	P2 Criticality = "P2"
	P3 Criticality = "P3"
	P4 Criticality = "P4"
)

type Outcome string

const (
	Admit   Outcome = "ADMIT"
	Queue   Outcome = "QUEUE"
	Defer   Outcome = "DEFER"
	Degrade Outcome = "DEGRADE"
	Reject  Outcome = "REJECT"
	// Shed is retained as a protocol synonym for a rejected best-effort item.
	Shed Outcome = "SHED"
)

// Descriptive aliases keep call sites readable while the wire values remain
// the five outcomes owned by ADMISSION-001.
const (
	OutcomeAdmit   = Admit
	OutcomeQueue   = Queue
	OutcomeDefer   = Defer
	OutcomeDegrade = Degrade
	OutcomeReject  = Reject
	CriticalityP0  = P0
	CriticalityP1  = P1
	CriticalityP2  = P2
	CriticalityP3  = P3
	CriticalityP4  = P4
)

// Decision is a complete, deterministic admission receipt.
type Decision struct {
	DecisionID   string
	Outcome      Outcome
	Reason       string
	RetryAfter   int
	Reservation  int
	TenantID     string
	CellID       string
	Criticality  Criticality
	QuotaVersion string
	RetryBudget  string
	Evidence     Evidence
}

type Evidence struct {
	TenantID          string
	CellID            string
	Criticality       Criticality
	QuotaLimit        int
	QuotaConsumed     int
	QuotaPending      int
	Capacity          int
	Requested         int
	ReservedP0        int
	RetryRemaining    int
	PlacementEpoch    uint64
	ObservedEpoch     uint64
	QuotaKnown        bool
	PlacementCurrent  bool
	NoisyTenant       bool
	Draining          bool
	AvailableCapacity int
}

type Request struct {
	TenantID       string
	CellID         string
	PlacementEpoch uint64
	Criticality    Criticality
	EstimatedCost  int
	RetryBudgetID  string
	RetryAttempt   int
}

type Snapshot struct {
	TenantID       string
	CellID         string
	PlacementEpoch uint64
	Quota          Quota
	Capacity       int
	ReservedP0     int
	RetryRemaining int
	NoisyTenant    bool
	Draining       bool
}

type Quota struct {
	Known    bool
	Version  string
	Limit    int
	Consumed int
	Pending  int
}

type Policy struct {
	QueueRetryAfter   int
	DeferRetryAfter   int
	DegradeRetryAfter int
}

var ErrInvalidInput = errors.New("admission: invalid input")

func (p Policy) normalized() Policy {
	if p.QueueRetryAfter <= 0 {
		p.QueueRetryAfter = 5
	}
	if p.DeferRetryAfter <= 0 {
		p.DeferRetryAfter = 15
	}
	if p.DegradeRetryAfter <= 0 {
		p.DegradeRetryAfter = 5
	}
	return p
}

// Decide evaluates exactly one snapshot. It never consults clocks, random
// state, or mutable globals, so equal inputs always produce equal receipts.
func Decide(req Request, state Snapshot, policy Policy) Decision {
	p := policy.normalized()
	e := Evidence{TenantID: req.TenantID, CellID: req.CellID, Criticality: req.Criticality,
		QuotaLimit: state.Quota.Limit, QuotaConsumed: state.Quota.Consumed, QuotaPending: state.Quota.Pending,
		Capacity: state.Capacity, Requested: req.EstimatedCost, ReservedP0: state.ReservedP0,
		RetryRemaining: state.RetryRemaining, PlacementEpoch: req.PlacementEpoch, ObservedEpoch: state.PlacementEpoch,
		QuotaKnown: state.Quota.Known, PlacementCurrent: req.PlacementEpoch == state.PlacementEpoch, NoisyTenant: state.NoisyTenant, Draining: state.Draining}
	d := Decision{TenantID: req.TenantID, CellID: req.CellID, Criticality: req.Criticality, QuotaVersion: state.Quota.Version, RetryBudget: req.RetryBudgetID, Evidence: e}
	finish := func(out Outcome, reason string, retry, reservation int) Decision {
		d.Outcome, d.Reason, d.RetryAfter, d.Reservation = out, reason, retry, reservation
		d.DecisionID = id(req, state, d)
		return d
	}
	if req.TenantID == "" || req.CellID == "" || req.EstimatedCost <= 0 || req.RetryAttempt < 0 || !validCriticality(req.Criticality) {
		return finish(Reject, "INVALID_CONTEXT", 0, 0)
	}
	if state.TenantID != req.TenantID || state.CellID != req.CellID {
		return finish(Reject, "TENANT_OR_CELL_MISMATCH", 0, 0)
	}
	if state.Draining {
		return finish(Defer, "CELL_DRAINING", p.DeferRetryAfter, 0)
	}
	if !state.Quota.Known {
		return finish(Defer, "QUOTA_UNKNOWN", p.DeferRetryAfter, 0)
	}
	if req.PlacementEpoch == 0 || req.PlacementEpoch != state.PlacementEpoch {
		return finish(Defer, "STALE_PLACEMENT", p.DeferRetryAfter, 0)
	}
	if state.Capacity < 0 || state.ReservedP0 < 0 || state.ReservedP0 > state.Capacity || state.Quota.Limit < 0 || state.Quota.Consumed < 0 || state.Quota.Pending < 0 || (state.Quota.Pending > 0 && state.Quota.Consumed > int(^uint(0)>>1)-state.Quota.Pending) {
		return finish(Reject, "INVALID_CAPACITY_RESERVATION", 0, 0)
	}
	e.AvailableCapacity = state.Capacity - state.ReservedP0
	d.Evidence = e
	if state.RetryRemaining <= 0 && req.RetryAttempt > 0 {
		return finish(Reject, "RETRY_BUDGET_EXHAUSTED", 0, 0)
	}
	used := state.Quota.Consumed + state.Quota.Pending
	quotaOK := state.Quota.Limit > 0 && used <= state.Quota.Limit-req.EstimatedCost
	availableCapacity := e.AvailableCapacity
	capacity := availableCapacity - req.EstimatedCost
	if req.Criticality == P0 {
		if state.Capacity-req.EstimatedCost < 0 {
			return finish(Defer, "P0_CAPACITY_RESERVED", p.DeferRetryAfter, 0)
		}
		if !quotaOK {
			return finish(Defer, "P0_QUOTA_RESERVED", p.DeferRetryAfter, 0)
		}
		return finish(Admit, "P0_RESERVED", p.DegradeRetryAfter, req.EstimatedCost)
	}
	if !quotaOK || capacity < 0 || (state.NoisyTenant && req.Criticality >= P2) {
		switch req.Criticality {
		case P1:
			return finish(Degrade, "PRESSURE_OR_NOISY_TENANT", p.DegradeRetryAfter, 0)
		case P2:
			return finish(Queue, "PRESSURE_OR_NOISY_TENANT", p.QueueRetryAfter, 0)
		case P3:
			return finish(Defer, "PRESSURE_OR_NOISY_TENANT", p.DeferRetryAfter, 0)
		default:
			return finish(Reject, "BEST_EFFORT_SHED", 0, 0)
		}
	}
	return finish(Admit, "WITHIN_TENANT_QUOTA_AND_CAPACITY", 0, 0)
}

func validCriticality(c Criticality) bool {
	switch c {
	case P0, P1, P2, P3, P4:
		return true
	default:
		return false
	}
}

func validOutcome(out Outcome) bool {
	return out == Admit || out == Queue || out == Defer || out == Degrade || out == Reject
}

func id(req Request, s Snapshot, d Decision) string {
	digest, err := canonicalbytes.New("hcmnext.operations.admission.DecisionID", contractVersion).
		String("tenant", req.TenantID).String("cell", req.CellID).
		String("observed_tenant", s.TenantID).String("observed_cell", s.CellID).
		String("criticality", string(req.Criticality)).
		Int("estimated_cost", int64(req.EstimatedCost)).Int("retry_attempt", int64(req.RetryAttempt)).
		String("retry_budget", req.RetryBudgetID).Int("placement_epoch", int64(req.PlacementEpoch)).
		Int("observed_placement_epoch", int64(s.PlacementEpoch)).
		String("quota_version", s.Quota.Version).Int("quota_limit", int64(s.Quota.Limit)).
		Int("quota_consumed", int64(s.Quota.Consumed)).Int("quota_pending", int64(s.Quota.Pending)).
		Int("capacity", int64(s.Capacity)).Int("reserved_p0", int64(s.ReservedP0)).
		Int("retry_remaining", int64(s.RetryRemaining)).Bool("noisy_tenant", s.NoisyTenant).
		Bool("draining", s.Draining).Bool("quota_known", s.Quota.Known).
		String("outcome", string(d.Outcome)).String("reason", d.Reason).
		Int("retry_after", int64(d.RetryAfter)).Int("reservation", int64(d.Reservation)).Digest()
	if err != nil {
		return ""
	}
	return "adm_" + strings.TrimPrefix(digest, "sha256:")
}

func (d Decision) Validate() error {
	if d.DecisionID == "" || d.TenantID == "" || d.CellID == "" || !validCriticality(d.Criticality) || !validOutcome(d.Outcome) || d.Reason == "" {
		return fmt.Errorf("%w: incomplete decision", ErrInvalidInput)
	}
	if d.RetryAfter < 0 || d.Reservation < 0 {
		return fmt.Errorf("%w: negative receipt value", ErrInvalidInput)
	}
	return nil
}
