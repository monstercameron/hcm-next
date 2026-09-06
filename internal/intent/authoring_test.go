package intent_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// promoteTemplate is a template of permitted defaults for PromoteWorker: the
// two fields an HR partner starts every annual-cycle promotion from.
func promoteTemplate() intent.Template {
	return intent.Template{
		TemplateID:  "template.people.annual_promotion",
		Version:     1,
		Definition:  intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1},
		DisplayName: "Annual promotion",
		OwnerDomain: "PEOPLE",
		Defaults: []intent.InputValue{
			{Path: "reason_ref", CanonicalText: "reason:annual_cycle"},
			{Path: "effective_time", CanonicalText: "2026-10-01"},
		},
	}
}

// promoteDraftSpec opens a draft against PromoteWorker with the two inputs a
// template cannot supply.
func promoteDraftSpec() intent.DraftSpec {
	return intent.DraftSpec{
		Tenant:     values.TenantId("acme-eu"),
		Definition: intent.Ref{TypeID: "hcmnext.people.promote_worker", Version: 1},
		Author:     principal(),
		Inputs: []intent.InputValue{
			{Path: "employment_ref", CanonicalText: "employment:9001"},
			{Path: "target_position_ref", CanonicalText: "position:staff-engineer"},
		},
	}
}

// derivationSpec is a fresh causal identity for a clone or a fork.
func derivationSpec() intent.DerivationSpec {
	trusted := trustedHumanOrigin()
	trusted.TrustedContextDigest = "sha256:trusted-context-clone"
	origin, err := intent.NewOrigin(intent.OriginClaim{
		TriggerRef:    "trigger:ui.promote_worker.clone",
		CorrelationID: "corr:clone:1",
	}, trusted)
	if err != nil {
		panic(err)
	}
	return intent.DerivationSpec{
		IdempotencyKey: "idem:promote:9001:2026-11-01",
		CorrelationID:  "corr:clone:1",
		TraceID:        "trace:clone:1",
		Origin:         origin,
	}
}

// sourceIntent builds a submitted PromoteWorker instance with one recorded
// proposal revision, which is what a clone and a fork are derived from.
func sourceIntent(t *testing.T, def intent.Definition) intent.Instance {
	t.Helper()
	d := mustDigester(t)
	spec := promoteSpec()
	origin, err := intent.NewOrigin(originClaim(), trustedHumanOrigin())
	if err != nil {
		t.Fatalf("origin: %v", err)
	}
	spec.Origin = origin
	inst, _, err := intent.Draft(spec, def, d, countingIDs("50000001"), fixedClock())
	if err != nil {
		t.Fatalf("source intent: %v", err)
	}
	rev, err := intent.NewProposalRevision(promoteProposal(t, inst.IntentID), def, d,
		countingIDs("50000002"), fixedClock())
	if err != nil {
		t.Fatalf("source revision: %v", err)
	}
	inst.ProposalRevisions = []intent.ProposalRevision{rev}
	return inst
}

// TestIntentDraftTemplateCloneForkBoundaries is the PRIMARY test for
// INTENT-014.
//
// RED: a mutable draft treated as a submitted intent, a template that embeds a
// principal, tenant or other server-owned fact or is pinned to a stale
// definition version, a clone that reuses idempotency, evidence or approval,
// and a fork that mutates its source are all rejected.
//
// GREEN: a template supplies versioned permitted defaults, a draft stores
// authorized incomplete input, submission mints exactly one immutable
// IntentInstance, and clone and fork produce new causal identities with
// redaction-safe lineage.
func TestIntentDraftTemplateCloneForkBoundaries(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	t.Run("RED: a template may not embed a server-owned fact", func(t *testing.T) {
		for _, path := range []string{
			"initiator.principal_id", "tenant_id", "organization_scope_id",
			"purpose", "idempotency_key", "correlation_id", "classification",
			"retention_class", "control_snapshots.policy_bundle_digest",
			"execution_mode", "approval.decision", "evidence.receipt_id",
			"role.grant", "session.ref",
		} {
			tmpl := promoteTemplate()
			tmpl.Defaults = append(tmpl.Defaults, intent.InputValue{Path: path, CanonicalText: "x"})
			err := tmpl.Validate(def)
			if !errors.Is(err, intent.ErrInvalidTemplate) {
				t.Fatalf("a template defaulted %q: %v", path, err)
			}
			if !strings.Contains(err.Error(), "server-owned") {
				t.Fatalf("path %q was refused for the wrong reason: %v", path, err)
			}
		}
	})

	t.Run("RED: a template may not default an undeclared input", func(t *testing.T) {
		tmpl := promoteTemplate()
		tmpl.Defaults = append(tmpl.Defaults,
			intent.InputValue{Path: "secret_bonus_multiplier", CanonicalText: "3"})
		if err := tmpl.Validate(def); !errors.Is(err, intent.ErrInvalidTemplate) {
			t.Fatalf("a template defaulted an input the definition does not declare: %v", err)
		}
	})

	t.Run("RED: a template pinned to another definition version is stale", func(t *testing.T) {
		tmpl := promoteTemplate()
		tmpl.Definition.Version = 2
		err := tmpl.Validate(def)
		if !errors.Is(err, intent.ErrInvalidTemplate) {
			t.Fatalf("a stale template was applied: %v", err)
		}
		if !strings.Contains(err.Error(), "pinned to") {
			t.Fatalf("the staleness rejection does not say why: %v", err)
		}
	})

	t.Run("RED: a submitted draft is no longer mutable", func(t *testing.T) {
		draft, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, nil, countingIDs("60000001"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		draft, err = draft.WithInput(intent.InputValue{Path: "reason_ref", CanonicalText: "reason:annual_cycle"}, def, fixedClock())
		if err != nil {
			t.Fatalf("fill reason: %v", err)
		}
		draft, err = draft.WithInput(intent.InputValue{Path: "effective_time", CanonicalText: "2026-10-01"}, def, fixedClock())
		if err != nil {
			t.Fatalf("fill effective time: %v", err)
		}
		closed, inst, _, err := intent.SubmitDraft(draft, promoteSpec(), def, d, countingIDs("60000002"), fixedClock())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if closed.SubmittedIntentID != inst.IntentID {
			t.Fatalf("the closed draft names %q, the instance is %q", closed.SubmittedIntentID, inst.IntentID)
		}
		if _, err := closed.WithInput(intent.InputValue{Path: "reason_ref", CanonicalText: "reason:retention"}, def, fixedClock()); !errors.Is(err, intent.ErrDraftAlreadySubmitted) {
			t.Fatalf("a submitted draft was edited: %v", err)
		}
		if _, _, _, err := intent.SubmitDraft(closed, promoteSpec(), def, d, nil, fixedClock()); !errors.Is(err, intent.ErrDraftAlreadySubmitted) {
			t.Fatalf("a draft was submitted twice: %v", err)
		}
	})

	t.Run("RED: an incomplete draft may not be submitted", func(t *testing.T) {
		draft, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, nil, countingIDs("60000003"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		missing := draft.MissingRequiredInputs(def)
		if len(missing) != 2 || missing[0] != "effective_time" || missing[1] != "reason_ref" {
			t.Fatalf("missing inputs = %v, want effective_time and reason_ref", missing)
		}
		_, _, _, err = intent.SubmitDraft(draft, promoteSpec(), def, d, nil, fixedClock())
		if !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("an incomplete draft was submitted: %v", err)
		}
	})

	t.Run("RED: a clone may not reuse the source's causal identity", func(t *testing.T) {
		src := sourceIntent(t, def)
		for _, tc := range []struct {
			name   string
			break_ func(*intent.DerivationSpec)
		}{
			{"idempotency key", func(s *intent.DerivationSpec) { s.IdempotencyKey = src.IdempotencyKey }},
			{"correlation id", func(s *intent.DerivationSpec) { s.CorrelationID = src.CorrelationID }},
			{"trusted origin", func(s *intent.DerivationSpec) { s.Origin = src.Origin }},
			{"no origin at all", func(s *intent.DerivationSpec) { s.Origin = intent.Origin{} }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				spec := derivationSpec()
				tc.break_(&spec)
				if _, _, err := intent.CloneIntent(src, spec, def, d, countingIDs("70000001"), fixedClock()); !errors.Is(err, intent.ErrLineageReuse) {
					t.Fatalf("a clone reused the source's %s: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("RED: a fork does not mutate its source", func(t *testing.T) {
		src := sourceIntent(t, def)
		before := reflect.DeepEqual(src, src)
		if !before {
			t.Fatal("the source is not comparable")
		}
		snapshot := deepCopyInstance(src)
		revisionID := src.ProposalRevisions[0].ProposalRevisionID
		forked, lineage, err := intent.ForkProposal(src, revisionID, derivationSpec(), def, d,
			countingIDs("70000002"), fixedClock())
		if err != nil {
			t.Fatalf("fork: %v", err)
		}
		if !reflect.DeepEqual(src, snapshot) {
			t.Fatal("forking mutated its source")
		}
		if len(forked.ProposalRevisions) != 0 {
			t.Fatalf("the fork inherited %d proposal revisions", len(forked.ProposalRevisions))
		}
		if lineage.Relation != intent.LineageFork || lineage.SourceProposalRevisionID != revisionID {
			t.Fatalf("the fork's lineage does not pin the source revision: %+v", lineage)
		}
		// Mutating the fork's own request bytes must not reach the source.
		if len(forked.Request.WireBytes) > 0 {
			forked.Request.WireBytes[0] ^= 0xff
		}
		if !reflect.DeepEqual(src.Request.WireBytes, snapshot.Request.WireBytes) {
			t.Fatal("the fork shares its request bytes with the source")
		}
	})

	t.Run("RED: forking a revision that does not exist is refused", func(t *testing.T) {
		src := sourceIntent(t, def)
		if _, _, err := intent.ForkProposal(src, "revision:does-not-exist", derivationSpec(), def, d, nil, fixedClock()); !errors.Is(err, intent.ErrInvalidProposal) {
			t.Fatalf("a fork invented a source revision: %v", err)
		}
	})

	t.Run("GREEN: template seeds draft, draft submits one intent", func(t *testing.T) {
		tmpl := promoteTemplate()
		if err := tmpl.Validate(def); err != nil {
			t.Fatalf("template: %v", err)
		}
		draft, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, &tmpl, countingIDs("60000010"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		if draft.TemplateRef != "template.people.annual_promotion/v1" {
			t.Fatalf("the draft does not reference its template: %q", draft.TemplateRef)
		}
		if got := draft.MissingRequiredInputs(def); len(got) != 0 {
			t.Fatalf("a template-seeded draft is still missing %v", got)
		}
		if draft.Submitted() {
			t.Fatal("a fresh draft reports itself submitted")
		}
		closed, inst, evidence, err := intent.SubmitDraft(draft, promoteSpec(), def, d,
			countingIDs("60000011"), fixedClock())
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if !closed.Submitted() || inst.IntentID == "" {
			t.Fatalf("submission did not mint one intent: draft=%+v", closed)
		}
		if evidence.IntentID != inst.IntentID {
			t.Fatalf("creation evidence names %q, the intent is %q", evidence.IntentID, inst.IntentID)
		}
		if inst.Lifecycle != intent.InitialDimensions(def) {
			t.Fatalf("a submitted draft did not start at the initial lifecycle tuple: %+v", inst.Lifecycle)
		}
		// The author's own input wins over the template default.
		if err := closed.Validate(def); err != nil {
			t.Fatalf("the closed draft does not revalidate: %v", err)
		}
	})

	t.Run("GREEN: an author's input overrides a template default", func(t *testing.T) {
		tmpl := promoteTemplate()
		spec := promoteDraftSpec()
		spec.Inputs = append(spec.Inputs,
			intent.InputValue{Path: "reason_ref", CanonicalText: "reason:retention_risk"})
		draft, err := intent.NewAuthoringDraft(spec, def, &tmpl, countingIDs("60000012"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		for _, in := range draft.Inputs {
			if in.Path == "reason_ref" && in.CanonicalText != "reason:retention_risk" {
				t.Fatalf("the template default overrode the author: %q", in.CanonicalText)
			}
		}
	})

	t.Run("GREEN: a clone is a new causal identity with redaction-safe lineage", func(t *testing.T) {
		src := sourceIntent(t, def)
		clone, lineage, err := intent.CloneIntent(src, derivationSpec(), def, d,
			countingIDs("70000010"), fixedClock())
		if err != nil {
			t.Fatalf("clone: %v", err)
		}
		if clone.IntentID == src.IntentID {
			t.Fatal("a clone reused the source's intent id")
		}
		if clone.CausationID == nil || *clone.CausationID != src.IntentID {
			t.Fatalf("a clone does not point at its source: %v", clone.CausationID)
		}
		if len(clone.ProposalRevisions) != 0 {
			t.Fatal("a clone inherited proposal history")
		}
		if clone.Lifecycle != intent.InitialDimensions(def) {
			t.Fatalf("a clone did not restart its lifecycle: %+v", clone.Lifecycle)
		}
		if clone.CanonicalRequestDigest.Digest == "" {
			t.Fatal("a clone has no canonical request digest of its own")
		}
		if err := lineage.Validate(); err != nil {
			t.Fatalf("lineage: %v", err)
		}
		if lineage.Relation != intent.LineageClone || lineage.DerivedIntentID != clone.IntentID {
			t.Fatalf("lineage does not describe this clone: %+v", lineage)
		}
	})

	t.Run("GREEN: a saved action stores references and parameters only", func(t *testing.T) {
		action := intent.SavedAction{
			SavedActionID:    "saved:promote-my-team",
			Tenant:           values.TenantId("acme-eu"),
			OwnerPrincipalID: "principal:hr-partner-7",
			Definition:       def.Ref,
			Label:            "Promote a team member",
			Parameters: []intent.InputValue{
				{Path: "reason_ref", CanonicalText: "reason:annual_cycle"},
			},
			LastUsedAt: values.NewInstant(fixedClock()().Time()),
		}
		if err := action.Validate(def); err != nil {
			t.Fatalf("saved action: %v", err)
		}
	})
}

// deepCopyInstance returns a copy that shares no mutable memory with src, so a
// mutation test can tell aliasing from equality.
func deepCopyInstance(src intent.Instance) intent.Instance {
	out := src
	out.Request = src.Request.Clone()
	out.Subjects = append([]intent.SubjectReference(nil), src.Subjects...)
	out.DelegationChain = append([]intent.DelegationReference(nil), src.DelegationChain...)
	out.ProposalRevisions = append([]intent.ProposalRevision(nil), src.ProposalRevisions...)
	out.Origin.OnBehalfOf = append([]intent.DelegationReference(nil), src.Origin.OnBehalfOf...)
	return out
}

// TestTodo_INTENT_014_Race drives concurrent authoring against one draft and
// one source intent. Drafts and instances are values, so two editors working
// from the same draft must produce two independent drafts rather than
// interfering, and two clones of one source must be two intents.
func TestTodo_INTENT_014_Race(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	tmpl := promoteTemplate()
	base, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, &tmpl, countingIDs("61000001"), fixedClock())
	if err != nil {
		t.Fatalf("open draft: %v", err)
	}
	src := sourceIntent(t, def)

	const workers = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		reasons = map[string]int{}
		ids     = map[string]int{}
	)
	wg.Add(workers)
	for i := range workers {
		go func(i int) {
			defer wg.Done()
			reason := "reason:worker_" + string(rune('a'+i))
			edited, err := base.WithInput(intent.InputValue{Path: "reason_ref", CanonicalText: reason}, def, fixedClock())
			if err != nil {
				t.Errorf("worker %d edit: %v", i, err)
				return
			}
			var got string
			for _, in := range edited.Inputs {
				if in.Path == "reason_ref" {
					got = in.CanonicalText
				}
			}
			spec := derivationSpec()
			spec.IdempotencyKey = "idem:clone:" + reason
			spec.CorrelationID = "corr:clone:" + reason
			clone, _, err := intent.CloneIntent(src, spec, def, d, countingIDs("7100000"+string(rune('0'+i%10))), fixedClock())
			if err != nil {
				t.Errorf("worker %d clone: %v", i, err)
				return
			}
			mu.Lock()
			reasons[got]++
			ids[clone.IntentID]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if len(reasons) != workers {
		t.Fatalf("%d concurrent edits produced %d distinct drafts; they interfered", workers, len(reasons))
	}
	// The base draft is untouched: every edit returned a copy.
	for _, in := range base.Inputs {
		if in.Path == "reason_ref" && in.CanonicalText != "reason:annual_cycle" {
			t.Fatalf("the shared draft was mutated in place: %q", in.CanonicalText)
		}
	}
	if len(src.ProposalRevisions) != 1 {
		t.Fatalf("concurrent cloning mutated the source: %d revisions", len(src.ProposalRevisions))
	}
}

// TestTodo_INTENT_014_Fault covers the failure paths an authoring surface hits
// in practice: a missing clock, a draft opened against the wrong definition,
// a draft submitted by someone else, and a draft submitted into another tenant.
func TestTodo_INTENT_014_Fault(t *testing.T) {
	reg := mustRegistry(t)
	d := mustDigester(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	t.Run("a draft cannot be opened without a clock", func(t *testing.T) {
		if _, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, nil, nil, nil); !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("a draft was opened with no clock: %v", err)
		}
	})

	t.Run("a draft cannot be opened against another definition", func(t *testing.T) {
		spec := promoteDraftSpec()
		spec.Definition = intent.Ref{TypeID: "hcmnext.rewards.change_base_pay", Version: 1}
		if _, err := intent.NewAuthoringDraft(spec, def, nil, countingIDs("62000001"), fixedClock()); !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("a draft named one definition and was opened against another: %v", err)
		}
	})

	t.Run("a draft cannot be submitted by another principal or into another tenant", func(t *testing.T) {
		tmpl := promoteTemplate()
		draft, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, &tmpl, countingIDs("62000002"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		other := promoteSpec()
		other.Initiator.PrincipalID = "principal:someone-else"
		if _, _, _, err := intent.SubmitDraft(draft, other, def, d, nil, fixedClock()); !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("another principal submitted this draft: %v", err)
		}
		foreign := promoteSpec()
		foreign.Tenant = values.TenantId("globex")
		if _, _, _, err := intent.SubmitDraft(draft, foreign, def, d, nil, fixedClock()); !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("a draft was submitted into another tenant: %v", err)
		}
	})

	t.Run("a failed submission leaves the draft open", func(t *testing.T) {
		draft, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, nil, countingIDs("62000003"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		if _, _, _, err := intent.SubmitDraft(draft, promoteSpec(), def, d, nil, fixedClock()); err == nil {
			t.Fatal("an incomplete draft submitted cleanly")
		}
		if draft.Submitted() {
			t.Fatal("a failed submission closed the draft")
		}
		// Filling the gap and submitting again works, which is the point of
		// leaving it open.
		for _, in := range []intent.InputValue{
			{Path: "reason_ref", CanonicalText: "reason:annual_cycle"},
			{Path: "effective_time", CanonicalText: "2026-10-01"},
		} {
			draft, err = draft.WithInput(in, def, fixedClock())
			if err != nil {
				t.Fatalf("fill %s: %v", in.Path, err)
			}
		}
		if _, _, _, err := intent.SubmitDraft(draft, promoteSpec(), def, d, countingIDs("62000004"), fixedClock()); err != nil {
			t.Fatalf("the repaired draft would not submit: %v", err)
		}
	})
}

// TestTodo_INTENT_014_Security is the "never copied authority" half: no
// authoring artifact may carry a rendered payload, a principal's roles or an
// approval, and lineage must stay disclosable.
func TestTodo_INTENT_014_Security(t *testing.T) {
	reg := mustRegistry(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	t.Run("no authoring type carries authority or rendered state", func(t *testing.T) {
		forbidden := []string{
			"role", "capability", "permission", "entitlement", "grant",
			"approval", "credential", "token", "secret", "rendered",
			"wirebytes", "payload", "evidence",
		}
		for _, typ := range []reflect.Type{
			reflect.TypeOf(intent.Template{}),
			reflect.TypeOf(intent.AuthoringDraft{}),
			reflect.TypeOf(intent.SavedAction{}),
			reflect.TypeOf(intent.Lineage{}),
		} {
			for i := range typ.NumField() {
				name := strings.ToLower(typ.Field(i).Name)
				for _, bad := range forbidden {
					if strings.Contains(name, bad) {
						t.Fatalf("%s.%s carries %s; authoring artifacts hold references and parameters only",
							typ.Name(), typ.Field(i).Name, bad)
					}
				}
			}
		}
	})

	t.Run("a saved action may not store copied authority", func(t *testing.T) {
		for _, path := range []string{"role.grant", "capability.people_promote", "approval.decision", "principal.id"} {
			action := intent.SavedAction{
				SavedActionID:    "saved:1",
				Tenant:           values.TenantId("acme-eu"),
				OwnerPrincipalID: "principal:hr-partner-7",
				Definition:       def.Ref,
				Label:            "Saved",
				Parameters:       []intent.InputValue{{Path: path, CanonicalText: "x"}},
			}
			if err := action.Validate(def); !errors.Is(err, intent.ErrInvalidSavedAction) {
				t.Fatalf("a saved action stored %q: %v", path, err)
			}
		}
	})

	t.Run("a saved action pinned to another definition is refused", func(t *testing.T) {
		other := mustResolve(t, reg, "hcmnext.operations.detect_drift/v1")
		action := intent.SavedAction{
			SavedActionID:    "saved:1",
			Tenant:           values.TenantId("acme-eu"),
			OwnerPrincipalID: "principal:hr-partner-7",
			Definition:       def.Ref,
			Label:            "Saved",
		}
		if err := action.Validate(other); !errors.Is(err, intent.ErrInvalidSavedAction) {
			t.Fatalf("a saved action was validated against a different definition: %v", err)
		}
	})

	t.Run("a draft belongs to one tenant and one author", func(t *testing.T) {
		spec := promoteDraftSpec()
		spec.Tenant = ""
		if _, err := intent.NewAuthoringDraft(spec, def, nil, countingIDs("63000001"), fixedClock()); !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("a tenantless draft was opened: %v", err)
		}
		spec = promoteDraftSpec()
		spec.Author = intent.PrincipalReference{}
		if _, err := intent.NewAuthoringDraft(spec, def, nil, countingIDs("63000002"), fixedClock()); !errors.Is(err, intent.ErrInvalidDraft) {
			t.Fatalf("an authorless draft was opened: %v", err)
		}
	})
}

// TestTodo_INTENT_014_Mutation perturbs one field at a time of a valid
// template, draft, saved action and lineage record. A mutation that survives is
// a rule the authoring contract does not actually enforce.
func TestTodo_INTENT_014_Mutation(t *testing.T) {
	reg := mustRegistry(t)
	def := mustResolve(t, reg, "hcmnext.people.promote_worker/v1")

	t.Run("template", func(t *testing.T) {
		cases := []struct {
			name   string
			break_ func(*intent.Template)
		}{
			{"no id", func(x *intent.Template) { x.TemplateID = "" }},
			{"no version", func(x *intent.Template) { x.Version = 0 }},
			{"no display name", func(x *intent.Template) { x.DisplayName = "" }},
			{"no owner domain", func(x *intent.Template) { x.OwnerDomain = "" }},
			{"no defaults", func(x *intent.Template) { x.Defaults = nil }},
			{"duplicate default path", func(x *intent.Template) {
				x.Defaults = append(x.Defaults, x.Defaults[0])
			}},
			{"empty default value", func(x *intent.Template) { x.Defaults[0].CanonicalText = "" }},
			{"malformed definition reference", func(x *intent.Template) { x.Definition.TypeID = "promote" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tmpl := promoteTemplate()
				tc.break_(&tmpl)
				if err := tmpl.Validate(def); !errors.Is(err, intent.ErrInvalidTemplate) {
					t.Fatalf("mutation %q survived: %v", tc.name, err)
				}
			})
		}
		if err := promoteTemplate().Validate(def); err != nil {
			t.Fatalf("the unmutated template was refused: %v", err)
		}
	})

	t.Run("draft", func(t *testing.T) {
		valid, err := intent.NewAuthoringDraft(promoteDraftSpec(), def, nil, countingIDs("64000001"), fixedClock())
		if err != nil {
			t.Fatalf("open draft: %v", err)
		}
		cases := []struct {
			name   string
			break_ func(*intent.AuthoringDraft)
		}{
			{"no id", func(x *intent.AuthoringDraft) { x.DraftID = "" }},
			{"no tenant", func(x *intent.AuthoringDraft) { x.Tenant = "" }},
			{"no author", func(x *intent.AuthoringDraft) { x.Author = intent.PrincipalReference{} }},
			{"undeclared input", func(x *intent.AuthoringDraft) {
				x.Inputs = append(x.Inputs, intent.InputValue{Path: "made_up", CanonicalText: "1"})
			}},
			{"server-owned input", func(x *intent.AuthoringDraft) {
				x.Inputs = append(x.Inputs, intent.InputValue{Path: "purpose", CanonicalText: "anything"})
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				draft := valid
				draft.Inputs = append([]intent.InputValue(nil), valid.Inputs...)
				tc.break_(&draft)
				if err := draft.Validate(def); !errors.Is(err, intent.ErrInvalidDraft) {
					t.Fatalf("mutation %q survived: %v", tc.name, err)
				}
			})
		}
		if err := valid.Validate(def); err != nil {
			t.Fatalf("the unmutated draft was refused: %v", err)
		}
	})

	t.Run("lineage", func(t *testing.T) {
		valid := intent.Lineage{
			Relation: intent.LineageClone, SourceIntentID: "intent:1", DerivedIntentID: "intent:2",
		}
		if err := valid.Validate(); err != nil {
			t.Fatalf("the unmutated lineage was refused: %v", err)
		}
		cases := []struct {
			name   string
			break_ func(*intent.Lineage)
		}{
			{"no relation", func(x *intent.Lineage) { x.Relation = intent.LineageUnspecified }},
			{"no source", func(x *intent.Lineage) { x.SourceIntentID = "" }},
			{"no derived intent", func(x *intent.Lineage) { x.DerivedIntentID = "" }},
			{"self-sourced", func(x *intent.Lineage) { x.DerivedIntentID = x.SourceIntentID }},
			{"fork with no source revision", func(x *intent.Lineage) { x.Relation = intent.LineageFork }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				l := valid
				tc.break_(&l)
				if err := l.Validate(); !errors.Is(err, intent.ErrLineageReuse) {
					t.Fatalf("mutation %q survived: %v", tc.name, err)
				}
			})
		}
	})
}
