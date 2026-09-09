package tenant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant/govauth"
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

func repositoryProfile(revision uint64) govauth.GovernmentAuthorizationProfile {
	return govauth.GovernmentAuthorizationProfile{
		SchemaVersion:      1,
		Revision:           revision,
		TenantID:           "tenant-key",
		IntegrationID:      "integration-a",
		ApplicablePrograms: []govauth.Program{govauth.ProgramGovRAMP},
		SystemBoundary:     "platform",
		AssessorStatus:     govauth.AssessorPending,
		ReviewDate:         time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
}

var repositoryPlacementKey = [32]byte{1, 2, 3, 4, 5}

func repositoryPlacement() Placement {
	return Placement{Tenant: "tenant-a", Cell: "cell-east-1", Region: "us-east", ResidencyProfile: "us-only", IsolationTier: "dedicated", Epoch: 7}
}

func TestStoreError_ClassificationAndWrapping(t *testing.T) {
	var nilError *StoreError
	if got := nilError.Error(); got != "tenant: persistence error" {
		t.Fatalf("nil StoreError.Error = %q", got)
	}
	duplicate := storeError(StoreCodeDuplicate, "already exists")
	if duplicate.Error() != "tenant: DUPLICATE: already exists" || !errors.Is(duplicate, ErrDuplicate) || errors.Is(duplicate, ErrNotFound) {
		t.Fatalf("duplicate classification/message failed: %v", duplicate)
	}
	if got := CodeOf(fmt.Errorf("wrapped: %w", duplicate)); got != StoreCodeDuplicate {
		t.Fatalf("CodeOf wrapped duplicate = %q, want DUPLICATE", got)
	}
	if got := CodeOf(errors.New("ordinary failure")); got != "" {
		t.Fatalf("CodeOf ordinary error = %q, want empty", got)
	}
	if got := (&StoreError{Code: StoreCodeNotFound}).Error(); got != "tenant: NOT_FOUND" {
		t.Fatalf("empty-detail error = %q", got)
	}
	if (&StoreError{Code: StoreCodeDuplicate}).Is(nil) || (&StoreError{Code: StoreCodeDuplicate}).Is(errors.New("x")) {
		t.Fatal("StoreError.Is matched an unrelated target")
	}
}

func TestMemoryStore_PlacementValidationAndCAS(t *testing.T) {
	ctx := context.Background()
	var nilStore *MemoryStore
	if err := nilStore.SavePlacement(ctx, "tenant", repositoryPlacement(), 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil SavePlacement = %v, want ErrInvalid", err)
	}
	if _, err := nilStore.LoadPlacement(ctx, "tenant"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil LoadPlacement = %v, want ErrInvalid", err)
	}
	store := NewMemoryStore()
	if err := store.SavePlacement(ctx, "", repositoryPlacement(), 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank tenant SavePlacement = %v, want ErrInvalid", err)
	}
	invalid := repositoryPlacement()
	invalid.Tenant = ""
	if err := store.SavePlacement(ctx, "tenant", invalid, 0); !errors.Is(err, ErrInvalidPlacement) {
		t.Fatalf("invalid placement = %v, want ErrInvalidPlacement", err)
	}
	unsigned := repositoryPlacement()
	if err := store.SavePlacement(ctx, "tenant", unsigned, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsigned placement = %v, want ErrInvalid", err)
	}
	signed, err := Sign(repositoryPlacement(), repositoryPlacementKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacement(ctx, "tenant", signed, 1); !errors.Is(err, ErrStaleCAS) {
		t.Fatalf("initial nonzero CAS = %v, want ErrStaleCAS", err)
	}
	if err := store.SavePlacement(ctx, "tenant", signed, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacement(ctx, "tenant", signed, 0); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate placement = %v, want ErrDuplicate", err)
	}
	if _, err := store.LoadPlacement(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing placement = %v, want ErrNotFound", err)
	}
	if err := store.SavePlacement(ctx, "tenant", signed, 99); !errors.Is(err, ErrStaleCAS) {
		t.Fatalf("wrong expected epoch = %v, want ErrStaleCAS", err)
	}
	stale := signed
	stale.Epoch = signed.Epoch
	if err := store.SavePlacement(ctx, "tenant", stale, signed.Epoch); !errors.Is(err, ErrStaleCAS) {
		t.Fatalf("non-increasing epoch = %v, want ErrStaleCAS", err)
	}
	updated := signed
	updated.Epoch++
	if err := store.SavePlacement(ctx, "tenant", updated, signed.Epoch); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadPlacement(ctx, "tenant")
	if err != nil || loaded.Epoch != updated.Epoch {
		t.Fatalf("loaded placement = %+v, err=%v", loaded, err)
	}
}

func TestMemoryStore_ProvisioningEventsValidateAndCopy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	event := ProvisioningEvent{Kind: EventPlaneVerified, Tenant: "tenant-key", Plane: PlanePlacement, VerifierPrincipal: "verifier", At: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}
	for name, tc := range map[string]struct {
		tenantID string
		event    ProvisioningEvent
		sequence uint64
	}{
		"blank storage tenant": {tenantID: "", event: event, sequence: 1},
		"blank event tenant":   {tenantID: "tenant", event: ProvisioningEvent{}, sequence: 1},
		"zero sequence":        {tenantID: "tenant", event: event, sequence: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if err := store.AppendProvisioningEvent(ctx, tc.tenantID, tc.event, tc.sequence); !errors.Is(err, ErrInvalid) {
				t.Fatalf("AppendProvisioningEvent = %v, want ErrInvalid", err)
			}
		})
	}
	if err := store.AppendProvisioningEvent(ctx, "tenant", event, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendProvisioningEvent(ctx, "tenant", event, 1); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate event = %v, want ErrDuplicate", err)
	}
	events, err := store.ListProvisioningEvents(ctx, "tenant")
	if err != nil || len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("events = %#v, err=%v", events, err)
	}
	events[0].Event.Tenant = "tampered"
	if got, err := store.ListProvisioningEvents(ctx, "tenant"); err != nil || got[0].Event.Tenant != event.Tenant {
		t.Fatalf("event storage was aliased: %#v, err=%v", got, err)
	}
	if _, err := store.ListProvisioningEvents(ctx, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank tenant ListProvisioningEvents = %v, want ErrInvalid", err)
	}
}

func TestMemoryStore_ProfilesNormalizeAliasesAndEnforceTenantAndRevisionRules(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	profile := repositoryProfile(1)
	if err := store.SaveGovernmentAuthorizationProfile(ctx, "other-tenant", profile); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong profile tenant = %v, want ErrInvalid", err)
	}
	badDate := repositoryProfile(2)
	badDate.ReviewDate = badDate.ReviewDate.Add(time.Hour)
	if err := store.SaveGovernmentAuthorizationProfile(ctx, "tenant-key", badDate); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-midnight review date = %v, want ErrInvalid", err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, "tenant-key", profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, "tenant-key", profile); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("duplicate profile = %v, want ErrDuplicate", err)
	}
	newer := repositoryProfile(2)
	if err := store.SaveGovernmentAuthorizationProfile(ctx, "tenant-key", newer); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadGovernmentAuthorizationProfile(ctx, "tenant-key", "integration-a", 1)
	if err != nil || loaded.Revision != 1 || loaded.RevisionDigest == "" {
		t.Fatalf("loaded profile = %+v, err=%v", loaded, err)
	}
	latest, err := store.LatestGovernmentAuthorizationProfile(ctx, "tenant-key", "integration-a")
	if err != nil || latest.Revision != 2 {
		t.Fatalf("latest profile = %+v, err=%v", latest, err)
	}
	for name, fn := range map[string]func() error{
		"load missing": func() error {
			_, err := store.LoadGovernmentAuthorizationProfile(ctx, "tenant-key", "integration-a", 9)
			return err
		},
		"latest missing": func() error {
			_, err := store.LatestGovernmentAuthorizationProfile(ctx, "tenant-key", "missing")
			return err
		},
		"load invalid": func() error {
			_, err := store.LoadGovernmentAuthorizationProfile(ctx, "", "integration-a", 1)
			return err
		},
		"latest invalid": func() error {
			_, err := store.LatestGovernmentAuthorizationProfile(ctx, "", "integration-a")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := fn()
			want := ErrNotFound
			if strings.Contains(name, "invalid") {
				want = ErrInvalid
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
		})
	}

	alias := govauth.GovernmentAuthorizationProfile{
		SchemaVersion: 1, Version: 3, TenantID: "tenant-key", TenantRef: "legacy-tenant", IntegrationRef: "integration-alias",
		Programs: []govauth.Program{govauth.ProgramGovRAMP}, SystemBoundary: "platform",
		AssessorStatus: govauth.AssessorPending, ReviewDate: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, "tenant-key", alias); err != nil {
		t.Fatal(err)
	}
	aliased, err := store.LoadGovernmentAuthorizationProfile(ctx, "tenant-key", "integration-alias", 3)
	if err != nil || aliased.TenantID != "tenant-key" || aliased.IntegrationID != "integration-alias" || aliased.Revision != 3 || len(aliased.ApplicablePrograms) != 1 {
		t.Fatalf("normalized alias profile = %+v, err=%v", aliased, err)
	}
}

func TestRepositoryValidSignatureRejectsMalformedValues(t *testing.T) {
	for _, signature := range []string{"", "zz", "00"} {
		if err := validSignature(signature); !errors.Is(err, ErrInvalid) {
			t.Fatalf("validSignature(%q) = %v, want ErrInvalid", signature, err)
		}
	}
	if err := validSignature(strings.Repeat("a", 64)); err != nil {
		t.Fatalf("validSignature valid hex = %v", err)
	}
}
