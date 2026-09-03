package intentdefinitions

// Registry returns the exact fourteen definitions in the reviewed draft
// contract slice. It is constructed from immutable source facts at package
// initialization and returned by value so callers cannot mutate the registry.
func Registry() []Descriptor {
	rows := []struct{ id, name, family, effect string }{
		{"hcmnext.people.change_manager", "ChangeManager", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.people.explain_worker_state", "ExplainWorkerState", "ANALYTICAL_REQUEST", "READ_ONLY"},
		{"hcmnext.people.promote_worker", "PromoteWorker", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.rewards.change_base_pay", "ChangeBasePay", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.rewards.simulate_compensation", "SimulateCompensation", "CALCULATION_REQUEST", "PURE"},
		{"hcmnext.rewards.evaluate_pay_band_position", "EvaluatePayBandPosition", "CALCULATION_REQUEST", "PURE"},
		{"hcmnext.rewards.reserve_compensation_budget", "ReserveCompensationBudget", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.rewards.release_compensation_budget", "ReleaseCompensationBudget", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.work.approve_proposal", "ApproveProposal", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.work.reject_proposal", "RejectProposal", "CHANGE_REQUEST", "INTERNAL_MUTATION"},
		{"hcmnext.intelligence.explain_transaction", "ExplainTransaction", "ANALYTICAL_REQUEST", "READ_ONLY"},
		{"hcmnext.operations.detect_drift", "DetectDrift", "ANALYTICAL_REQUEST", "READ_ONLY"},
		{"hcmnext.operations.create_repair_plan", "CreateRepairPlan", "ANALYTICAL_REQUEST", "READ_ONLY"},
		{"hcmnext.operations.simulate_repair", "SimulateRepair", "CALCULATION_REQUEST", "PURE"},
	}
	out := make([]Descriptor, 0, len(rows))
	for _, r := range rows {
		d := Descriptor{IntentTypeID: r.id, Version: 1, DisplayName: r.name, Family: r.family, SideEffectProfile: r.effect,
			Entities: []string{"subject"}, Properties: []string{"definition", "material_input"}, Reads: []string{"authoritative_state"},
			Writes: []string{"none"}, Effects: []string{r.effect}, Authority: []string{"tenant_policy", "initiator_authority"},
			Time: []string{"effective_at", "recorded_at"}, Lifecycle: Lifecycle{"REQUESTED", "NOT_STARTED", "PENDING", "NOT_APPLICABLE", "PENDING"},
			Evidence: []string{"request_digest", "decision_or_result", "provenance"}, NegativePolicy: []NegativePolicy{{"unauthorized_or_stale_input", "REJECT"}, {"unknown_required_fact", "FAIL_CLOSED"}},
			Scenario:   Scenario{"canonical-happy-path", "a valid typed request and pinned authority snapshot exist", "the definition is admitted", "the result is deterministic and evidence-linked"},
			SourceFile: "schema/schemaflux/business_intents/v1/promotion.yaml"}
		if r.id == "hcmnext.people.change_manager" {
			d.ConformanceOnly = true
			d.SourceFile = "schema/schemaflux/business_intents/v1/manager_change.yaml"
		}
		out = append(out, d)
	}
	return out
}

func ManifestDigest() (string, error) { return Digest(Registry()) }
