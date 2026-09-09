package safetystore_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/safetystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/safety"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var safetyAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply migrations through 00070: %v", err)
	}
	body, err := migrations.FS.ReadFile("00112_safety.sql")
	if err != nil {
		t.Fatalf("read 00112 migration: %v", err)
	}
	up := strings.SplitN(string(body), "-- +goose Down", 2)[0]
	up = strings.Replace(up, "-- +goose Up", "", 1)
	if _, err := db.Conn.Exec(context.Background(), up); err != nil {
		t.Fatalf("apply owned 00112 migration: %v", err)
	}
	body, err = migrations.FS.ReadFile("00278_safety_conformance_records.sql")
	if err != nil {
		t.Fatalf("read 00278 migration: %v", err)
	}
	up = strings.SplitN(string(body), "-- +goose Down", 2)[0]
	up = strings.Replace(up, "-- +goose Up", "", 1)
	// The fixture applies the migration body directly; Goose markers are
	// comments here and the embedded PostgreSQL accepts the PL/pgSQL blocks.
	if _, err := db.Conn.Exec(context.Background(), up); err != nil {
		t.Fatalf("apply owned 00278 migration: %v", err)
	}
	return db
}

func id(name string) string { return uuid.NewSHA1(uuid.Nil, []byte("hcm-next/safety/"+name)).String() }

func insertTenant(t *testing.T, db *pgtest.DB, name string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-safety',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, name, name)
	return tenant
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func tenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	if err := tenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func instant() values.Instant { return values.NewInstant(safetyAt) }

func incident(t *testing.T, revision uint64, parent string, description string) safety.IncidentRevision {
	t.Helper()
	var parentRevision uint64
	if revision > 1 {
		parentRevision = revision - 1
	}
	r, err := safety.NewIncidentRevision(safety.IncidentRevision{ID: id("incident"), CaseRef: id("case"), CompartmentRef: id("operational-compartment"), Revision: revision, ParentRevision: parentRevision, ParentDigest: parent, IncidentAt: instant(), Kind: safety.IncidentInjury, WorkerRef: id("worker"), ReporterRef: id("reporter"), LocationRef: id("location"), Description: description, Status: safety.IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func allRecords(t *testing.T) (safety.IncidentRevision, safety.InjuryRevision, safety.ReportabilityDeterminationRevision, safety.ClaimRevision, safety.WorkRestrictionRevision, safety.CorrectiveActionRevision) {
	t.Helper()
	i := incident(t, 1, "", "incident description")
	rule, _ := safety.RuleFor(safety.OSHAReportableSevere)
	injury, err := safety.NewInjuryRevision(safety.InjuryRevision{ID: id("injury"), CaseRef: i.CaseRef, CompartmentRef: id("medical-compartment"), Revision: 1, IncidentRef: i.ID, WorkerRef: id("worker"), Kind: safety.InjuryPhysical, MedicalEvidenceRef: id("medical-evidence"), Severity: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	reportability, err := safety.NewReportabilityDeterminationRevision(safety.ReportabilityDeterminationRevision{ID: id("reportability"), CaseRef: i.CaseRef, CompartmentRef: id("regulatory-compartment"), Revision: 1, IncidentRef: i.ID, IncidentAt: i.IncidentAt, Class: rule.Class, Clock: rule.Clock, RuleCitation: rule.Citation, Deadline: values.NewInstant(i.IncidentAt.Time().Add(rule.Duration)), Rationale: "derived from declared regulatory clock"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := safety.NewClaimRevision(safety.ClaimRevision{ID: id("claim"), CaseRef: i.CaseRef, CompartmentRef: id("claims-compartment"), Revision: 1, IncidentRef: i.ID, WorkerRef: id("worker"), ClaimRef: "claim-ref", AuthorityRef: id("claims-authority"), Status: safety.ClaimSubmitted})
	if err != nil {
		t.Fatal(err)
	}
	restriction, err := safety.NewWorkRestrictionRevision(safety.WorkRestrictionRevision{ID: id("restriction"), CaseRef: i.CaseRef, CompartmentRef: id("medical-compartment"), Revision: 1, IncidentRef: i.ID, WorkerRef: id("worker"), Kind: safety.RestrictionModifiedDuty, MedicalEvidenceRef: id("medical-evidence"), Status: safety.RestrictionActive})
	if err != nil {
		t.Fatal(err)
	}
	action, err := safety.NewCorrectiveActionRevision(safety.CorrectiveActionRevision{ID: id("action"), CaseRef: i.CaseRef, CompartmentRef: id("operational-compartment"), Revision: 1, IncidentRef: i.ID, OwnerRef: id("owner"), DueRule: "verify-before-close", Action: "repair guard", Status: safety.CorrectiveActionOpen})
	if err != nil {
		t.Fatal(err)
	}
	return i, injury, reportability, claim, restriction, action
}

func saveAll(t *testing.T, tx dbport.Tx, tenant uuid.UUID) {
	t.Helper()
	i, injury, reportability, claim, restriction, action := allRecords(t)
	store := safetystore.New(tx, tenant)
	if err := store.SaveIncident(i); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInjury(injury); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReportability(reportability); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClaim(claim); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRestriction(restriction); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCorrectiveAction(action); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_SAFETY_001(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-primary")
	var want [6]string
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		i, injury, reportability, claim, restriction, action := allRecords(t)
		store := safetystore.New(tx, tenant)
		for _, err := range []error{store.SaveIncident(i), store.SaveInjury(injury), store.SaveReportability(reportability), store.SaveClaim(claim), store.SaveRestriction(restriction), store.SaveCorrectiveAction(action)} {
			if err != nil {
				return err
			}
		}
		want = [6]string{i.CanonicalDigest, injury.CanonicalDigest, reportability.CanonicalDigest, claim.CanonicalDigest, restriction.CanonicalDigest, action.CanonicalDigest}
		return nil
	})
	fresh := appConn(t, db)
	tenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		i, injury, reportability, claim, restriction, action := allRecords(t)
		store := safetystore.New(tx, tenant)
		gotI, ok := store.GetIncident(i.ID, 1)
		if !ok || gotI.CanonicalDigest != want[0] {
			t.Fatalf("incident was not reloaded")
		}
		gotInjury, ok := store.GetInjury(injury.ID, 1)
		if !ok || gotInjury.CanonicalDigest != want[1] {
			t.Fatalf("injury was not reloaded")
		}
		gotReportability, ok := store.GetReportability(reportability.ID, 1)
		if !ok || gotReportability.Deadline.Time() != reportability.Deadline.Time() || gotReportability.CanonicalDigest != want[2] {
			t.Fatalf("reportability was not reloaded")
		}
		gotClaim, ok := store.GetClaim(claim.ID, 1)
		if !ok || gotClaim.CanonicalDigest != want[3] {
			t.Fatalf("claim was not reloaded")
		}
		gotRestriction, ok := store.GetRestriction(restriction.ID, 1)
		if !ok || gotRestriction.CanonicalDigest != want[4] {
			t.Fatalf("restriction was not reloaded")
		}
		gotAction, ok := store.GetCorrectiveAction(action.ID, 1)
		if !ok || gotAction.CanonicalDigest != want[5] {
			t.Fatalf("corrective action was not reloaded")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Integration(t *testing.T) {
	db := newDB(t)
	writer, reader := appConn(t, db), appConn(t, db)
	tenant := insertTenant(t, db, "safety-integration")
	want := incident(t, 1, "", "durable incident")
	tenantTx(t, writer, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(want) })
	tenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		got, ok := safetystore.New(tx, tenant).GetIncident(want.ID, 1)
		if !ok || got.CanonicalDigest != want.CanonicalDigest {
			t.Fatalf("fresh connection lost incident")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Recovery(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-recovery")
	want := incident(t, 1, "", "recovery incident")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(want) })
	fresh := appConn(t, db)
	tenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		got, ok := safetystore.New(tx, tenant).GetIncident(want.ID, 1)
		if !ok || got.Description != want.Description {
			t.Fatalf("row did not survive a fresh connection")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Fault(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-fault")
	first := incident(t, 1, "", "first")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(first) })
	err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(first) })
	if !errors.Is(err, safetystore.ErrDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	var duplicate *safetystore.Error
	if !errors.As(err, &duplicate) || duplicate.Code != safetystore.CodeDuplicateRevision {
		t.Fatalf("duplicate code = %v", err)
	}
	nextA := incident(t, 2, first.CanonicalDigest, "successor A")
	nextB := incident(t, 2, first.CanonicalDigest, "successor B")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(nextA) })
	err = tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(nextB) })
	if !errors.Is(err, safetystore.ErrVersionConflict) {
		t.Fatalf("stale CAS = %v", err)
	}
	var stale *safetystore.Error
	if !errors.As(err, &stale) || stale.Code != safetystore.CodeVersionConflict {
		t.Fatalf("stale code = %v", err)
	}
	wrongCase := nextA
	wrongCase.ID = id("wrong-case-claim")
	wrongCase.CaseRef = id("other-case")
	_, _, _, claim, _, _ := allRecords(t)
	claim.CaseRef = wrongCase.CaseRef
	claim.IncidentRef = first.ID
	err = tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveClaim(claim) })
	if !errors.Is(err, safetystore.ErrReferenceConflict) {
		t.Fatalf("cross-case incident = %v", err)
	}
}

func TestTodo_PERSIST_SAFETY_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "safety-alpha"), insertTenant(t, db, "safety-beta")
	conn := appConn(t, db)
	want := incident(t, 1, "", "alpha-only")
	tenantTx(t, conn, alpha, func(tx dbport.Tx) error { return safetystore.New(tx, alpha).SaveIncident(want) })
	tenantTx(t, conn, beta, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM safety_incident_revision`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("beta saw %d alpha rows", count)
		}
		if _, ok := safetystore.New(tx, beta).GetIncident(want.ID, 1); ok {
			t.Fatal("cross-tenant read succeeded")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Mutation(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-mutation")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { saveAll(t, tx, tenant); return nil })
	for _, table := range []string{"safety_incident_revision", "safety_injury_revision", "safety_reportability_revision", "safety_claim_revision", "safety_work_restriction_revision", "safety_corrective_action_revision"} {
		for _, operation := range []string{"UPDATE " + table + " SET canonical_digest=canonical_digest WHERE tenant_id=$1", "DELETE FROM " + table + " WHERE tenant_id=$1"} {
			err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), operation, tenant); return err })
			if err == nil {
				t.Fatalf("%s accepted mutation", table)
			}
		}
	}
}

func TestTodo_SAFETY_002_ConformanceRecordsDurableAndTenantScoped(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	alpha, beta := insertTenant(t, db, "safety-conformance-alpha"), insertTenant(t, db, "safety-conformance-beta")
	filing, err := safety.NewFilingRevision(safety.FilingRevision{ID: id("filing"), CaseRef: id("case"), CompartmentRef: id("regulatory"), Revision: 1, IncidentRef: id("incident"), AuthorityRef: id("authority"), ProviderRef: id("provider"), SubmissionRef: "submission-1", SignerRef: id("signer"), SignatureRef: "signature-1", Status: safety.FilingSubmitted, Obligations: []string{"notify"}})
	if err != nil {
		t.Fatal(err)
	}
	payment, err := safety.NewWorkersCompPaymentRevision(safety.WorkersCompPaymentRevision{ID: id("payment"), CaseRef: id("case"), CompartmentRef: id("claims"), Revision: 1, ClaimRef: "claim", WorkerRef: id("worker"), AmountMinor: 100, Currency: "USD", Status: safety.PaymentSettled, ObservationRef: "settled"})
	if err != nil {
		t.Fatal(err)
	}
	clearance, err := safety.NewRestrictionClearanceRevision(safety.RestrictionClearanceRevision{ID: id("clearance"), CaseRef: id("case"), CompartmentRef: id("medical"), Revision: 1, RestrictionRef: id("restriction"), WorkerRef: id("worker"), EvidenceRef: "note", EvidenceDigest: "sha256:note", AuthorityRef: "clinician", ObservationRef: "observed"})
	if err != nil {
		t.Fatal(err)
	}
	correction, err := safety.NewSafetyCorrectionRevision(safety.SafetyCorrectionRevision{ID: id("correction"), CaseRef: id("case"), CompartmentRef: id("regulatory"), Revision: 1, IncidentRef: id("incident"), SourceRevisionDigest: filing.CanonicalDigest, Reason: "late source", EvidenceRef: "evidence", AmendedFilingRef: filing.CanonicalDigest})
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, err := safety.NewSafetyReconciliationRevision(safety.SafetyReconciliationRevision{ID: id("reconciliation"), CaseRef: id("case"), CompartmentRef: id("regulatory"), Revision: 1, IncidentRef: id("incident"), SourceRevisionDigest: filing.CanonicalDigest, ObservationRef: "observation-1", Obligations: []string{"amend filing"}, ObservedAt: values.NewInstant(safetyAt.Add(123 * time.Nanosecond)), Status: safety.ReconciliationOpen})
	if err != nil {
		t.Fatal(err)
	}
	tenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		s := safetystore.New(tx, alpha)
		for _, e := range []error{s.SaveFiling(filing), s.SaveWorkersCompPayment(payment), s.SaveRestrictionClearance(clearance), s.SaveSafetyReconciliation(reconciliation), s.SaveSafetyCorrection(correction)} {
			if e != nil {
				return e
			}
		}
		return nil
	})
	fresh := appConn(t, db)
	tenantTx(t, fresh, alpha, func(tx dbport.Tx) error {
		s := safetystore.New(tx, alpha)
		if got, ok := s.GetFiling(filing.ID, 1); !ok || got.CanonicalDigest != filing.CanonicalDigest {
			t.Fatal("filing not durable")
		}
		if got, ok := s.GetWorkersCompPayment(payment.ID, 1); !ok || got.CanonicalDigest != payment.CanonicalDigest {
			t.Fatal("payment not durable")
		}
		if got, ok := s.GetRestrictionClearance(clearance.ID, 1); !ok || got.CanonicalDigest != clearance.CanonicalDigest {
			t.Fatal("clearance not durable")
		}
		if got, ok := s.GetSafetyCorrection(correction.ID, 1); !ok || got.CanonicalDigest != correction.CanonicalDigest {
			t.Fatal("correction not durable")
		}
		if got, ok := s.GetSafetyReconciliation(reconciliation.ID, 1); !ok || got.CanonicalDigest != reconciliation.CanonicalDigest || got.ObservedAt.Time() != reconciliation.ObservedAt.Time() {
			t.Fatal("reconciliation not durably reconstructed")
		}
		return nil
	})
	families := []struct {
		table, idColumn string
		id              string
	}{
		{"safety_filing_revision", "filing_id", filing.ID},
		{"safety_workers_comp_payment_revision", "payment_id", payment.ID},
		{"safety_restriction_clearance_revision", "clearance_id", clearance.ID},
		{"safety_reconciliation_revision", "reconciliation_id", reconciliation.ID},
		{"safety_correction_revision", "correction_id", correction.ID},
	}
	cloneSQL := func(family struct{ table, idColumn, id string }, overrides string) string {
		return fmt.Sprintf(`INSERT INTO %s SELECT (jsonb_populate_record(NULL::%s, to_jsonb(t) || jsonb_build_object('row_id',$3::text,'revision',$4::bigint,'parent_revision',$5::bigint,'parent_digest',$6::text,'canonical_digest',$7::text)%s)).* FROM %s t WHERE tenant_id=$1 AND %s=$2 AND revision=$8`, family.table, family.table, overrides, family.table, family.idColumn)
	}
	for _, family := range families {
		family := family
		t.Run("direct SQL lineage "+family.table, func(t *testing.T) {
			idValue := uuid.MustParse(family.id)
			var parentDigest string
			tenantTx(t, conn, alpha, func(tx dbport.Tx) error {
				if err := tx.QueryRow(context.Background(), fmt.Sprintf(`SELECT canonical_digest FROM %s WHERE tenant_id=$1 AND %s=$2 AND revision=1`, family.table, family.idColumn), alpha, idValue).Scan(&parentDigest); err != nil {
					return err
				}
				_, err := tx.Exec(context.Background(), cloneSQL(family, ""), alpha, idValue, uuid.New(), 2, 1, parentDigest, strings.Repeat("a", 64), 1)
				return err
			})
			for name, overrides := range map[string]string{
				"null parent":       "",
				"bad digest":        "",
				"cross case":        ` || jsonb_build_object('case_ref',$9::text)`,
				"cross compartment": ` || jsonb_build_object('compartment_ref',$9::text)`,
			} {
				name, overrides := name, overrides
				t.Run(name, func(t *testing.T) {
					parentRevision, digest := any(int64(2)), parentDigest
					if name == "null parent" {
						parentRevision, digest = nil, ""
					} else if name == "bad digest" {
						digest = strings.Repeat("b", 64)
					}
					args := []any{alpha, idValue, uuid.New(), 3, parentRevision, nullStringTest(digest), strings.Repeat("c", 64), 2}
					if overrides != "" {
						args = append(args, uuid.New())
					}
					err := tenantTxErr(conn, alpha, func(tx dbport.Tx) error {
						_, err := tx.Exec(context.Background(), cloneSQL(family, overrides), args...)
						return err
					})
					if err == nil {
						t.Fatal("forged successor was accepted")
					}
				})
			}
			for _, operation := range []string{"UPDATE", "DELETE"} {
				err := tenantTxErr(conn, alpha, func(tx dbport.Tx) error {
					if operation == "UPDATE" {
						_, err := tx.Exec(context.Background(), fmt.Sprintf(`UPDATE %s SET canonical_digest=canonical_digest WHERE tenant_id=$1 AND %s=$2`, family.table, family.idColumn), alpha, idValue)
						return err
					}
					_, err := tx.Exec(context.Background(), fmt.Sprintf(`DELETE FROM %s WHERE tenant_id=$1 AND %s=$2`, family.table, family.idColumn), alpha, idValue)
					return err
				})
				if err == nil {
					t.Fatalf("%s bypassed append-only protection", operation)
				}
			}
			tenantTx(t, conn, alpha, func(tx dbport.Tx) error {
				var count int
				if err := tx.QueryRow(context.Background(), fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id=$1 AND %s=$2 AND revision=3`, family.table, family.idColumn), alpha, idValue).Scan(&count); err != nil {
					return err
				}
				if count != 0 {
					t.Fatalf("failed lineage writes left %d revision-3 rows", count)
				}
				return nil
			})
		})
	}
	tenantTx(t, conn, beta, func(tx dbport.Tx) error {
		for _, family := range families {
			var count int
			if err := tx.QueryRow(context.Background(), fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s=$1`, family.table, family.idColumn), uuid.MustParse(family.id)).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Fatalf("cross-tenant %s read returned %d rows", family.table, count)
			}
		}
		return nil
	})
}

func nullStringTest(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func TestTodo_SAFETY_002_Race(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "safety-payment-race")
	settled, err := safety.NewWorkersCompPaymentRevision(safety.WorkersCompPaymentRevision{ID: id("race-payment"), CaseRef: id("race-case"), CompartmentRef: id("claims"), Revision: 1, ClaimRef: "claim-race", WorkerRef: id("race-worker"), AmountMinor: 4250, Currency: "USD", Status: safety.PaymentSettled, ObservationRef: "settled-observation"})
	if err != nil {
		t.Fatal(err)
	}
	setup := appConn(t, db)
	tenantTx(t, setup, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveWorkersCompPayment(settled) })
	reversals := make([]safety.WorkersCompPaymentRevision, 2)
	for i := range reversals {
		reversals[i], err = safety.NewWorkersCompPaymentRevision(safety.WorkersCompPaymentRevision{ID: settled.ID, CaseRef: settled.CaseRef, CompartmentRef: settled.CompartmentRef, Revision: 2, ParentRevision: 1, ParentDigest: settled.CanonicalDigest, ClaimRef: settled.ClaimRef, WorkerRef: settled.WorkerRef, AmountMinor: settled.AmountMinor, Currency: settled.Currency, Status: safety.PaymentReversed, ObservationRef: fmt.Sprintf("reversal-observation-%d", i), ReversalRef: fmt.Sprintf("reversal-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	results := make([]error, 2)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := range results {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			conn := appConn(t, db)
			start.Wait()
			results[i] = tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveWorkersCompPayment(reversals[i]) })
		}(i)
	}
	start.Done()
	done.Wait()
	wins := 0
	for _, err := range results {
		if err == nil {
			wins++
		} else if !errors.Is(err, safetystore.ErrVersionConflict) {
			t.Fatalf("unexpected reversal result: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("reversal wins = %d, want exactly 1", wins)
	}
	tenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		var count int
		var amount int64
		var parent string
		if err := tx.QueryRow(context.Background(), `SELECT count(*), min(amount_minor), min(parent_digest) FROM safety_workers_comp_payment_revision WHERE tenant_id=$1 AND payment_id=$2 AND revision=2`, tenant, uuid.MustParse(settled.ID)).Scan(&count, &amount, &parent); err != nil {
			return err
		}
		if count != 1 || amount != settled.AmountMinor || domainDigestForTest(parent) != settled.CanonicalDigest {
			t.Fatalf("stored reversal count/amount/parent = %d/%d/%s", count, amount, parent)
		}
		return nil
	})
}

func domainDigestForTest(v string) string {
	if strings.HasPrefix(v, "sha256:") {
		return v
	}
	return "sha256:" + v
}
