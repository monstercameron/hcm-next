package configregistry_test

import (
	"testing"
	"time"

	dataconfigregistry "github.com/monstercameron/hcm-next/internal/data/configregistry"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	platformconfig "github.com/monstercameron/hcm-next/internal/platform/configregistry"
)

func TestStoreGetLatestActivationUnknownGroupIsNotFoundNotError(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "activation-unknown")
	store := dataconfigregistry.New(appConn(t, db))

	_, found, err := store.GetLatestActivation(platformconfig.Scope{TenantID: tenantID.String()}, platformconfig.KindAgent, "never-activated")
	if err != nil {
		t.Fatalf("GetLatestActivation: %v", err)
	}
	if found {
		t.Fatal("GetLatestActivation reported found for a group with no activations")
	}
}

func TestStoreActivationSequenceIsGapFreeAndOrdered(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "activation-sequence")
	store := dataconfigregistry.New(appConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String()}

	var revisions []platformconfig.ConfigurationObject
	for rev := uint32(1); rev <= 3; rev++ {
		obj, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
			Kind: platformconfig.KindConnector, ID: "payroll-feed", Revision: rev,
			Body: []byte{byte('a' + rev)}, SchemaRef: "hcmnext.connector/v1",
			Scope: scope, PublisherPrincipal: "admin", PublishedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("Publish revision %d: %v", rev, err)
		}
		revisions = append(revisions, obj)
	}

	for i, obj := range revisions {
		if _, err := platformconfig.Activate(store, obj.Ref(), platformconfig.ActivationEvidence{
			ActivatedBy: "admin", ActivatedAt: time.Unix(int64(i+1), 0),
		}); err != nil {
			t.Fatalf("Activate revision %d: %v", obj.Revision, err)
		}
	}

	latest, found, err := store.GetLatestActivation(scope, platformconfig.KindConnector, "payroll-feed")
	if err != nil || !found || latest.Revision != 3 {
		t.Fatalf("GetLatestActivation = %+v, found=%v, err=%v, want revision 3", latest, found, err)
	}

	history, err := store.ListActivations(scope, platformconfig.KindConnector, "payroll-feed")
	if err != nil {
		t.Fatalf("ListActivations: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("ListActivations returned %d rows, want 3 (every superseded activation retained)", len(history))
	}
	for i, rec := range history {
		if rec.Revision != uint32(i+1) {
			t.Fatalf("ListActivations[%d].Revision = %d, want %d", i, rec.Revision, i+1)
		}
	}
}

func TestStoreActivateRefusesAnUnpublishedRevisionThroughPostgres(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "activation-refuse")
	store := dataconfigregistry.New(appConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String()}

	_, err := platformconfig.Activate(store, platformconfig.ObjectRef{
		Scope: scope, Kind: platformconfig.KindMapping, ID: "never-published", Revision: 1,
	}, platformconfig.ActivationEvidence{ActivatedBy: "admin", ActivatedAt: time.Now()})
	if platformconfig.CodeOf(err) != platformconfig.CodeUnknownRevision {
		t.Fatalf("CodeOf(err) = %q, want %q", platformconfig.CodeOf(err), platformconfig.CodeUnknownRevision)
	}
}
