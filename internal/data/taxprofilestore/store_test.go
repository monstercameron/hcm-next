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

func TestTodo_TAXPROFILE_002_PostgresSuccessorFenceAndEffectiveEnd(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	worker, _, _, election, _ := fixture(t)
	store := taxprofilestore.New(appConn(t, db))
	if err := store.SaveElection(context.Background(), tenant.String(), election); err != nil {
		t.Fatal(err)
	}
	next := election
	next.Kind = taxprofile.ElectionAdditionalAmount
	next.Amount, _ = values.NewDecimal("17.2500", 4, values.RoundingHalfEven)
	next.Effective, _ = values.NewInstantInterval(taxInstant(t, "2025-12-01T00:00:00.123456789Z"), taxInstant(t, "2025-12-31T00:00:00.987654321Z"))
	next.KnownAt = taxInstant(t, "2026-02-02T00:00:00.456789123Z")
	next, err := taxprofile.NewWithholdingElectionRevision(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveElectionSuccessor(context.Background(), tenant.String(), election.CanonicalDigest, next); err != nil {
		t.Fatalf("successor: %v", err)
	}
	loaded, err := store.LoadElection(context.Background(), tenant.String(), election.ElectionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ElectionID != election.ElectionID || loaded.WorkerRef != worker.String() {
		t.Fatalf("loaded identity = %+v", loaded)
	}
	start, _ := loaded.Effective.StartInstant()
	if start.String() != "2025-12-01T00:00:00.123456789Z" || loaded.KnownAt.String() != "2026-02-02T00:00:00.456789123Z" {
		t.Fatalf("nanosecond start/known = %s / %s", start.String(), loaded.KnownAt.String())
	}
	if end, ok := loaded.Effective.EndInstant(); !ok || end.String() != "2025-12-31T00:00:00.987654321Z" {
		t.Fatalf("effective end = %v, %v", end, ok)
	}
	if loaded.CanonicalDigest != next.CanonicalDigest || loaded.Amount.Scale() != 4 || loaded.Amount.String() != "17.2500" {
		t.Fatalf("decimal/digest round trip = amount %s scale %d digest %s", loaded.Amount.String(), loaded.Amount.Scale(), loaded.CanonicalDigest)
	}
	if err := store.SaveElection(context.Background(), tenant.String(), next); !errors.Is(err, taxprofile.ErrStoreStaleCAS) {
		t.Fatalf("plain save bypass = %v", err)
	}
	wrongIdentity := next
	wrongIdentity.WorkerRef = uuid.NewString()
	wrongIdentity.Effective = taxInterval(t, "2026-04-01T00:00:00Z")
	wrongIdentity, _ = taxprofile.NewWithholdingElectionRevision(wrongIdentity)
	if err := store.SaveElectionSuccessor(context.Background(), tenant.String(), next.CanonicalDigest, wrongIdentity); !errors.Is(err, taxprofile.ErrStoreInvalid) {
		t.Fatalf("identity fork = %v", err)
	}
	rawSuccessor := next
	rawSuccessor.Effective = taxInterval(t, "2026-04-01T00:00:00Z")
	rawSuccessor.KnownAt = taxInstant(t, "2026-04-02T00:00:00Z")
	rawSuccessor.CanonicalDigest = ""
	if err := store.SaveElectionSuccessor(context.Background(), tenant.String(), next.CanonicalDigest, rawSuccessor); err != nil {
		t.Fatalf("uncanonicalized successor = %v", err)
	}
	if err := store.SaveElectionSuccessor(context.Background(), tenant.String(), election.CanonicalDigest, next); !errors.Is(err, taxprofile.ErrStoreStaleCAS) {
		t.Fatalf("replay = %v", err)
	}
	other := insertTenant(t, db)
	if err := store.SaveElectionSuccessor(context.Background(), other.String(), election.CanonicalDigest, next); !errors.Is(err, taxprofile.ErrStoreStaleCAS) {
		t.Fatalf("cross tenant = %v", err)
	}
	var rows int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM withholding_election_revision WHERE tenant_id=$1 AND election_id=$2`, tenant, election.ElectionID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Fatalf("append-only revision count = %d, want 3", rows)
	}
	db.Exec(t, `DELETE FROM withholding_election_head WHERE tenant_id=$1 AND election_id=$2`, tenant, election.ElectionID)
	db.Exec(t, `INSERT INTO withholding_election_head (tenant_id,election_id,current_digest)
		SELECT DISTINCT ON (tenant_id,election_id) tenant_id,election_id,canonical_digest
		FROM withholding_election_revision WHERE tenant_id=$1 AND election_id=$2
		ORDER BY tenant_id,election_id,known_at DESC,row_id DESC,effective_from DESC`, tenant, election.ElectionID)
	var backfilledDigest string
	if err := db.QueryRow(context.Background(), `SELECT current_digest FROM withholding_election_head WHERE tenant_id=$1 AND election_id=$2`, tenant, election.ElectionID).Scan(&backfilledDigest); err != nil {
		t.Fatal(err)
	}
	backfilled, err := store.LoadElection(context.Background(), tenant.String(), election.ElectionID)
	if err != nil {
		t.Fatal(err)
	}
	if "sha256:"+backfilledDigest != backfilled.CanonicalDigest || backfilled.KnownAt.String() != "2026-04-02T00:00:00Z" {
		t.Fatalf("legacy backfill head = %s known %s", backfilledDigest, backfilled.KnownAt.String())
	}
}

func TestTodo_TAXPROFILE_002_PostgresConcurrentSuccessorHasOneWinner(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	_, _, _, election, _ := fixture(t)
	base := taxprofilestore.New(appConn(t, db))
	if err := base.SaveElection(context.Background(), tenant.String(), election); err != nil {
		t.Fatal(err)
	}
	next := election
	next.Kind = taxprofile.ElectionMultipleJobs
	next.Effective = taxInterval(t, "2026-02-01T00:00:00Z")
	next, _ = taxprofile.NewWithholdingElectionRevision(next)
	left, right := taxprofilestore.New(appConn(t, db)), taxprofilestore.New(appConn(t, db))
	results := make(chan error, 2)
	go func() {
		results <- left.SaveElectionSuccessor(context.Background(), tenant.String(), election.CanonicalDigest, next)
	}()
	go func() {
		results <- right.SaveElectionSuccessor(context.Background(), tenant.String(), election.CanonicalDigest, next)
	}()
	var success, stale int
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, taxprofile.ErrStoreStaleCAS):
			stale++
		default:
			t.Fatalf("concurrent successor = %v", err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("success=%d stale=%d", success, stale)
	}
}

func TestTodo_TAXPROFILE_002_PostgresSubMicrosecondEffectiveBounds(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db)
	worker, _, _, election, _ := fixture(t)
	election.ElectionID = "submicrosecond-election"
	election.Effective, _ = values.NewInstantInterval(
		taxInstant(t, "2026-01-01T00:00:00.123456100Z"),
		taxInstant(t, "2026-01-01T00:00:00.123456900Z"),
	)
	election, err := taxprofile.NewWithholdingElectionRevision(election)
	if err != nil {
		t.Fatal(err)
	}
	store := taxprofilestore.New(appConn(t, db))
	if err := store.SaveElection(context.Background(), tenant.String(), election); err != nil {
		t.Fatalf("same-microsecond interval: %v", err)
	}
	loaded, err := store.LoadElection(context.Background(), tenant.String(), election.ElectionID)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := loaded.Effective.StartInstant()
	end, _ := loaded.Effective.EndInstant()
	if start.String() != "2026-01-01T00:00:00.1234561Z" || end.String() != "2026-01-01T00:00:00.1234569Z" || loaded.CanonicalDigest != election.CanonicalDigest {
		t.Fatalf("same-microsecond round trip = %s to %s digest %s", start.String(), end.String(), loaded.CanonicalDigest)
	}
	if err := db.ExecErr(`INSERT INTO withholding_election_revision
		(tenant_id,row_id,election_id,worker_ref,jurisdiction,kind,form_revision_ref,evidence_ref,
		effective_from,effective_to,known_at,canonical_digest,effective_from_ns_remainder,effective_to_ns_remainder,known_at_ns_remainder)
		VALUES ($1,$2,'reversed-submicrosecond',$3,'US-CA','STANDARD_WITHHOLDING','form','evidence',
		timestamptz '2026-01-01T00:00:00.123456Z',timestamptz '2026-01-01T00:00:00.123456Z',
		timestamptz '2026-01-02T00:00:00Z',$4,900,100,0)`, tenant, uuid.New(), worker, digestA); err == nil {
		t.Fatal("database accepted reversed sub-microsecond effective bounds")
	}
}

func TestTodo_TAXPROFILE_002_PostgresPopulatedLegacyMigration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 262); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	tenant := insertTenant(t, db)
	worker := uuid.New()
	// Legacy numeric(19,4) rows retain only PostgreSQL's physical scale. Any
	// different pre-migration declared scale was never stored and cannot be
	// reconstructed by this migration.
	amount, err := values.NewDecimal("17.2500", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	election, err := taxprofile.NewWithholdingElectionRevision(taxprofile.WithholdingElectionRevision{
		ElectionID: "legacy-election", WorkerRef: worker.String(), Jurisdiction: "US-CA",
		Kind: taxprofile.ElectionAdditionalAmount, FormRevisionRef: "form:w4:v2025", EvidenceRef: "evidence:legacy",
		Amount: amount, Effective: taxInterval(t, "2025-01-01T00:00:00Z"), KnownAt: taxInstant(t, "2025-01-02T00:00:00Z"),
	})
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(t, `INSERT INTO withholding_election_revision
		(tenant_id,row_id,election_id,worker_ref,jurisdiction,kind,form_revision_ref,evidence_ref,amount,effective_from,known_at,canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tenant, uuid.New(), election.ElectionID, worker,
		election.Jurisdiction, election.Kind, election.FormRevisionRef, election.EvidenceRef, "17.25",
		taxInstant(t, "2025-01-01T00:00:00Z").Time(), election.KnownAt.Time(), election.CanonicalDigest[len("sha256:"):])
	if err := db.ExecErr(`UPDATE withholding_election_revision SET amount=18 WHERE tenant_id=$1 AND election_id=$2`, tenant, election.ElectionID); err == nil {
		t.Fatal("legacy append-only trigger permitted mutation")
	}
	if _, err := db.Provider(t).UpTo(context.Background(), 263); err != nil {
		t.Fatalf("migrate populated legacy schema: %v", err)
	}
	var scale, effectiveFromNS, effectiveToNS, knownAtNS *int16
	if err := db.QueryRow(context.Background(), `SELECT amount_scale,effective_from_ns_remainder,effective_to_ns_remainder,known_at_ns_remainder
		FROM withholding_election_revision WHERE tenant_id=$1 AND election_id=$2`, tenant, election.ElectionID).
		Scan(&scale, &effectiveFromNS, &effectiveToNS, &knownAtNS); err != nil {
		t.Fatal(err)
	}
	if scale != nil || effectiveFromNS != nil || effectiveToNS != nil || knownAtNS != nil {
		t.Fatalf("legacy row was backfilled: scale=%v from=%v to=%v known=%v", scale, effectiveFromNS, effectiveToNS, knownAtNS)
	}
	loaded, err := taxprofilestore.New(appConn(t, db)).LoadElection(context.Background(), tenant.String(), election.ElectionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CanonicalDigest != election.CanonicalDigest || loaded.Amount.Scale() != 4 || loaded.Amount.String() != "17.2500" {
		t.Fatalf("legacy fallback = amount %s scale %d digest %s", loaded.Amount.String(), loaded.Amount.Scale(), loaded.CanonicalDigest)
	}
}
