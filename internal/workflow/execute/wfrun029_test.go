package execute

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// --- WF-RUN-029 fixtures ----------------------------------------------------
//
// The PRIMARY, FAULT and SECURITY cases reuse [newWfrun028Fixture]'s durable,
// COMPLETED WorkItem parked on a WAITING instance and attach a [CurrencyGuard]
// to the driver that resumes it (WF-RUN-029's "the driver" is the same one
// WF-RUN-028 already loads its WorkItem through). PROPERTY, GOLDEN and
// MUTATION exercise [CurrencyGuard.Check] directly: it reads only through the
// [runtime.ProposalFacts]/[runtime.ApprovalFacts] ports it is handed, so a
// bare in-memory double is a faithful double and needs no database.

// materiallyChangedRevision returns a copy of rev whose PROPOSAL material
// encoding differs (Purpose.Purpose is a named material field -- see
// internal/intent/proposal.go's MaterialPayload) -- the shape
// [runtime.ProposalSupersessionFact.CurrentRevision] carries when a
// supersession is a genuine material change.
func materiallyChangedRevision(rev intent.ProposalRevision) intent.ProposalRevision {
	changed := rev
	changed.Purpose.Purpose = "materially-different-purpose"
	return changed
}

// approvedApprovalFacts reports rev as approved by exactly one standing
// decision correctly bound to rev's own material digest.
func approvedApprovalFacts(rev intent.ProposalRevision) runtime.MemoryApprovalFacts {
	return runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		rev.ProposalRevisionID: {{
			DecisionID: "decision:currency-standing", Outcome: runtime.ApprovalOutcomeApproved,
			ProposalDigest: rev.MaterialDigest.Digest,
		}},
	}}
}

// --- TestTodo_WF_RUN_029 -----------------------------------------------------

// TestTodo_WF_RUN_029 is the PRIMARY test: a Resume whose pinned proposal was
// superseded by a materially different revision is blocked, and the instance
// is durably moved to BLOCKED through the runtime store's existing
// transition path -- never silently completed.
func TestTodo_WF_RUN_029(t *testing.T) {
	f := newWfrun028Fixture(t, "currency-primary")
	changed := materiallyChangedRevision(f.proposal)
	guard := &CurrencyGuard{
		Proposal: runtime.MemoryProposalFacts{Facts: map[string]runtime.ProposalSupersessionFact{
			f.proposal.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:newer", CurrentRevision: &changed},
		}},
		Approval: approvedApprovalFacts(f.proposal),
	}

	_, err := f.driverWithCurrency(t, guard).Resume(context.Background(), f.resumeRequest())
	if !errors.Is(err, ErrCurrencyBlocked) {
		t.Fatalf("Resume error = %v, want currency blocked", err)
	}

	var inst runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var loadErr error
		inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		return loadErr
	})
	if inst.RuntimeStatus != runtime.InstanceBlocked {
		t.Fatalf("instance runtime status = %s, want BLOCKED", inst.RuntimeStatus)
	}
	if inst.InstanceVersion != f.instanceVersion+1 {
		t.Fatalf("instance version after block = %d, want %d", inst.InstanceVersion, f.instanceVersion+1)
	}
}

// currencyCase is one PROPERTY table row: a combination of the three
// currency facts [CurrencyGuard.Check] revalidates, and whether the
// combination should block.
type currencyCase struct {
	name        string
	superseded  bool
	material    bool
	approved    bool
	invalidated bool
	mismatched  bool
	wantBlocked bool
}

// runCurrencyCase builds the ports currencyCase describes and asserts
// [CurrencyGuard.Check]'s verdict against WantBlocked -- the PROPERTY this
// test proves is "Blocked if and only if the proposal was superseded with a
// material change, the approval binding is mismatched, or no standing,
// non-invalidated approval remains".
func runCurrencyCase(t *testing.T, tc currencyCase) {
	t.Helper()
	rev := baseCurrencyRevision()
	proposalFacts := runtime.MemoryProposalFacts{}
	if tc.superseded {
		current := rev
		if tc.material {
			current = materiallyChangedRevision(rev)
		}
		proposalFacts.Facts = map[string]runtime.ProposalSupersessionFact{
			rev.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:newer", CurrentRevision: &current},
		}
	}
	decision := runtime.ApprovalDecisionFact{
		DecisionID: "decision:property", Outcome: runtime.ApprovalOutcomeRejected,
		ProposalDigest: rev.MaterialDigest.Digest, Invalidated: tc.invalidated,
	}
	if tc.approved {
		decision.Outcome = runtime.ApprovalOutcomeApproved
	}
	if tc.mismatched {
		decision.ProposalDigest = "sha256:" + "0000000000000000000000000000000000000000000000000000000000ff"
	}
	approvalFacts := runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		rev.ProposalRevisionID: {decision},
	}}

	guard := CurrencyGuard{Proposal: proposalFacts, Approval: approvalFacts}
	verdict, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if verdict.Blocked != tc.wantBlocked {
		t.Fatalf("case %q: Blocked = %v (reason %q), want %v", tc.name, verdict.Blocked, verdict.Reason, tc.wantBlocked)
	}
}

func baseCurrencyRevision() intent.ProposalRevision {
	revisionID := "proposal:currency:1"
	return intent.ProposalRevision{
		IntentID: "intent:currency", ProposalRevisionID: revisionID, Revision: 1,
		MaterialDigest: digest.Reference{
			ProfileID: "PROPOSAL", ProfileVersion: 1,
			SchemaID: "hcmnext.intent.ProposalRevision", SchemaVersion: 1,
			AlgorithmID: "sha256", CanonicalLength: 42,
			Digest:             "sha256:1111111111111111111111111111111111111111111111111111111111aa",
			ScopeBindingDigest: "sha256:2222222222222222222222222222222222222222222222222222222222bb",
			ProposalRevisionID: &revisionID,
		},
	}
}

// TestTodo_WF_RUN_029_Property proves the currency verdict for every
// combination of supersession, materiality, approval and binding this ticket
// names.
func TestTodo_WF_RUN_029_Property(t *testing.T) {
	cases := []currencyCase{
		{name: "current, standing approval", superseded: false, approved: true, wantBlocked: false},
		{name: "superseded but immaterial, standing approval", superseded: true, material: false, approved: true, wantBlocked: false},
		{name: "superseded with material change", superseded: true, material: true, approved: true, wantBlocked: true},
		{name: "current, no approval", superseded: false, approved: false, wantBlocked: true},
		{name: "current, only an invalidated approval", superseded: false, approved: true, invalidated: true, wantBlocked: true},
		{name: "current, only a rejected decision", superseded: false, approved: false, invalidated: false, wantBlocked: true},
		{name: "approval bound to another digest", superseded: false, approved: true, mismatched: true, wantBlocked: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCurrencyCase(t, tc)
		})
	}
}

// TestTodo_WF_RUN_029_Golden pins the exact reason and revalidation-target
// fields [CurrencyGuard.Check] reports for one blocked and one immaterial
// scenario -- a drift in either is a behavior change, not a refactor.
func TestTodo_WF_RUN_029_Golden(t *testing.T) {
	rev := baseCurrencyRevision()

	t.Run("blocked on supersession names the reason", func(t *testing.T) {
		changed := materiallyChangedRevision(rev)
		guard := CurrencyGuard{
			Proposal: runtime.MemoryProposalFacts{Facts: map[string]runtime.ProposalSupersessionFact{
				rev.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:golden-newer", CurrentRevision: &changed},
			}},
			Approval: approvedApprovalFacts(rev),
		}
		verdict, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
			TenantID: uuid.New(), InstanceID: uuid.New(), Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !verdict.Blocked || verdict.Reason != ReasonCurrencyProposalSuperseded {
			t.Fatalf("verdict = %+v, want Blocked with reason %q", verdict, ReasonCurrencyProposalSuperseded)
		}
	})

	t.Run("immaterial supersession is recorded, not blocked", func(t *testing.T) {
		current := rev
		guard := CurrencyGuard{
			Proposal: runtime.MemoryProposalFacts{Facts: map[string]runtime.ProposalSupersessionFact{
				rev.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:golden-revalidated", CurrentRevision: &current},
			}},
			Approval: approvedApprovalFacts(rev),
		}
		verdict, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
			TenantID: uuid.New(), InstanceID: uuid.New(), Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if verdict.Blocked || !verdict.Immaterial || verdict.RevalidatedAgainstRevisionID != rev.ProposalRevisionID {
			t.Fatalf("verdict = %+v, want an unblocked, recorded immaterial revalidation against %q", verdict, rev.ProposalRevisionID)
		}
	})
}

// TestTodo_WF_RUN_029_Fault proves an immaterial change never blocks: a
// Resume whose pinned proposal was revalidated (superseded in the store, but
// with an unchanged material result) reaches COMPLETE exactly as it would
// with no CurrencyGuard configured at all.
func TestTodo_WF_RUN_029_Fault(t *testing.T) {
	f := newWfrun028Fixture(t, "currency-fault")
	current := f.proposal
	guard := &CurrencyGuard{
		Proposal: runtime.MemoryProposalFacts{Facts: map[string]runtime.ProposalSupersessionFact{
			f.proposal.ProposalRevisionID: {Superseded: true, SupersededByRevisionID: "revision:revalidated", CurrentRevision: &current},
		}},
		Approval: approvedApprovalFacts(f.proposal),
	}

	result, err := f.driverWithCurrency(t, guard).Resume(context.Background(), f.resumeRequest())
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Status != StatusComplete {
		t.Fatalf("result = %+v, want COMPLETE (an immaterial revalidation must never block)", result)
	}
}

// recordingProposalFacts records the tenantID [CurrencyGuard.Check] passes
// it, so TestTodo_WF_RUN_029_Security can prove the check never substitutes
// or drops the caller's own tenant scope.
type recordingProposalFacts struct {
	seenTenant *uuid.UUID
}

func (f *recordingProposalFacts) Supersession(_ context.Context, _ runtime.Executor, tenantID uuid.UUID, _ intent.ProposalRevision) (runtime.ProposalSupersessionFact, error) {
	f.seenTenant = &tenantID
	return runtime.ProposalSupersessionFact{}, nil
}

// TestTodo_WF_RUN_029_Security proves CurrencyGuard.Check always passes the
// requesting call's own tenant id to ProposalFacts/ApprovalFacts, never a
// zero value or another tenant's -- the fact ports are the only thing
// standing between a parked instance and a cross-tenant currency read.
func TestTodo_WF_RUN_029_Security(t *testing.T) {
	f := newWfrun028Fixture(t, "currency-security")
	recorder := &recordingProposalFacts{}
	guard := CurrencyGuard{Proposal: recorder, Approval: approvedApprovalFacts(f.proposal)}

	if _, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
		TenantID: f.tenantID, InstanceID: f.instanceID, Proposal: runtime.ProposalBinding{Revision: f.proposal}, CheckedAt: f.at,
	}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if recorder.seenTenant == nil || *recorder.seenTenant != f.tenantID {
		t.Fatalf("ProposalFacts saw tenant %v, want %s", recorder.seenTenant, f.tenantID)
	}
}

// TestTodo_WF_RUN_029_Mutation proves the approval-binding-mismatch check
// does not short-circuit on the first correctly bound decision: a second
// decision bound to a different digest still blocks the whole revalidation,
// even though the first one alone would have satisfied approval.
func TestTodo_WF_RUN_029_Mutation(t *testing.T) {
	rev := baseCurrencyRevision()
	approvalFacts := runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		rev.ProposalRevisionID: {
			{DecisionID: "decision:good", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: rev.MaterialDigest.Digest},
			{DecisionID: "decision:bad", Outcome: runtime.ApprovalOutcomeApproved, ProposalDigest: "sha256:" + "9999999999999999999999999999999999999999999999999999999999aa"},
		},
	}}
	guard := CurrencyGuard{Proposal: runtime.MemoryProposalFacts{}, Approval: approvalFacts}
	verdict, err := guard.Check(context.Background(), nil, CurrencyCheckRequest{
		TenantID: uuid.New(), InstanceID: uuid.New(), Proposal: runtime.ProposalBinding{Revision: rev}, CheckedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !verdict.Blocked || verdict.Reason != ReasonCurrencyApprovalBindingMismatch {
		t.Fatalf("verdict = %+v, want Blocked with reason %q", verdict, ReasonCurrencyApprovalBindingMismatch)
	}
}
