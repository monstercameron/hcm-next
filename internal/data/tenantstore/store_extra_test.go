package tenantstore

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant/govauth"
)

func TestReadyAndErrorConstructors_RejectInvalidScope(t *testing.T) {
	store := Store{}
	for _, ref := range []string{"", "not-a-uuid", uuid.Nil.String()} {
		if _, err := store.ready(ref); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ready(%q) = %v, want ErrInvalid", ref, err)
		}
	}
	if _, err := store.ready(uuid.NewString()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil executor ready = %v, want ErrInvalid", err)
	}
	for _, tc := range []struct {
		name string
		got  error
		want error
	}{
		{"invalid", invalid("x"), ErrInvalid},
		{"duplicate", duplicate("x"), ErrDuplicate},
		{"not found", notFound("x"), ErrNotFound},
		{"stale", stale("x"), ErrStaleCAS},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.got, tc.want) || CodeOf(tc.got) != CodeOf(tc.want) {
				t.Fatalf("error=%v code=%s; want %v code=%s", tc.got, CodeOf(tc.got), tc.want, CodeOf(tc.want))
			}
		})
	}
	if got := jsonString("quoted\nvalue"); got != `"quoted\nvalue"` {
		t.Fatalf("jsonString() = %q", got)
	}
}

func TestSealProfile_RejectsTenantAndDateTampering(t *testing.T) {
	tenantID := uuid.NewString()
	base := govauth.GovernmentAuthorizationProfile{SchemaVersion: 1, Revision: 1, TenantID: tenantID, IntegrationID: "integration", ReviewDate: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), AssessorStatus: govauth.AssessorPending}
	if _, err := sealProfile(uuid.NewString(), base); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tenant mismatch = %v, want ErrInvalid", err)
	}
	nonMidnight := base
	nonMidnight.ReviewDate = nonMidnight.ReviewDate.Add(time.Hour)
	if _, err := sealProfile(tenantID, nonMidnight); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-midnight date = %v, want ErrInvalid", err)
	}
	invalidProfile := base
	invalidProfile.SchemaVersion = 99
	if _, err := sealProfile(tenantID, invalidProfile); err == nil {
		t.Fatal("sealProfile accepted an unsupported schema")
	}
}

func TestDecodeProfile_RejectsMalformedStoredValues(t *testing.T) {
	tenantID := uuid.New()
	date := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	if _, err := decodeProfile(1, 1, "integration", "{", `{}`, `{}`, `{}`, govauth.AssessorPending, date, `{}`, `{}`, `{}`, "digest", tenantID); err == nil {
		t.Fatal("decodeProfile accepted malformed programs JSON")
	}
	if _, err := decodeProfile(1, 1, "integration", `{}`, `{}`, `{}`, `{}`, govauth.AssessorPending, time.Time{}, `{}`, `{}`, `{}`, "digest", tenantID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("decodeProfile zero date = %v, want ErrInvalid", err)
	}
}

func TestStore_ZeroValueMethodsFailClosed(t *testing.T) {
	ctx := t.Context()
	var store Store
	if err := store.SavePlacement(ctx, uuid.NewString(), tenant.Placement{}, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SavePlacement zero store = %v, want ErrInvalid", err)
	}
	if _, err := store.LoadPlacement(ctx, uuid.NewString()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("LoadPlacement zero store = %v, want ErrInvalid", err)
	}
	if _, err := store.LatestGovernmentAuthorizationProfile(ctx, uuid.NewString(), " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Latest profile invalid input = %v, want ErrInvalid", err)
	}
}
