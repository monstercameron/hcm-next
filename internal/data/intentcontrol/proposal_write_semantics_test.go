package intentcontrol_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestProposalWriteSemanticsRoundTrip(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "proposal-write-semantics")
	intentID := insertIntent(t, db, tenant, "proposal-write-semantics")
	insertRevision(t, db, tenant, intentID, 1)
	conn := appConn(t, db)

	start, err := values.ParseLocalDate("2026-11-01")
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.ParseLocalDate("2027-01-01")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "calendar:us-payroll", Version: "2026.4"})
	if err != nil {
		t.Fatal(err)
	}
	interval, err = interval.WithZone(values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026b"}, values.DisambiguationLater)
	if err != nil {
		t.Fatal(err)
	}
	key, err := values.NewResourceKey(values.TenantId(tenant.String()), values.Kind("employment"), "worker-42")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("employment:worker-42", 19)
	if err != nil {
		t.Fatal(err)
	}
	instantStart := time.Date(2026, 9, 5, 12, 0, 0, 123456789, time.UTC)
	instantInterval, err := values.NewInstantInterval(values.NewInstant(instantStart), values.NewInstant(instantStart.Add(24*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	typed, err := intentcontrol.NewWriteItems([]intent.PlannedWrite{
		{
			Subject:     intent.SubjectReference{Kind: "WORKER", SubjectID: "worker-42", AuthorityDomain: "PEOPLE"},
			ResourceKey: key, FieldPath: "employment.manager_id", CurrentCanonicalText: "manager-1", ProposedCanonicalText: "manager-2",
			SourceAuthorityDecision: "authority:people/v4", ExpectedRevision: revision,
			Operation: intent.WriteOperationUpdate, EffectiveInterval: interval,
		},
		{
			Subject:     intent.SubjectReference{Kind: "WORKER", SubjectID: "worker-42", AuthorityDomain: "SECURITY"},
			ResourceKey: key, FieldPath: "employment.access", CurrentCanonicalText: "enabled", ProposedCanonicalText: "disabled",
			SourceAuthorityDecision: "authority:security/v2", ExpectedRevision: revision,
			Operation: intent.WriteOperationDelete, EffectiveInterval: instantInterval,
		},
	})
	if err != nil {
		t.Fatalf("NewWriteItems: %v", err)
	}

	sets := intentcontrol.ProposalSets{Writes: typed}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return (intentcontrol.ProposalSetStore{}).Record(context.Background(), tx, tenant, intentID, 1, sets)
	})
	// A fresh application-role connection models a restarted coordinator: no
	// process-local value from Record participates in reconstruction.
	restarted := appConn(t, db)
	var got intentcontrol.ProposalSets
	inTenantTx(t, restarted, tenant, func(tx dbport.Tx) error {
		var loadErr error
		got, loadErr = (intentcontrol.ProposalSetStore{}).Load(context.Background(), tx, tenant, intentID, 1)
		return loadErr
	})
	if len(got.Writes) != 2 {
		t.Fatalf("loaded writes = %d, want 2", len(got.Writes))
	}
	w := got.Writes[0]
	if w.AuthorityDomain != "PEOPLE" || w.Operation != intent.WriteOperationUpdate || w.SourceAuthorityDecision != "authority:people/v4" {
		t.Fatalf("authority/operation metadata changed: %+v", w)
	}
	if w.EffectiveInterval != interval || w.EffectiveInterval.Zone() != interval.Zone() || w.EffectiveInterval.Disambiguation() != values.DisambiguationLater {
		t.Fatalf("effective interval changed: got %+v want %+v", w.EffectiveInterval, interval)
	}
	instantWrite := got.Writes[1]
	if instantWrite.AuthorityDomain != "SECURITY" || instantWrite.Operation != intent.WriteOperationDelete || instantWrite.EffectiveInterval != instantInterval {
		t.Fatalf("instant write metadata changed: %+v", instantWrite)
	}
}

func TestProposalWriteSemanticsLegacyAndMalformedRowsFailClosed(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "proposal-write-semantics-invalid")
	intentID := insertIntent(t, db, tenant, "proposal-write-semantics-invalid")
	insertRevision(t, db, tenant, intentID, 1)
	insertRevision(t, db, tenant, intentID, 2)
	insertRevision(t, db, tenant, intentID, 3)
	conn := appConn(t, db)

	legacy := referenceSets()
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return (intentcontrol.ProposalSetStore{}).Record(context.Background(), tx, tenant, intentID, 1, legacy)
	})
	var loaded intentcontrol.ProposalSets
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		loaded, err = (intentcontrol.ProposalSetStore{}).Load(context.Background(), tx, tenant, intentID, 1)
		return err
	})
	if len(loaded.Writes) != 1 || loaded.Writes[0].Operation.Valid() || loaded.Writes[0].EffectiveInterval.Validate() == nil {
		t.Fatalf("legacy NULL semantics became executable: %+v", loaded.Writes)
	}

	db.Exec(t, `INSERT INTO proposal_write_item (
		tenant_id, intent_id, revision, ordinal, subject_kind, subject_id, resource_key, field_path,
		current_canonical_text, proposed_canonical_text, expected_revision, source_authority_decision,
		authority_domain, operation, effective_interval_kind, effective_interval_start,
		effective_interval_calendar_ref, effective_interval_calendar_version,
		effective_interval_zone_id, effective_interval_tzdb_version, effective_interval_disambiguation)
		VALUES ($1,$2,2,1,'WORKER','worker-42','resource','field','before','after','rev-1','ALLOW',
		'PEOPLE','UPDATE','LOCAL_DATE','not-a-date','calendar:us','2026.4','America/New_York','2026b','LATER')`, tenant, intentID)
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (intentcontrol.ProposalSetStore{}).Load(context.Background(), tx, tenant, intentID, 2)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "decode write interval") {
		t.Fatalf("malformed stored interval = %v, want decode refusal", err)
	}
	if err := db.ExecErr(`INSERT INTO proposal_write_item (
		tenant_id, intent_id, revision, ordinal, subject_kind, subject_id, resource_key, field_path,
		current_canonical_text, proposed_canonical_text, expected_revision, source_authority_decision,
		authority_domain, operation, effective_interval_kind, effective_interval_start,
		effective_interval_calendar_ref, effective_interval_calendar_version,
		effective_interval_zone_id, effective_interval_tzdb_version, effective_interval_disambiguation)
		VALUES ($1,$2,3,1,'WORKER','worker-42','resource','field','before','after','rev-1','ALLOW',
		'PEOPLE','UPDATE','LOCAL_DATE','2026-01-01','calendar:us','2026.4','America/New_York','2026b',NULL)`, tenant, intentID); err == nil {
		t.Fatal("schema accepted zone metadata without disambiguation")
	}

	otherTenant := insertTenant(t, db, "proposal-write-semantics-other")
	inTenantTx(t, conn, otherTenant, func(tx dbport.Tx) error {
		got, err := (intentcontrol.ProposalSetStore{}).Load(context.Background(), tx, tenant, intentID, 1)
		if err != nil {
			return err
		}
		if len(got.Writes) != 0 {
			t.Fatalf("tenant isolation exposed writes: %+v", got.Writes)
		}
		return nil
	})
}

func TestProposalWriteSemanticsRollback(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "proposal-write-semantics-rollback")
	intentID := insertIntent(t, db, tenant, "proposal-write-semantics-rollback")
	insertRevision(t, db, tenant, intentID, 1)
	conn := appConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(fixedInstant))
	if err != nil {
		t.Fatal(err)
	}
	typed := referenceSets()
	typed.Writes[0].AuthorityDomain = "PEOPLE"
	typed.Writes[0].Operation = intent.WriteOperationUpdate
	typed.Writes[0].EffectiveInterval = interval
	if err := (intentcontrol.ProposalSetStore{}).Record(context.Background(), tx, tenant, intentID, 1, typed); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db, "proposal_write_item", tenant); got != 0 {
		t.Fatalf("rollback retained %d proposal writes", got)
	}
}

func TestProposalWriteSemanticsPre277Upgrade(t *testing.T) {
	db := pgtest.New(t)
	schema := "pre277_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := db.Conn.Exec(context.Background(), `CREATE SCHEMA `+schema+`; SET search_path TO `+schema+`; CREATE TABLE proposal_write_item (
		tenant_id uuid NOT NULL, intent_id uuid NOT NULL, revision bigint NOT NULL, ordinal integer NOT NULL,
		subject_kind text NOT NULL, subject_id text NOT NULL, resource_key text NOT NULL, field_path text NOT NULL,
		current_canonical_text text NOT NULL, proposed_canonical_text text NOT NULL,
		expected_revision text NOT NULL, source_authority_decision text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn.Exec(context.Background(), `INSERT INTO `+schema+`.proposal_write_item VALUES
		($1,$2,1,1,'WORKER','worker-1','resource','field','before','after','rev-1','ALLOW')`, uuid.New(), uuid.New()); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../../../migrations/00277_proposal_write_semantics.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, ok := strings.Cut(string(body), "-- +goose Down")
	if !ok {
		t.Fatal("migration 00277 has no Down marker")
	}
	up = strings.TrimSpace(strings.TrimPrefix(up, "-- Owner: intentcontrol. Additive typed replay metadata for proposal writes.\n-- Legacy rows remain NULL and therefore uncertified for fenced execution.\n-- +goose Up"))
	if _, err := db.Conn.Exec(context.Background(), `SET search_path TO `+schema+`; `+up); err != nil {
		t.Fatalf("upgrade populated pre-00277 table: %v", err)
	}
	var operation, kind *string
	if err := db.Conn.QueryRow(context.Background(), `SELECT operation,effective_interval_kind FROM `+schema+`.proposal_write_item`).Scan(&operation, &kind); err != nil {
		t.Fatal(err)
	}
	if operation != nil || kind != nil {
		t.Fatalf("legacy row was fabricated into typed semantics: operation=%v kind=%v", operation, kind)
	}
}
