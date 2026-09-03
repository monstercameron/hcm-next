package workflow

import (
	"errors"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
)

// TerminalProfile names a fixed set of terminal combinations a definition may
// select from. It is a selection, never a lattice: a definition cannot invent
// its own legality rules, and every profile is a subset of what the intent
// kernel's six fixed rules already permit (WF-STEP-017 REFACTOR).
type TerminalProfile string

// The declared terminal profiles.
const (
	// TerminalProfileSimulateOnly is the P1A profile. Nothing committed, so
	// no terminal may claim a committed execution or a completed business
	// outcome.
	TerminalProfileSimulateOnly TerminalProfile = "SIMULATE_ONLY"
	// TerminalProfileExecute admits every terminal the kernel rules permit.
	TerminalProfileExecute TerminalProfile = "EXECUTE"
)

// terminalProfile is the fixed per-dimension allow-list of one profile. A nil
// map means "every state the kernel legality rules already permit".
type terminalProfile struct {
	request     map[lifecycle.RequestState]bool
	execution   map[lifecycle.ExecutionState]bool
	business    map[lifecycle.BusinessState]bool
	consistency map[lifecycle.ConsistencyState]bool
	obligation  map[lifecycle.ObligationState]bool
}

var terminalProfiles = map[TerminalProfile]terminalProfile{
	TerminalProfileSimulateOnly: {
		request: map[lifecycle.RequestState]bool{
			lifecycle.RequestPreflighted: true,
			lifecycle.RequestSimulated:   true,
			lifecycle.RequestRejected:    true,
			lifecycle.RequestWithdrawn:   true,
			lifecycle.RequestCancelled:   true,
			lifecycle.RequestSuperseded:  true,
		},
		execution: map[lifecycle.ExecutionState]bool{
			lifecycle.ExecutionNotPlanned: true,
			lifecycle.ExecutionScheduled:  true,
			lifecycle.ExecutionBlocked:    true,
		},
		business: map[lifecycle.BusinessState]bool{
			lifecycle.BusinessNotStarted:  true,
			lifecycle.BusinessNotAchieved: true,
			lifecycle.BusinessUnknown:     true,
		},
		consistency: map[lifecycle.ConsistencyState]bool{
			lifecycle.ConsistencyNotApplicable:      true,
			lifecycle.ConsistencyPendingObservation: true,
			lifecycle.ConsistencyUnknown:            true,
		},
		obligation: map[lifecycle.ObligationState]bool{
			lifecycle.ObligationNotApplicable: true,
			lifecycle.ObligationPending:       true,
			lifecycle.ObligationSatisfied:     true,
			lifecycle.ObligationWaived:        true,
			lifecycle.ObligationUnknown:       true,
		},
	},
	TerminalProfileExecute: {},
}

// TerminalProfiles lists the declared profiles, sorted.
func TerminalProfiles() []TerminalProfile {
	out := make([]TerminalProfile, 0, len(terminalProfiles))
	for p := range terminalProfiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Valid reports whether p names a declared terminal profile.
func (p TerminalProfile) Valid() bool { _, ok := terminalProfiles[p]; return ok }

// Terminal is one compiled END result: the instance's own runtime status
// beside the intent's five lifecycle dimensions. There is no sixth field and
// no collapsed status.
type Terminal struct {
	NodeID                    string
	TerminalCode              string
	RuntimeStatus             RuntimeStatus
	Dimensions                lifecycle.Dimensions
	OutstandingObligationRefs []string
	RepairRefs                []string
	IncidentRefs              []string
}

// parseCompletionMapping turns an END's declared mapping into the five
// dimensions. A key outside the fixed five is a sixth dimension and is
// rejected; a missing key is a dimension the terminal failed to record.
func parseCompletionMapping(m map[string]string, loc Location, c *collector) (lifecycle.Dimensions, bool) {
	var dims lifecycle.Dimensions
	ok := true

	known := map[string]bool{}
	for _, dim := range lifecycle.AllDimensions() {
		known[string(dim)] = true
	}
	extra := make([]string, 0)
	for key := range m {
		if !known[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	for _, key := range extra {
		ok = false
		c.add(CodeSixthDimension, loc,
			"completion mapping declares %q; the intent lifecycle has exactly five dimensions (%s)",
			key, strings.Join(dimensionNames(), ", "))
	}

	for _, dim := range lifecycle.AllDimensions() {
		raw, present := m[string(dim)]
		if !present || raw == "" {
			ok = false
			c.add(CodeMissingDimension, loc, "completion mapping does not record %s", dim)
			continue
		}
		id := lifecycle.StateID(raw)
		var err error
		switch dim {
		case lifecycle.DimensionRequest:
			dims.Request, err = lifecycle.ParseRequestState(id)
		case lifecycle.DimensionExecution:
			dims.Execution, err = lifecycle.ParseExecutionState(id)
		case lifecycle.DimensionBusiness:
			dims.Business, err = lifecycle.ParseBusinessState(id)
		case lifecycle.DimensionConsistency:
			dims.Consistency, err = lifecycle.ParseConsistencyState(id)
		case lifecycle.DimensionObligation:
			dims.Obligation, err = lifecycle.ParseObligationState(id)
		}
		if err != nil {
			ok = false
			c.add(CodeUnresolvedRef, loc, "%s: %v", dim, err)
		}
	}
	return dims, ok
}

func dimensionNames() []string {
	dims := lifecycle.AllDimensions()
	out := make([]string, len(dims))
	for i, d := range dims {
		out[i] = string(d)
	}
	return out
}

// inProfile reports whether a tuple is within the definition's declared
// terminal profile.
func inProfile(p TerminalProfile, d lifecycle.Dimensions) (lifecycle.Dimension, bool) {
	prof, ok := terminalProfiles[p]
	if !ok {
		return "", true
	}
	if prof.request != nil && !prof.request[d.Request] {
		return lifecycle.DimensionRequest, false
	}
	if prof.execution != nil && !prof.execution[d.Execution] {
		return lifecycle.DimensionExecution, false
	}
	if prof.business != nil && !prof.business[d.Business] {
		return lifecycle.DimensionBusiness, false
	}
	if prof.consistency != nil && !prof.consistency[d.Consistency] {
		return lifecycle.DimensionConsistency, false
	}
	if prof.obligation != nil && !prof.obligation[d.Obligation] {
		return lifecycle.DimensionObligation, false
	}
	return "", true
}

// checkEnd validates one END node's terminal artifact (WF-STEP-017).
func checkEnd(def *Definition, n *Node, degradedEntry bool, c *collector) *Terminal {
	loc := Location{NodeID: n.ID}
	if n.End == nil {
		c.add(CodeInvalidDefinition, loc, "END node declares no end specification")
		return nil
	}
	end := n.End
	if end.TerminalCode == "" {
		c.add(CodeInvalidDefinition, loc, "END node declares no terminal_code")
	}
	if !end.RuntimeStatus.Valid() {
		c.add(CodeInvalidDefinition, loc, "runtime_status %q is not a declared terminal status", string(end.RuntimeStatus))
	}

	dims, ok := parseCompletionMapping(end.CompletionMapping, loc, c)
	if !ok {
		return nil
	}

	ctx := lifecycle.Context{
		ApprovalRequired:               end.ApprovalRequired,
		CurrentMaterialDigest:          end.ApprovedMaterialDigest,
		CommitReceiptRef:               end.CommitReceiptRef,
		ClosurePolicyPermitsOpenRepair: end.ClosurePolicyPermitsOpenRepair,
		Persisted:                      true,
	}
	if len(end.RepairRefs) > 0 {
		ctx.RepairRef = end.RepairRefs[0]
	} else if len(end.IncidentRefs) > 0 {
		ctx.RepairRef = end.IncidentRefs[0]
	}
	if end.ApprovalRequired && end.ApprovedMaterialDigest != "" {
		ctx.Bindings = []lifecycle.ApprovalBindingState{{
			RequirementID:          n.ID,
			RequirementLevel:       true,
			ApprovedProposalDigest: end.ApprovedMaterialDigest,
			Approved:               true,
		}}
	}

	if err := lifecycle.Check(dims, ctx); err != nil {
		var vs *lifecycle.ViolationSet
		if errors.As(err, &vs) {
			for _, v := range vs.Violations {
				c.add(CodeIllegalTerminalTuple, Location{NodeID: n.ID, Field: string(v.Dimension)},
					"%s: %s", v.Rule, v.Detail)
			}
		} else {
			c.add(CodeIllegalTerminalTuple, loc, "%v", err)
		}
		return nil
	}

	if dim, within := inProfile(def.TerminalProfile, dims); !within {
		c.add(CodeTerminalNotInProfile, Location{NodeID: n.ID, Field: string(dim)},
			"terminal profile %s does not admit %s=%s", def.TerminalProfile, dim, dims.State(dim))
	}

	// An outstanding obligation is never collapsed into a discharged one.
	if len(end.OutstandingObligationRefs) > 0 {
		switch dims.Obligation {
		case lifecycle.ObligationSatisfied, lifecycle.ObligationWaived, lifecycle.ObligationNotApplicable:
			c.add(CodeObligationCollapsed, Location{NodeID: n.ID, Field: string(lifecycle.DimensionObligation)},
				"terminal declares ObligationState=%s while %d obligation(s) remain outstanding: %s",
				dims.Obligation, len(end.OutstandingObligationRefs),
				strings.Join(end.OutstandingObligationRefs, ", "))
		}
	}

	// A terminal a degraded route reaches never claims a consistent, completed
	// outcome. Jane is either promoted with a linked repair or she is not; the
	// terminal says which.
	if degradedEntry &&
		dims.Consistency == lifecycle.ConsistencyConsistent &&
		dims.Business == lifecycle.BusinessCompleted {
		c.add(CodeDegradedCollapsedToSuccess, loc,
			"terminal is reachable by a degraded outcome route but claims BusinessState=COMPLETED with ConsistencyState=CONSISTENT")
	}

	if lifecycle.ExecutionRepairRequired == dims.Execution && end.RuntimeStatus == RuntimeCompleted {
		c.add(CodeDegradedCollapsedToSuccess, loc,
			"terminal reports runtime_status=COMPLETED while ExecutionState=REPAIR_REQUIRED")
	}

	return &Terminal{
		NodeID:                    n.ID,
		TerminalCode:              end.TerminalCode,
		RuntimeStatus:             end.RuntimeStatus,
		Dimensions:                dims,
		OutstandingObligationRefs: append([]string(nil), end.OutstandingObligationRefs...),
		RepairRefs:                append([]string(nil), end.RepairRefs...),
		IncidentRefs:              append([]string(nil), end.IncidentRefs...),
	}
}
