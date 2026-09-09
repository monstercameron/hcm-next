package intentcontrol_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// The reads and the parent-row write WF-RUN-027's durable approval facts are
// built on: proposal_revision materialization, the whole-row decision read for
// one exact revision, and the SUPERSEDES lookup.

// referenceRevision is a well-formed [intentcontrol.Revision] for intent.
func referenceRevision(tenant, intentID uuid.UUID, revision uint64, label string) intentcontrol.Revision {
	return intentcontrol.Revision{
		TenantID:       tenant,
		IntentID:       intentID,
		Revision:       revision,
		ProposalDigest: digestOf("proposal:" + label),
		MaterialDigest: digestOf("material:" + label),
		SchemaRef:      "hcmnext.intents.v1.Proposal",
		Payload:        []byte(`{"proposal":"` + label + `"}`),
		ProducedBy:     "test-fixture",
		ProducedAt:     fixedInstant,
	}
}

// factsDecision is a well-formed APPROVED decision bound to digest.
func factsDecision(tenant, intentID uuid.UUID, revision uint64, requirement, by, digest string) intentcontrol.Decision {
	return intentcontrol.Decision{
		TenantID:         tenant,
		DecisionID:       uuid.New(),
		IntentID:         intentID,
		Revision:         revision,
		RequirementID:    requirement,
		Kind:             intentcontrol.DecisionHumanApproval,
		Outcome:          intentcontrol.OutcomeApproved,
		ProposalDigest:   digest,
		ControlDigest:    digestOf("control:" + requirement),
		MaterialityClass: intentcontrol.Material,
		DecidedBy:        by,
		AuthorityRef:     "authority:test",
		Reason:           "because the test says so",
		DecidedAt:        fixedInstant,
	}
}

func TestRevisionStoreMaterializesOnceAndReportsTheSecondCall(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "facts-materialize")
	intentID := insertIntent(t, db, tenant, "facts-materialize-1")
	rev := referenceRevision(tenant, intentID, 1, "materialize")

	var first, second bool
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		first, err = (intentcontrol.RevisionStore{}).Materialize(context.Background(), tx, rev)
		if err != nil {
			return err
		}
		second, err = (intentcontrol.RevisionStore{}).Materialize(context.Background(), tx, rev)
		return err
	})
	if !first {
		t.Error("the first Materialize must report that it created the row")
	}
	if second {
		t.Error("the second Materialize must report that the row was already stored")
	}
	if n := countRows(t, db, "proposal_revision", tenant); n != 1 {
		t.Fatalf("proposal_revision rows = %d, want 1", n)
	}

	var stored string
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		stored, err = (intentcontrol.RevisionStore{}).MaterialDigestOf(
			context.Background(), tx, tenant, intentID, 1)
		return err
	})
	if stored != rev.MaterialDigest {
		t.Fatalf("MaterialDigestOf = %q, want %q", stored, rev.MaterialDigest)
	}
}

func TestRevisionStoreRefusesAnIncompleteRow(t *testing.T) {
	for name, mutate := range map[string]func(*intentcontrol.Revision){
		"no tenant":    func(r *intentcontrol.Revision) { r.TenantID = uuid.Nil },
		"no intent":    func(r *intentcontrol.Revision) { r.IntentID = uuid.Nil },
		"revision 0":   func(r *intentcontrol.Revision) { r.Revision = 0 },
		"bad proposal": func(r *intentcontrol.Revision) { r.ProposalDigest = "not-a-digest" },
		"bad material": func(r *intentcontrol.Revision) { r.MaterialDigest = "" },
		"no schema":    func(r *intentcontrol.Revision) { r.SchemaRef = "" },
		"no payload":   func(r *intentcontrol.Revision) { r.Payload = nil },
		"no producer":  func(r *intentcontrol.Revision) { r.ProducedBy = "" },
		"no instant":   func(r *intentcontrol.Revision) { r.ProducedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			rev := referenceRevision(uuid.New(), uuid.New(), 1, "invalid")
			mutate(&rev)
			if err := rev.Validate(); !errors.Is(err, intentcontrol.ErrInvalidRow) {
				t.Fatalf("Validate() = %v, want ErrInvalidRow", err)
			}
		})
	}
}

func TestRevisionStoreMaterialDigestOfAnUnknownRevisionIsNotFound(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "facts-unknown-revision")
	intentID := insertIntent(t, db, tenant, "facts-unknown-revision-1")

	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, readErr := (intentcontrol.RevisionStore{}).MaterialDigestOf(
			context.Background(), tx, tenant, intentID, 7)
		return readErr
	})
	if !errors.Is(err, intentcontrol.ErrNotFound) {
		t.Fatalf("MaterialDigestOf(unknown) = %v, want ErrNotFound", err)
	}
}

func TestDecisionStoreForRevisionReturnsWholeRowsForThatRevisionOnly(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "facts-decisions")
	intentID := insertIntent(t, db, tenant, "facts-decisions-1")
	rev1 := referenceRevision(tenant, intentID, 1, "decisions-1")
	rev2 := referenceRevision(tenant, intentID, 2, "decisions-2")

	wanted := factsDecision(tenant, intentID, 1, "req.one/v1", "principal:a", rev1.ProposalDigest)
	other := factsDecision(tenant, intentID, 1, "req.two/v1", "principal:b", rev1.ProposalDigest)
	elsewhere := factsDecision(tenant, intentID, 2, "req.one/v1", "principal:a", rev2.ProposalDigest)

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		ctx := context.Background()
		for _, r := range []intentcontrol.Revision{rev1, rev2} {
			if _, err := (intentcontrol.RevisionStore{}).Materialize(ctx, tx, r); err != nil {
				return err
			}
		}
		for _, d := range []intentcontrol.Decision{wanted, other, elsewhere} {
			if err := (intentcontrol.DecisionStore{}).Record(ctx, tx, d); err != nil {
				return err
			}
		}
		return nil
	})

	var got []intentcontrol.Decision
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		got, err = (intentcontrol.DecisionStore{}).ForRevision(context.Background(), tx, tenant, intentID, 1)
		return err
	})
	if len(got) != 2 {
		t.Fatalf("ForRevision(revision 1) returned %d rows, want 2", len(got))
	}
	byID := map[uuid.UUID]intentcontrol.Decision{}
	for _, d := range got {
		byID[d.DecisionID] = d
		if d.Revision != 1 {
			t.Errorf("decision %s carries revision %d, want 1", d.DecisionID, d.Revision)
		}
		if d.ProposalDigest != rev1.ProposalDigest {
			t.Errorf("decision %s is bound to %q, want %q", d.DecisionID, d.ProposalDigest, rev1.ProposalDigest)
		}
		if d.Outcome != intentcontrol.OutcomeApproved || d.Kind != intentcontrol.DecisionHumanApproval {
			t.Errorf("decision %s = %s/%s, want HUMAN_APPROVAL/APPROVED", d.DecisionID, d.Kind, d.Outcome)
		}
	}
	if _, ok := byID[elsewhere.DecisionID]; ok {
		t.Error("ForRevision(revision 1) must not return revision 2's decision")
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].DecisionID.String() > got[i].DecisionID.String() {
			t.Fatalf("ForRevision must order by decision id, got %v then %v",
				got[i-1].DecisionID, got[i].DecisionID)
		}
	}
}

func TestDecisionStoreForRevisionIsEmptyWithNoDecisions(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "facts-no-decisions")
	intentID := insertIntent(t, db, tenant, "facts-no-decisions-1")

	var got []intentcontrol.Decision
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		got, err = (intentcontrol.DecisionStore{}).ForRevision(context.Background(), tx, tenant, intentID, 1)
		return err
	})
	if len(got) != 0 {
		t.Fatalf("ForRevision with nothing recorded returned %d rows, want 0", len(got))
	}
}

func TestRelationshipStoreSupersededByReadsTheEdgeChildToParent(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "facts-supersession")
	superseded := insertIntent(t, db, tenant, "facts-supersession-old")
	superseder := insertIntent(t, db, tenant, "facts-supersession-new")
	untouched := insertIntent(t, db, tenant, "facts-supersession-other")

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return (intentcontrol.RelationshipStore{}).Link(context.Background(), tx, intentcontrol.Relationship{
			TenantID:            tenant,
			RelationshipID:      uuid.New(),
			Type:                intentcontrol.RelationSupersedes,
			Parent:              superseder,
			Child:               superseded,
			Ordinal:             1,
			MaterialInputDigest: digestOf("supersession"),
			EstablishedAt:       fixedInstant,
		})
	})

	var by uuid.UUID
	var found bool
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		by, found, err = (intentcontrol.RelationshipStore{}).SupersededBy(
			context.Background(), tx, tenant, superseded)
		return err
	})
	if !found || by != superseder {
		t.Fatalf("SupersededBy(superseded) = %v/%t, want %v/true", by, found, superseder)
	}

	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		by, found, err = (intentcontrol.RelationshipStore{}).SupersededBy(
			context.Background(), tx, tenant, untouched)
		return err
	})
	if found || by != uuid.Nil {
		t.Fatalf("SupersededBy(untouched) = %v/%t, want nil/false", by, found)
	}

	// The superseder itself is not superseded: the edge is read child to
	// parent, never both ways.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		by, found, err = (intentcontrol.RelationshipStore{}).SupersededBy(
			context.Background(), tx, tenant, superseder)
		return err
	})
	if found {
		t.Fatalf("SupersededBy(superseder) = %v/true, want not found", by)
	}
}
