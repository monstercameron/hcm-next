package workflow

import (
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/capability"
)

// requiredGovernanceDecisions are the subdecisions every capability-invoking
// node must have composed before it runs. The workflow coordinates the
// results; it never implements AuthZ or legal semantics itself.
var requiredGovernanceDecisions = []GovernanceKind{
	GovernanceAuthZ,
	GovernanceLegal,
	GovernancePurpose,
	GovernanceRisk,
}

// scopeWithin reports whether child is inside parent, treating "/" as the
// scope separator. A workflow authorized for one organization scope cannot
// resolve an approver outside it.
func scopeWithin(child, parent string) bool {
	if parent == "" || child == "" {
		return false
	}
	return child == parent || strings.HasPrefix(child, parent+"/")
}

// analyzeGovernance proves that every governed action declares the decisions,
// obligations, approvals and revalidation boundaries it depends on, and that
// derived agent output never reaches an effect unvalidated (WF-COMP-005).
func analyzeGovernance(
	def *Definition,
	g *graph,
	records map[string]capability.Record,
	c *collector,
) GovernanceSummary {
	approvals := map[string]ApprovalRequirement{}
	for _, r := range def.ApprovalRequirements {
		approvals[r.ID] = r
	}
	obligations := map[string]ObligationRequirement{}
	for _, o := range def.Obligations {
		obligations[o.ID] = o
	}

	summary := GovernanceSummary{
		InsertionPoints:      map[string][]string{},
		RevalidationPoints:   map[string]RevalidationBoundary{},
		ApprovalRequirements: append([]ApprovalRequirement(nil), def.ApprovalRequirements...),
		Obligations:          append([]ObligationRequirement(nil), def.Obligations...),
	}
	sort.Slice(summary.ApprovalRequirements, func(i, j int) bool {
		return summary.ApprovalRequirements[i].ID < summary.ApprovalRequirements[j].ID
	})
	sort.Slice(summary.Obligations, func(i, j int) bool {
		return summary.Obligations[i].ID < summary.Obligations[j].ID
	})

	obligationUsed := map[string]bool{}
	obligationAtClosure := map[string]bool{}
	var decisions []string
	seenContext := map[string]bool{}

	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		loc := Location{NodeID: id}
		conf, _ := ConformanceFor(n.Type)

		for _, k := range n.Governance.RequiredDecisions {
			decisions = append(decisions, string(k))
		}

		if conf.RequiresCapability {
			declared := map[GovernanceKind]bool{}
			for _, k := range n.Governance.RequiredDecisions {
				declared[k] = true
			}
			for _, want := range requiredGovernanceDecisions {
				if !declared[want] {
					c.add(CodeMissingGovernanceEvaluation, Location{NodeID: id, Field: string(want)},
						"%s invokes a capability and must declare a %s evaluation", n.Type, want)
				}
			}
			if n.Governance.Purpose == "" {
				c.add(CodeMissingGovernanceEvaluation, Location{NodeID: id, Field: "purpose"},
					"a governed invocation declares its purpose")
			}
			if n.Governance.DataAccessManifestRef == "" {
				c.add(CodeMissingGovernanceEvaluation, Location{NodeID: id, Field: "data_access_manifest_ref"},
					"a governed invocation declares the data access manifest covering every rendered field")
			}
			switch n.Governance.RevalidationBoundary {
			case RevalidateNone, RevalidatePreExecution, RevalidatePreEffect, RevalidatePreClosure:
			default:
				c.add(CodeMissingGovernanceEvaluation, Location{NodeID: id, Field: "revalidation_boundary"},
					"revalidation_boundary %q is not declared; proposal-time allow never implies execution-time allow",
					string(n.Governance.RevalidationBoundary))
			}
			if effectClassOf(n, records).IsWrite() {
				switch n.Governance.RevalidationBoundary {
				case RevalidatePreExecution, RevalidatePreEffect:
				default:
					c.add(CodeMissingGovernanceEvaluation, Location{NodeID: id, Field: "revalidation_boundary"},
						"a mutating invocation revalidates at PRE_EXECUTION or PRE_EFFECT, not %s",
						n.Governance.RevalidationBoundary)
				}
			}
		}

		if n.Governance.RevalidationBoundary != "" && n.Governance.RevalidationBoundary != RevalidateNone {
			summary.RevalidationPoints[id] = n.Governance.RevalidationBoundary
		}

		for _, ref := range n.Governance.ApprovalRequirements {
			req, ok := approvals[ref]
			if !ok {
				c.add(CodeUnresolvedApprovalScope, Location{NodeID: id, Ref: ref},
					"approval requirement %q is referenced but not declared", ref)
				continue
			}
			if !scopeWithin(req.Scope, def.OrganizationScope) {
				c.add(CodeUnresolvedApprovalScope, Location{NodeID: id, Ref: ref},
					"approval scope %q is outside the workflow organization scope %q",
					req.Scope, def.OrganizationScope)
			}
		}

		for _, ref := range n.Governance.ObligationRefs {
			ob, ok := obligations[ref]
			if !ok {
				c.add(CodeUnresolvedObligation, Location{NodeID: id, Ref: ref},
					"obligation %q is referenced but not declared", ref)
				continue
			}
			obligationUsed[ref] = true
			if n.Type == StepEnd {
				obligationAtClosure[ref] = true
			}
			summary.InsertionPoints[string(ob.InsertionPoint)] =
				append(summary.InsertionPoints[string(ob.InsertionPoint)], id)
		}

		for _, req := range n.RequiredContext {
			key := req.Kind + "|" + strings.Join(req.FieldPaths, ",")
			if seenContext[key] {
				continue
			}
			seenContext[key] = true
			summary.RequiredContexts = append(summary.RequiredContexts, req)
		}
		_ = loc
	}

	for _, ob := range def.Obligations {
		loc := Location{Ref: ob.ID}
		if ob.InsertionPoint != InsertDefinitionPublication && !obligationUsed[ob.ID] {
			c.add(CodeUnresolvedObligation, loc,
				"obligation declares insertion point %s but no node inserts it", ob.InsertionPoint)
		}
		if ob.Mandatory && ob.InsertionPoint == InsertClosure && !obligationAtClosure[ob.ID] {
			c.add(CodeUnresolvedObligation, loc,
				"a mandatory closure obligation is proved at an END node; none references it")
		}
	}

	checkAgentOutputValidation(g, records, c)

	summary.RequiredDecisions = nil
	for _, k := range sortedStrings(decisions) {
		summary.RequiredDecisions = append(summary.RequiredDecisions, GovernanceKind(k))
	}
	sort.Slice(summary.RequiredContexts, func(i, j int) bool {
		if summary.RequiredContexts[i].Kind != summary.RequiredContexts[j].Kind {
			return summary.RequiredContexts[i].Kind < summary.RequiredContexts[j].Kind
		}
		return strings.Join(summary.RequiredContexts[i].FieldPaths, ",") <
			strings.Join(summary.RequiredContexts[j].FieldPaths, ",")
	})
	for k := range summary.InsertionPoints {
		summary.InsertionPoints[k] = sortedStrings(summary.InsertionPoints[k])
	}
	return summary
}

// checkAgentOutputValidation follows derived agent output through the mapping
// graph. Agent output is untrusted until a typed validator accepts it; routing
// it into an effect without that step is how a prompt becomes a payroll
// change.
func checkAgentOutputValidation(g *graph, records map[string]capability.Record, c *collector) {
	tainted := map[string]bool{}
	order := g.order
	if len(order) == 0 {
		order = g.sortedNodeIDs()
	}
	for _, id := range order {
		n, ok := g.nodes[id]
		if !ok {
			continue
		}
		inherited := false
		for _, m := range n.InputMappings {
			if m.Source.Kind == SourceNodeOutput && tainted[m.Source.NodeID] {
				inherited = true
				break
			}
		}
		rec, hasRec := records[id]
		agentItself := hasRec && rec.Definition.AgentEligible
		// A validator on this node clears the taint it carries onward; it
		// cannot retroactively validate an effect this same node performs.
		tainted[id] = (inherited || agentItself) && n.Governance.OutputValidatorRef == ""

		class := effectClassOf(n, records)
		if !class.IsWrite() {
			continue
		}
		switch {
		case agentItself:
			c.add(CodeUnvalidatedAgentOutput, Location{NodeID: id},
				"an agent-eligible capability performs a %s effect directly; a mutation needs a later deterministic capability",
				class)
		case inherited && n.Governance.OutputValidatorRef == "":
			c.add(CodeUnvalidatedAgentOutput, Location{NodeID: id},
				"agent-derived output reaches a %s effect with no typed output validator on the path", class)
		}
	}
}
