package taxprofilestore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/taxprofilestore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/taxprofile"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const (
	digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	return pgtest.New(t)
}

func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, "tenant-"+id.String(), "Tenant "+id.String())
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
	}
	return conn
}

func taxInterval(t *testing.T, at string) values.EffectiveInterval {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(parsed))
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func taxInstant(t *testing.T, at string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, at)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func fixture(t *testing.T) (uuid.UUID, taxprofile.WorkerTaxProfileRevision, taxprofile.TaxRegistrationRevision, taxprofile.WithholdingElectionRevision, taxprofile.TaxExemptionRevision) {
	t.Helper()
	worker := uuid.New()
	registration, err := taxprofile.NewTaxRegistrationRevision(taxprofile.TaxRegistrationRevision{
		RegistrationIDRef: "registration-ref-1", Jurisdiction: "US-CA", AuthorityRef: "authority-ref-1", Revision: 1,
		Effective: taxInterval(t, "2026-01-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-01-01T01:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	election, err := taxprofile.NewWithholdingElectionRevision(taxprofile.WithholdingElectionRevision{
		ElectionID: "election-1", WorkerRef: worker.String(), Jurisdiction: "US-CA", Kind: taxprofile.ElectionStandardWithholding,
		FormRevisionRef: "form:w4:v2026", EvidenceRef: "evidence:election-1", Effective: taxInterval(t, "2026-01-01T00:00:00Z"),
		KnownAt: taxInstant(t, "2026-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	exemption, err := taxprofile.NewTaxExemptionRevision(taxprofile.TaxExemptionRevision{
		ExemptionID: "exemption-1", WorkerRef: worker.String(), Jurisdiction: "US-CA", Kind: taxprofile.ExemptionState,
		EvidenceRefs: []string{"evidence:exemption-1"}, ExpiresAt: taxInstant(t, "2026-12-31T00:00:00Z"),
		Effective: taxInterval(t, "2026-01-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := taxprofile.NewWorkerTaxProfileRevision(taxprofile.WorkerTaxProfileRevision{
		WorkerRef: worker.String(), Revision: 1, ResidenceJurisdictions: []string{"US-CA"}, WorkJurisdictions: []string{"US-CA"},
		FilingStatus: taxprofile.FilingSingle, Classification: taxprofile.ClassificationResident,
		ClassificationEvidenceRef: "evidence:classification-1", Registrations: []taxprofile.TaxRegistrationRevision{registration},
		Elections: []taxprofile.WithholdingElectionRevision{election}, Exemptions: []taxprofile.TaxExemptionRevision{exemption},
		Effective: taxInterval(t, "2026-01-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-01-03T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return worker, profile, registration, election, exemption
}

func TestTodo_PERSIST_TAXPROFILE_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	_, profile, registration, election, exemption := fixture(t)
	store := taxprofilestore.New(appConn(t, db))
	if err := store.SaveProfile(context.Background(), tenant.String(), profile); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	got, err := store.LoadProfile(context.Background(), tenant.String(), profile.WorkerRef, 1)
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if got.CanonicalDigest != profile.CanonicalDigest || len(got.Registrations) != 1 || len(got.Elections) != 1 || len(got.Exemptions) != 1 {
		t.Fatalf("profile round trip = %+v", got)
	}
	for _, check := range []func() error{
		func() error {
			_, err := store.LoadRegistration(context.Background(), tenant.String(), registration.RegistrationIDRef, 1)
			return err
		},
		func() error {
			_, err := store.LoadElection(context.Background(), tenant.String(), election.ElectionID)
			return err
		},
		func() error {
			_, err := store.LoadExemption(context.Background(), tenant.String(), exemption.ExemptionID)
			return err
		},
	} {
		if err := check(); err != nil {
			t.Fatalf("child load: %v", err)
		}
	}
}

func TestTodo_PERSIST_TAXPROFILE_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	_, profile, _, _, _ := fixture(t)
	store := taxprofilestore.New(appConn(t, db))
	if err := store.SaveProfile(context.Background(), tenant.String(), profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfile(context.Background(), tenant.String(), profile); !errors.Is(err, taxprofile.ErrStoreDuplicate) {
		t.Fatalf("duplicate profile = %v, want ErrStoreDuplicate", err)
	}
	next := profile
	next.Revision = 3
	next.ParentRevision = 2
	next.ParentDigest = profile.CanonicalDigest
	next.CanonicalDigest = ""
	if err := store.SaveProfile(context.Background(), tenant.String(), next); !errors.Is(err, taxprofile.ErrStoreStaleCAS) {
		t.Fatalf("stale profile = %v, want ErrStoreStaleCAS", err)
	}
}

func TestTodo_PERSIST_TAXPROFILE_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	_, profile, _, _, _ := fixture(t)
	store := taxprofilestore.New(appConn(t, db))
	if err := store.SaveProfile(context.Background(), tenant.String(), profile); err != nil {
		t.Fatal(err)
	}
	next, err := profile.Fork(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfile(context.Background(), tenant.String(), next); err != nil {
		t.Fatalf("SaveProfile revision 2: %v", err)
	}
	got, err := store.LoadProfile(context.Background(), tenant.String(), profile.WorkerRef, 2)
	if err != nil || got.ParentRevision != 1 || got.ParentDigest != profile.CanonicalDigest {
		t.Fatalf("successor = %+v, err=%v", got, err)
	}
}

func TestTodo_PERSIST_TAXPROFILE_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db), insertTenant(t, db)
	_, profile, _, _, _ := fixture(t)
	if err := taxprofilestore.New(appConn(t, db)).SaveProfile(context.Background(), alpha.String(), profile); err != nil {
		t.Fatal(err)
	}
	store := taxprofilestore.New(appConn(t, db))
	if _, err := store.LoadProfile(context.Background(), beta.String(), profile.WorkerRef, 1); !errors.Is(err, taxprofile.ErrStoreNotFound) {
		t.Fatalf("cross-tenant profile = %v, want ErrStoreNotFound", err)
	}
	if _, err := store.LoadElection(context.Background(), beta.String(), "election-1"); !errors.Is(err, taxprofile.ErrStoreNotFound) {
		t.Fatalf("cross-tenant election = %v, want ErrStoreNotFound", err)
	}
}

func TestTodo_PERSIST_TAXPROFILE_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	_, profile, _, _, _ := fixture(t)
	if err := taxprofilestore.New(appConn(t, db)).SaveProfile(context.Background(), tenant.String(), profile); err != nil {
		t.Fatal(err)
	}
	got, err := taxprofilestore.New(appConn(t, db)).LoadProfile(context.Background(), tenant.String(), profile.WorkerRef, 1)
	if err != nil || got.CanonicalDigest != profile.CanonicalDigest {
		t.Fatalf("fresh connection profile = %+v, err=%v", got, err)
	}
}

func TestTodo_PERSIST_TAXPROFILE_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	_, profile, _, _, _ := fixture(t)
	if err := taxprofilestore.New(appConn(t, db)).SaveProfile(context.Background(), tenant.String(), profile); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`UPDATE worker_tax_profile_revision SET classification = 'NON_RESIDENT' WHERE tenant_id = $1`,
		`DELETE FROM worker_tax_profile_revision WHERE tenant_id = $1`,
		`UPDATE tax_registration_revision SET jurisdiction = 'US-NY' WHERE tenant_id = $1`,
		`DELETE FROM tax_registration_revision WHERE tenant_id = $1`,
		`UPDATE withholding_election_revision SET kind = 'EXEMPT' WHERE tenant_id = $1`,
		`DELETE FROM withholding_election_revision WHERE tenant_id = $1`,
		`UPDATE tax_exemption_revision SET kind = 'FEDERAL' WHERE tenant_id = $1`,
		`DELETE FROM tax_exemption_revision WHERE tenant_id = $1`,
	}
	for _, statement := range statements {
		t.Run(statement, func(t *testing.T) {
			conn := appConn(t, db)
			tx, err := conn.Begin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(context.Background(), statement, tenant); err == nil {
				t.Fatalf("mutation accepted: %s", statement)
			}
			_ = tx.Rollback(context.Background())
		})
	}
}
