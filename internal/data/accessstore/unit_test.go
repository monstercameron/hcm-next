package accessstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/domains/access"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestAccessStore_SequenceAndOptionalUUIDValidation(t *testing.T) {
	tests := []struct {
		name string
		fn   func() error
		want error
	}{
		{name: "zero sequence", fn: func() error { _, err := sequence(values.RevisionToken{}); return err }, want: ErrInvalidRow},
		{name: "empty optional uuid", fn: func() error { _, err := optionalUUID("", "ref"); return err }, want: nil},
		{name: "malformed optional uuid", fn: func() error { _, err := optionalUUID("not-a-uuid", "ref"); return err }, want: ErrInvalidRow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want errors.Is(%v)", err, tt.want)
			}
		})
	}
}

func TestAccessStore_CoordinatesAndMetadataBranches(t *testing.T) {
	when := values.NewInstant(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	known, err := values.NewKnownAt(when)
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(when)
	if err != nil {
		t.Fatal(err)
	}
	prov := evidence.Provenance{Source: "test", EvidenceRef: "ref", RecordedAt: recorded}
	open, err := values.NewOpenInstantInterval(when)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := values.NewInstantInterval(when, values.NewInstant(when.Time().Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if start, end, gotKnown, gotRecorded, err := coordinates(open, known, prov); err != nil || end != nil || !start.Equal(when.Time()) || !gotKnown.Equal(when.Time()) || !gotRecorded.Equal(when.Time()) {
		t.Fatalf("open coordinates = %v, %v, %v, %v, %v", start, end, gotKnown, gotRecorded, err)
	}
	if _, end, _, _, err := coordinates(closed, known, prov); err != nil || end == nil {
		t.Fatalf("closed coordinates = end %v, err %v", end, err)
	}
	if _, _, _, _, err := coordinates(open, values.KnownAt{}, prov); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("missing known_at = %v", err)
	}
	if _, _, _, _, err := coordinates(open, known, evidence.Provenance{}); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("invalid provenance = %v", err)
	}
	if _, _, _, _, err := coordinates(values.EffectiveInterval{}, known, prov); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("invalid interval = %v", err)
	}
	if got := digest([]byte("payload")); len(got) != 64 {
		t.Fatalf("digest length = %d", len(got))
	}
	if knownAt, provenance, err := metadata(time.Time{}, "digest"); err != nil || knownAt.Canonical() == nil || provenance.EvidenceRef != "digest" {
		t.Fatalf("metadata = %+v, %+v, %v", knownAt, provenance, err)
	}
}

func TestAccessStore_RehydrateBranches(t *testing.T) {
	tenant := values.TenantId("tenant-a")
	worker := uuid.New()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	identity, err := rehydrateIdentity(tenant, "identity-1", "subject", "github", worker, 1, string(access.AuthorityNative), from, &to, from, string(access.LifecycleActive), "digest")
	if err != nil || identity.ID != "identity-1" || identity.WorkerRef.Id != worker.String() {
		t.Fatalf("identity = %+v, err %v", identity, err)
	}
	row := identityRow{id: identity.ID, rowID: uuid.New(), subject: identity.Subject, system: identity.System}
	account, err := rehydrateAccount(tenant, "link-1", row, "directory", "github", "acct", 1, from, nil, "digest")
	if err != nil || account.WorkforceIdentityID != identity.ID {
		t.Fatalf("account = %+v, err %v", account, err)
	}
	entitlement, err := rehydrateEntitlement(tenant, "ent-1", "github", "repo", "v1", string(access.RiskLow), "owner", 1, from, &to, "digest")
	if err != nil || entitlement.Code != "repo" {
		t.Fatalf("entitlement = %+v, err %v", entitlement, err)
	}
	ref := uuid.New()
	expected, err := rehydrateExpected(tenant, "expected-1", row, account.AccountID, entitlementRow{id: entitlement.ID, rowID: uuid.New(), system: entitlement.Application}, &ref, nil, nil, 1, from, nil, "digest")
	if err != nil || expected.EmploymentRef != ref.String() || expected.PositionRef != "" {
		t.Fatalf("expected = %+v, err %v", expected, err)
	}
	if uuidText(nil) != "" || uuidText(&ref) != ref.String() {
		t.Fatal("uuidText did not preserve optional reference semantics")
	}
	badTo := from.Add(-time.Hour)
	for name, call := range map[string]func() error{
		"identity interval": func() error {
			_, err := rehydrateIdentity(tenant, "id", "s", "sys", worker, 1, "NATIVE", from, &badTo, from, "ACTIVE", "d")
			return err
		},
		"account interval": func() error {
			_, err := rehydrateAccount(tenant, "id", row, "src", "sys", "acct", 1, from, &badTo, "d")
			return err
		},
		"entitlement interval": func() error {
			_, err := rehydrateEntitlement(tenant, "id", "sys", "code", "v", "LOW", "owner", 1, from, &badTo, "d")
			return err
		},
		"expected interval": func() error {
			_, err := rehydrateExpected(tenant, "id", row, "acct", entitlementRow{id: "ent", system: "sys"}, nil, nil, nil, 1, from, &badTo, "d")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if call() == nil {
				t.Fatal("invalid revision was accepted")
			}
		})
	}
}

func TestAccessStore_RecordTenantAndDispatchRejectUnsupportedRecords(t *testing.T) {
	var nilIdentity *access.WorkforceIdentity
	if _, err := recordTenant(nilIdentity); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("nil identity tenant = %v", err)
	}
	if _, err := recordTenant(nil); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("nil record tenant = %v", err)
	}
	store := New(nil)
	if _, err := store.appendRecord(context.Background(), nil, uuid.New(), nilIdentity); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("nil identity dispatch = %v", err)
	}
	if _, err := store.appendRecord(context.Background(), nil, uuid.New(), nil); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("nil dispatch = %v", err)
	}
	if err := store.withTenant(context.Background(), "tenant", func(dbport.Tx, uuid.UUID) error { return nil }); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("nil database = %v", err)
	}
	if err := (&Store{}).Add(context.Background(), nilIdentity); !errors.Is(err, ErrInvalidRow) {
		t.Fatalf("Add nil pointer = %v", err)
	}
}
