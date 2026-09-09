package version_test

import (
	"testing"

	version "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_COMP_006_Security proves the activation boundary cannot be
// forced open: no unauthorized principal, no stale approval, no retired
// version and no implicit takeover of another workflow's active slot ever
// yields an ACTIVE version, and a caller can never coerce Resolve into
// guessing a pin it was not given.
func TestTodo_WF_COMP_006_Security(t *testing.T) {
	t.Run("zero_value_evidence_grants_nothing", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		_, err := version.Activate(store, v.CompiledPlanDigest, version.ActivationEvidence{})
		requireCode(t, err, version.CodeUnauthorizedActivation)
		if _, found, _ := store.GetActiveForWorkflow(v.WorkflowID); found {
			t.Fatal("a zero-value evidence call must never leave a version active")
		}
	})

	t.Run("authorized_but_unnamed_approver_is_still_unauthorized", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		ev := validEvidence(v)
		ev.Authorized = true
		ev.ApprovedBy = "" // claims authority but names no one accountable
		_, err := version.Activate(store, v.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeUnauthorizedActivation)
	})

	t.Run("approval_for_a_different_plan_cannot_activate_this_one", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		ev := validEvidence(v)
		// An attacker (or a stale approval ticket) presents a digest that was
		// genuinely reviewed once, just not the one being activated now.
		ev.ReviewedPlanDigest = v.CompiledPlanDigest + "-tampered"
		_, err := version.Activate(store, v.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeChangedAfterReview)
	})

	t.Run("retirement_cannot_be_bypassed_by_resubmitting_evidence", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if _, err := version.Retire(store, v.CompiledPlanDigest, "eol", "principal:release-manager", "role:change-governance", version.ActivationEvidence{ApprovedAt: fixedTime()}); err != nil {
			t.Fatalf("retire: %v", err)
		}
		// Even a fully valid, freshly reviewed, tested, authorized evidence
		// bundle must not resurrect a retired version.
		_, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v))
		requireCode(t, err, version.CodeRetiredCannotActivate)
		if _, found, _ := store.GetActiveForWorkflow(v.WorkflowID); found {
			t.Fatal("a retired version must never become active regardless of presented evidence")
		}
	})

	t.Run("a_second_workflow_cannot_seize_activation_through_a_forged_dependency", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if _, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v)); err != nil {
			t.Fatalf("activate baseline: %v", err)
		}
		// Resolving by a digest that belongs to a different workflow id must
		// fail even though the digest itself is genuinely active somewhere.
		_, err := version.Resolve(store, "hcmnext.workflows.someone_elses_workflow", version.Pin{CompiledPlanDigest: v.CompiledPlanDigest})
		requireCode(t, err, version.CodeUnknownVersion)
	})

	t.Run("resolve_never_infers_latest_or_active_from_an_unpinned_request", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if _, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v)); err != nil {
			t.Fatalf("activate: %v", err)
		}
		// Exactly one version exists and it is active, yet an unpinned
		// request must still be refused rather than quietly resolved to it.
		_, err := version.Resolve(store, v.WorkflowID, version.Pin{})
		requireCode(t, err, version.CodeInvalidPin)
	})

	t.Run("competing_activation_without_supersede_never_dislodges_the_incumbent", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if _, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v)); err != nil {
			t.Fatalf("activate baseline: %v", err)
		}
		attacker := v
		attacker.CompiledPlanDigest = v.CompiledPlanDigest + "-forged"
		// Directly attempting to activate an unpublished, forged digest must
		// be refused as unknown, never as a successful takeover.
		_, err := version.Activate(store, attacker.CompiledPlanDigest, validEvidence(v))
		requireCode(t, err, version.CodeUnknownRecord)
		active, found, err := store.GetActiveForWorkflow(v.WorkflowID)
		if err != nil || !found || active.CompiledPlanDigest != v.CompiledPlanDigest {
			t.Fatal("the legitimately active version must remain active and unaffected")
		}
	})

	t.Run("quarantine_is_not_a_side_door_around_activation_gates", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		// Quarantine only ever pulls something already active; it must not
		// double as an unauthorized way to flip a DRAFT version's status
		// without ever clearing Activate's authorization, review, test and
		// dependency gates.
		_, err := version.Quarantine(store, v.CompiledPlanDigest, "attempted bypass", "principal:nobody", "", version.ActivationEvidence{ApprovedAt: fixedTime()})
		requireCode(t, err, version.CodeNotActive)
		stored, found, err := store.GetByDigest(v.CompiledPlanDigest)
		if err != nil || !found || stored.Status != version.StatusDraft {
			t.Fatal("a rejected quarantine attempt must leave the version exactly as published")
		}
	})
}
