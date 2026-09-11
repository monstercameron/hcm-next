package workitem_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// queueItem builds a stored-shaped WorkItem directly (the membership rules
// under test read stored state, so the fixture needs no store).
func queueItem(mut func(*workitem.WorkItem)) workitem.WorkItem {
	item := workitem.WorkItem{
		TenantID:    uuid.New(),
		WorkItemID:  uuid.New(),
		ItemVersion: 3,
		Kind:        workitem.KindTask,
		WorkType:    "worktype.test/v1",
		Status:      workitem.StatusAssigned,
		OwnerKind:   workitem.OwnerPrincipal,
		OwnerRef:    "principal:amy",
		Visibility:  workitem.VisibilityAssigneeOnly,
		DeadlineAt:  fixedInstant.Add(24 * time.Hour),
		CreatedAt:   fixedInstant,
	}
	if mut != nil {
		mut(&item)
	}
	return item
}

func candidateAssignment(principals ...string) workitem.Assignment {
	return workitem.Assignment{
		Resolution: humanwork.Resolution{
			RequirementID: "req.test/v1",
			Outcome:       humanwork.OutcomeResolved,
			Candidates:    candidateSet(principals...),
			ResolvedAt:    values.NewInstant(fixedInstant),
			EffectiveAt:   values.NewInstant(fixedInstant),
		},
	}
}

func candidateSet(principals ...string) []humanwork.Candidate {
	out := make([]humanwork.Candidate, len(principals))
	for i, p := range principals {
		out[i] = humanwork.Candidate{PrincipalID: p, Via: humanwork.SourceDirect}
	}
	return out
}

func TestWorkItemMembershipFollowsClaimOwnerAndCandidateEvidence(t *testing.T) {
	now := fixedInstant

	t.Run("empty principal is never a member", func(t *testing.T) {
		if m := workitem.MembershipOf(queueItem(nil), "", now); m != workitem.MembershipNone {
			t.Fatalf("empty principal membership = %v", m)
		}
	})
	t.Run("live claimant outranks owner", func(t *testing.T) {
		expiry := now.Add(time.Hour)
		item := queueItem(func(w *workitem.WorkItem) {
			w.Status = workitem.StatusClaimed
			w.ClaimedBy = "principal:amy"
			w.ClaimedAt = ptrTime(now)
			w.ClaimExpiresAt = &expiry
		})
		if m := workitem.MembershipOf(item, "principal:amy", now); m != workitem.MembershipClaimant {
			t.Fatalf("live claimant membership = %v", m)
		}
	})
	t.Run("expired claim reverts to owner standing", func(t *testing.T) {
		expired := now.Add(-time.Hour)
		item := queueItem(func(w *workitem.WorkItem) {
			w.Status = workitem.StatusClaimed
			w.ClaimedBy = "principal:amy"
			w.ClaimExpiresAt = &expired
		})
		if m := workitem.MembershipOf(item, "principal:amy", now); m != workitem.MembershipAssignee {
			t.Fatalf("expired claim membership = %v, want assignee", m)
		}
	})
	t.Run("other principal is no member", func(t *testing.T) {
		if m := workitem.MembershipOf(queueItem(nil), "principal:bob", now); m != workitem.MembershipNone {
			t.Fatalf("unrelated principal membership = %v", m)
		}
	})
	t.Run("candidate set member is a candidate", func(t *testing.T) {
		item := queueItem(func(w *workitem.WorkItem) {
			w.Status = workitem.StatusAvailable
			w.OwnerKind = workitem.OwnerCandidateSet
			w.OwnerRef = "candidates:req.test/v1@sha256:x"
			w.Visibility = workitem.VisibilityCandidateSet
			w.Assignment = candidateAssignment("principal:amy", "principal:bob")
		})
		if m := workitem.MembershipOf(item, "principal:bob", now); m != workitem.MembershipCandidate {
			t.Fatalf("candidate membership = %v", m)
		}
	})
	t.Run("recused candidate is no member", func(t *testing.T) {
		item := queueItem(func(w *workitem.WorkItem) {
			w.Status = workitem.StatusAvailable
			w.OwnerKind = workitem.OwnerCandidateSet
			w.Assignment = workitem.Assignment{Resolution: humanwork.Resolution{
				Outcome:    humanwork.OutcomeResolved,
				Candidates: candidateSet("principal:amy", "principal:bob"),
				Excluded:   []humanwork.Exclusion{{PrincipalID: "principal:bob", RuleID: "humanwork.conflict.sod"}},
			}}
		})
		if m := workitem.MembershipOf(item, "principal:bob", now); m != workitem.MembershipNone {
			t.Fatalf("recused principal membership = %v", m)
		}
	})
	t.Run("expired delegation is no member", func(t *testing.T) {
		item := queueItem(func(w *workitem.WorkItem) {
			w.Status = workitem.StatusAvailable
			w.OwnerKind = workitem.OwnerCandidateSet
			w.Assignment = workitem.Assignment{Resolution: humanwork.Resolution{
				Outcome: humanwork.OutcomeResolved,
				Candidates: []humanwork.Candidate{{
					PrincipalID:      "principal:delegate",
					Via:              humanwork.SourceDelegated,
					DelegatedFrom:    "principal:amy",
					DelegationExpiry: values.NewInstant(now.Add(-time.Hour)),
				}},
			}}
		})
		if m := workitem.MembershipOf(item, "principal:delegate", now); m != workitem.MembershipNone {
			t.Fatalf("stale delegate membership = %v", m)
		}
	})
}

func TestWorkItemVisibilityAndEvidenceFollowMembership(t *testing.T) {
	restricted := queueItem(func(w *workitem.WorkItem) {
		w.Visibility = workitem.VisibilityAssigneeOnly
	})
	if workitem.Visible(restricted, workitem.MembershipNone, true, true) {
		t.Fatal("assignee-only item visible to a non-member")
	}
	if !workitem.Visible(restricted, workitem.MembershipAssignee, false, false) {
		t.Fatal("assignee cannot see own item")
	}
	org := queueItem(func(w *workitem.WorkItem) { w.Visibility = workitem.VisibilityOrganizationScope })
	if !workitem.Visible(org, workitem.MembershipNone, true, false) || workitem.Visible(org, workitem.MembershipNone, false, false) {
		t.Fatal("organization-scope visibility not following the scope match")
	}
	gov := queueItem(func(w *workitem.WorkItem) { w.Visibility = workitem.VisibilityTenantGovernance })
	if !workitem.Visible(gov, workitem.MembershipNone, false, true) || workitem.Visible(gov, workitem.MembershipNone, true, false) {
		t.Fatal("governance visibility not following the governed flag")
	}
	if workitem.EvidenceVisible(workitem.MembershipCandidate) || workitem.EvidenceVisible(workitem.MembershipNone) {
		t.Fatal("restricted evidence visible to a candidate or non-member")
	}
	if !workitem.EvidenceVisible(workitem.MembershipClaimant) || !workitem.EvidenceVisible(workitem.MembershipAssignee) {
		t.Fatal("acting member cannot see evidence context")
	}
	if workitem.ContextVisible(workitem.MembershipNone) || !workitem.ContextVisible(workitem.MembershipCandidate) {
		t.Fatal("identity context visibility broken")
	}
}

// TestTodo_EP_WORK_001_Property sweeps every membership and every lifecycle
// status: the advertised action set may only contain actions the store's own
// transition map would accept from that standing, and only actionable
// memberships may ever see any action.
func TestTodo_EP_WORK_001_Property(t *testing.T) {
	statuses := []workitem.Status{
		workitem.StatusCreated, workitem.StatusRouted, workitem.StatusAssigned,
		workitem.StatusAvailable, workitem.StatusClaimed, workitem.StatusInProgress,
		workitem.StatusEscalated, workitem.StatusExpired, workitem.StatusCompleted,
		workitem.StatusReturned, workitem.StatusCancelled,
	}
	memberships := []workitem.Membership{
		workitem.MembershipNone, workitem.MembershipCandidate,
		workitem.MembershipAssignee, workitem.MembershipClaimant,
	}
	for _, status := range statuses {
		for _, m := range memberships {
			for _, kind := range []workitem.Kind{workitem.KindTask, workitem.KindApproval} {
				item := queueItem(func(w *workitem.WorkItem) {
					w.Status = status
					w.Kind = kind
					w.ProposalRef = "sha256:" + repeatHex("9")
				})
				actions := workitem.PermittedActions(item, m)
				seen := map[workitem.Action]bool{}
				for _, a := range actions {
					if seen[a] {
						t.Fatalf("status=%s membership=%d: duplicate action %q", status, m, a)
					}
					seen[a] = true
				}
				switch {
				case status.Terminal(), m == workitem.MembershipNone:
					if len(actions) != 0 {
						t.Fatalf("status=%s membership=%d: actions %v", status, m, actions)
					}
				case m == workitem.MembershipAssignee:
					for _, a := range actions {
						if a != workitem.ActionClaim {
							t.Fatalf("assignee advertised %q", a)
						}
					}
					if status != workitem.StatusAssigned && len(actions) != 0 {
						t.Fatalf("assignee at %s advertised %v", status, actions)
					}
				case m == workitem.MembershipCandidate:
					for _, a := range actions {
						if a != workitem.ActionClaim {
							t.Fatalf("candidate advertised %q", a)
						}
					}
					if status != workitem.StatusAvailable && len(actions) != 0 {
						t.Fatalf("candidate at %s advertised %v", status, actions)
					}
				case m == workitem.MembershipClaimant:
					for _, a := range actions {
						switch a {
						case workitem.ActionRelease:
							if status != workitem.StatusClaimed && status != workitem.StatusInProgress {
								t.Fatalf("release advertised at %s", status)
							}
						case workitem.ActionComplete:
							if status != workitem.StatusInProgress {
								t.Fatalf("complete advertised at %s", status)
							}
						case workitem.ActionDecideApproval:
							if status != workitem.StatusInProgress || kind != workitem.KindApproval {
								t.Fatalf("decide_approval advertised at %s kind %s", status, kind)
							}
						default:
							t.Fatalf("claimant advertised unknown action %q", a)
						}
					}
				}
			}
		}
	}
}

// TestTodo_EP_WORK_001_Queue is the store-level read: ListQueue returns
// exactly the live items the principal can act on, in deadline order, and
// never another tenant's row, a terminal row, an excluded candidate's row or
// a row whose only standing was an expired claim.
func TestTodo_EP_WORK_001_Queue(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work-queue")
	otherTenant := insertTenant(t, db, "work-queue-other")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	otherInstance := uuid.New()
	insertInstance(t, db, otherTenant, otherInstance)

	store := workitem.Store{}
	conn := appConn(t, db)

	newItem := func(mut func(*workitem.NewWorkItemInput)) workitem.WorkItem {
		in := newTaskInput(tenant, instance)
		if mut != nil {
			mut(&in)
		}
		item, err := workitem.NewWorkItem(in)
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		var stored workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			stored, err = store.Create(context.Background(), tx, item, meta("create"))
			return err
		})
		return stored
	}
	route := func(item workitem.WorkItem, res humanwork.Resolution) workitem.WorkItem {
		var routed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			routed, err = store.Route(context.Background(), tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: res}, meta("route"))
			return err
		})
		return routed
	}
	claim := func(item workitem.WorkItem, claimant string, claimAt, expiresAt time.Time) workitem.WorkItem {
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.Claim(context.Background(), tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: claimant, ClaimExpiresAt: expiresAt, Now: claimAt,
				Meta: meta("claim"),
			})
			return err
		})
		return claimed
	}

	// assigned to the caller.
	assigned := route(newItem(func(in *workitem.NewWorkItemInput) {
		in.DeadlineAt = fixedInstant.Add(24 * time.Hour)
	}), singleCandidateResolution("principal:amy"))
	// available to a candidate set containing the caller.
	available := route(newItem(func(in *workitem.NewWorkItemInput) {
		in.DeadlineAt = fixedInstant.Add(12 * time.Hour)
		in.Visibility = workitem.VisibilityCandidateSet
	}), multiCandidateResolution("principal:amy", "principal:bob"))
	// claimed by the caller (earliest deadline -> sorts first).
	claimed := claim(route(newItem(func(in *workitem.NewWorkItemInput) {
		in.DeadlineAt = fixedInstant.Add(6 * time.Hour)
	}), singleCandidateResolution("principal:amy")), "principal:amy", fixedInstant, fixedInstant.Add(4*time.Hour))
	// belongs to someone else.
	route(newItem(nil), singleCandidateResolution("principal:bob"))
	// expired claim on someone else's item: claimed an hour before `now`,
	// already lapsed when the queue is read.
	stranger := route(newItem(nil), singleCandidateResolution("principal:bob"))
	_ = claim(stranger, "principal:carol", fixedInstant.Add(-2*time.Hour), fixedInstant.Add(-time.Hour))
	// terminal.
	done := route(newItem(nil), singleCandidateResolution("principal:amy"))
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		claimedDone, err := store.Claim(context.Background(), tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: done.WorkItemID, ExpectedVersion: done.ItemVersion,
			ClaimantPrincipalID: "principal:amy", ClaimExpiresAt: fixedInstant.Add(4 * time.Hour),
			Now: fixedInstant, Meta: meta("claim"),
		})
		if err != nil {
			return err
		}
		started, err := store.Start(context.Background(), tx, tenant, done.WorkItemID, claimedDone.ItemVersion, fixedInstant, meta("start"))
		if err != nil {
			return err
		}
		_, err = store.Complete(context.Background(), tx, workitem.CompleteInput{
			TenantID: tenant, WorkItemID: done.WorkItemID, ExpectedVersion: started.ItemVersion,
			CompletedBy: "principal:amy", CompletedOutputDigest: "sha256:" + repeatHex("7"),
			Now: fixedInstant, Meta: meta("complete"),
		})
		return err
	})
	// the other tenant's queue is structurally out of reach.
	var otherCount int
	inTenantTx(t, conn, otherTenant, func(tx dbport.Tx) error {
		in := newTaskInput(otherTenant, otherInstance)
		item, err := workitem.NewWorkItem(in)
		if err != nil {
			return err
		}
		stored, err := store.Create(context.Background(), tx, item, meta("create"))
		if err != nil {
			return err
		}
		routed, err := store.Route(context.Background(), tx, otherTenant, stored.WorkItemID, stored.ItemVersion,
			workitem.Assignment{Resolution: singleCandidateResolution("principal:amy")}, meta("route"))
		if err != nil {
			return err
		}
		otherCount++
		_ = routed
		return nil
	})

	var amy []workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		amy, err = store.ListQueue(context.Background(), tx, tenant, "principal:amy", fixedInstant)
		return err
	})
	got := make([]uuid.UUID, len(amy))
	for i, item := range amy {
		got[i] = item.WorkItemID
	}
	want := []uuid.UUID{claimed.WorkItemID, available.WorkItemID, assigned.WorkItemID}
	if len(got) != len(want) {
		t.Fatalf("amy queue ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("amy queue order = %v, want %v", got, want)
		}
	}
	if otherCount != 1 {
		t.Fatal("other tenant seed missing")
	}

	var bob []workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		bob, err = store.ListQueue(context.Background(), tx, tenant, "principal:bob", fixedInstant)
		return err
	})
	if len(bob) != 3 {
		// bob is a candidate on `available`, assignee of his own item, and
		// the owner of `stranger` once carol's claim on it has lapsed.
		t.Fatalf("bob queue size = %d, want 3", len(bob))
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
