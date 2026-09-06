package tenant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/domains/tenant/govauth"
)

func TestTodo_PERSIST_TENANT_001_DomainPort(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	store := NewMemoryStore()
	key := [32]byte{1, 2, 3}
	placement, err := Sign(Placement{
		Tenant: "tenant-key", Cell: "cell-a", Region: "us-east",
		ResidencyProfile: "US", IsolationTier: "dedicated", Epoch: 1,
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacement(ctx, id.String(), placement, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacement(ctx, id.String(), placement, 0); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate placement error = %v, want ErrDuplicate", err)
	}

	event := ProvisioningEvent{
		Kind: EventPlaneVerified, Tenant: "tenant-key", Plane: PlanePlacement,
		VerifierPrincipal: "verifier", At: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
	if err := store.AppendProvisioningEvent(ctx, id.String(), event, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendProvisioningEvent(ctx, id.String(), event, 1); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate event error = %v, want ErrDuplicate", err)
	}
	events, err := store.ListProvisioningEvents(ctx, id.String())
	if err != nil || len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}

	profile, err := govauth.NewProfile(govauth.GovernmentAuthorizationProfile{
		SchemaVersion: 1, Revision: 1, TenantID: id.String(), IntegrationID: "integration-a",
		ApplicablePrograms: []govauth.Program{govauth.ProgramGovRAMP}, SystemBoundary: "platform",
		AssessorStatus: govauth.AssessorPending, ReviewDate: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, id.String(), profile); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LatestGovernmentAuthorizationProfile(ctx, id.String(), "integration-a")
	if err != nil || loaded.Revision != 1 || loaded.RevisionDigest != profile.RevisionDigest {
		t.Fatalf("loaded profile = %+v, err = %v", loaded, err)
	}
}
