package time

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// Environment is the wired, zero-effect, in-memory projection this
// conformance fixture is simulated against. Every field is a declared,
// pinned fact; nothing here reads a clock, a database or a network.
type Environment struct {
	ClockSkewWithinTolerance   bool
	DSTFoldAmbiguous           bool
	OfflineReplayDetected      bool
	SharedDeviceSpoofSuspected bool
	PostLockEditAttempted      bool
	TimecardReopenedStale      bool
	DuplicatePunchDetected     bool
	TimezoneID                 string
	TZDBVersion                string

	// ObserveOutcome and ObserveWatermark control the answer the injected
	// read port gives for the payroll-bridge ingestion observation.
	ObserveOutcome   workflow.Outcome
	ObserveWatermark string
}

// GoldenEnvironment is the reference scenario: a clean punch within clock
// tolerance, no DST fold, no offline replay, no spoof suspicion, no
// post-lock edit, no stale reopen, no duplicate, and a clean payroll-bridge
// ingestion observation.
func GoldenEnvironment() *Environment {
	return &Environment{
		ClockSkewWithinTolerance:   true,
		DSTFoldAmbiguous:           false,
		OfflineReplayDetected:      false,
		SharedDeviceSpoofSuspected: false,
		PostLockEditAttempted:      false,
		TimecardReopenedStale:      false,
		DuplicatePunchDetected:     false,
		TimezoneID:                 "America/Chicago",
		TZDBVersion:                "tzdata2026b",
		ObserveOutcome:             workflow.OutcomePass,
		ObserveWatermark:           "payroll.bridge.stream_head@21",
	}
}

// DuplicatePunchEnvironment reports a duplicate punch, which the
// classification DECISION must refuse ahead of every other condition.
func DuplicatePunchEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DuplicatePunchDetected = true
	return e
}

// PostLockEditEnvironment reports an attempted edit after the timecard
// locked, an irreversible integrity violation the decision must reject.
func PostLockEditEnvironment() *Environment {
	e := GoldenEnvironment()
	e.PostLockEditAttempted = true
	return e
}

// StaleReopenEnvironment reports a timecard reopened after it had already
// gone stale relative to the payroll cutoff.
func StaleReopenEnvironment() *Environment {
	e := GoldenEnvironment()
	e.TimecardReopenedStale = true
	return e
}

// SpoofSuspectedEnvironment reports a suspected shared-device spoof, which
// must route to review rather than either silent acceptance or rejection.
func SpoofSuspectedEnvironment() *Environment {
	e := GoldenEnvironment()
	e.SharedDeviceSpoofSuspected = true
	return e
}

// OfflineReplayEnvironment reports an offline-replayed punch.
func OfflineReplayEnvironment() *Environment {
	e := GoldenEnvironment()
	e.OfflineReplayDetected = true
	return e
}

// DSTFoldEnvironment reports a DST-fold-ambiguous timestamp: a local time
// that occurred twice because clocks fell back, so which instant is meant
// cannot be inferred from local time alone.
func DSTFoldEnvironment() *Environment {
	e := GoldenEnvironment()
	e.DSTFoldAmbiguous = true
	return e
}

// ClockSkewEnvironment reports a punch outside the configured clock-skew
// tolerance.
func ClockSkewEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ClockSkewWithinTolerance = false
	return e
}

// DegradedBridgeEnvironment answers the payroll-bridge ingestion observation
// with FAIL, which must route to the bridge's own degraded repair terminal
// rather than being folded into a consistent completion, even though the
// punch itself classified as ACCEPTED.
func DegradedBridgeEnvironment() *Environment {
	e := GoldenEnvironment()
	e.ObserveOutcome = workflow.OutcomeFail
	e.ObserveWatermark = ""
	return e
}

// Registry publishes the fixture capability versions this reference workflow
// binds. Every definition is READ_ONLY: the governed gateway refuses every
// write effect class in P1A, so nothing registered here could mutate even if
// a handler tried.
func (e *Environment) Registry() (*capability.Registry, error) {
	r := capability.NewRegistry()
	entries := []struct {
		id      string
		domain  string
		handler capability.Handler
	}{
		{CapReadDeviceAndClockContext, "time.device", e.handleReadDeviceAndClockContext},
		// The bridge observation runs through the injected ReadPort, not
		// this capability handler; the capability exists only because a
		// StepObserve node must bind one.
		{CapObservePayrollBridge, "time.bridge", e.handleObservePayrollBridgeUnused},
	}
	for _, entry := range entries {
		if err := r.Register(readOnlyDefinition(entry.id, entry.domain), entry.handler); err != nil {
			return nil, fmt.Errorf("time: publish %s: %w", entry.id, err)
		}
	}
	return r, nil
}

func readOnlyDefinition(id, domain string) capability.Definition {
	schema := func(slot string) capability.SchemaRef {
		return capability.SchemaRef{
			SchemaID: id + "." + slot + "/v1", Version: 1,
			ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
		}
	}
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: domain,
		RequestSchema: schema("request"), ResponseSchema: schema("response"), ErrorSchema: schema("error"),
		EffectClass: capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:" + domain + ".read",
		LegalBasisRef: "legal.p1a.observation-only.v1", EntitlementRef: "entitlement.pilot.p1a.v1",
		SLOClassRef: "slo.interactive.p95-2s.v1", TestRef: "conformance:" + id + "/v1",
	}
}

func request(payload any) (simulate.CapabilityRequest, error) {
	req, ok := payload.(simulate.CapabilityRequest)
	if !ok {
		return simulate.CapabilityRequest{}, fmt.Errorf("time: handler received %T, want simulate.CapabilityRequest", payload)
	}
	return req, nil
}

func (e *Environment) handleReadDeviceAndClockContext(_ context.Context, payload any) (any, error) {
	if _, err := request(payload); err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{
			"clock_skew_within_tolerance":   simulate.NewBool(e.ClockSkewWithinTolerance),
			"dst_fold_ambiguous":            simulate.NewBool(e.DSTFoldAmbiguous),
			"offline_replay_detected":       simulate.NewBool(e.OfflineReplayDetected),
			"shared_device_spoof_suspected": simulate.NewBool(e.SharedDeviceSpoofSuspected),
			"post_lock_edit_attempted":      simulate.NewBool(e.PostLockEditAttempted),
			"timecard_reopened_stale":       simulate.NewBool(e.TimecardReopenedStale),
			"duplicate_punch_detected":      simulate.NewBool(e.DuplicatePunchDetected),
			"timezone_id":                   simulate.NewString(e.TimezoneID),
			"tzdb_version":                  simulate.NewString(e.TZDBVersion),
		},
		Detail: "read device and clock context in " + e.TimezoneID + " under " + e.TZDBVersion,
	}, nil
}

// handleObservePayrollBridgeUnused exists only to publish
// CapObservePayrollBridge, so the OBSERVE node has a capability to bind. The
// interpreter never invokes it: OBSERVE dispatches through the injected
// simulate.ReadPort (see [Reads]).
func (e *Environment) handleObservePayrollBridgeUnused(_ context.Context, payload any) (any, error) {
	req, err := request(payload)
	if err != nil {
		return nil, err
	}
	return simulate.CapabilityResponse{
		Outcome: workflow.OutcomeUnknown, Outputs: simulate.Bag{},
		Detail: "node " + req.NodeID + " is served by the injected read port",
	}, nil
}
