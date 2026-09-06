package locationstore_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/locationstore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/domains/location"
	"github.com/monstercameron/hcm-next/internal/governance/decision"
)

func TestTodo_PERSIST_LOCATION_003(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "location-correction")
	store := locationstore.Store{}
	first := testWorkLocation(t, "location:correction")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, first)
		return err
	})

	request := location.WorkLocationCorrectionRequest{
		Current: first,
		Replacement: location.WorkLocationRevision{
			Address: first.Address, SourceAuthority: "correction/v1", Confidence: location.ConfidenceAuthoritative,
			Effective: testInterval(t), KnownAt: testKnownAt(t),
		},
		Authority:    decision.Decision{State: decision.Allow, ProposalRevisionDigest: "sha256:proposal", Digest: "sha256:decision"},
		Dependencies: []location.DependentPeriod{{Kind: location.ImpactTax, SubjectRef: "tax:worker-1", Period: testInterval(t), RuleRelease: "tax-2026.1"}},
	}
	var plan location.CorrectionPlan
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		plan, err = store.CorrectWorkLocation(context.Background(), tx, tenant, request)
		return err
	})
	if plan.Successor.Revision != 2 || plan.Successor.ParentDigest != first.CanonicalDigest {
		t.Fatalf("stored correction plan = %+v", plan)
	}
	var loaded location.WorkLocationRevision
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = store.LoadWorkLocation(context.Background(), tx, tenant, first.LocationID, 2)
		return err
	})
	if loaded.CanonicalDigest != plan.Successor.CanonicalDigest || loaded.ParentDigest != first.CanonicalDigest {
		t.Fatalf("durable successor = %+v", loaded)
	}
}
