package surveystore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/surveystore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/survey"
	"github.com/monstercameron/hcm-next/internal/engines/messagetemplate"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db           *pgtest.DB
	tenant       uuid.UUID
	other        uuid.UUID
	bank         survey.QuestionBankRevision
	survey       survey.SurveyRevision
	camp         survey.CampaignRevision
	sample       survey.CampaignSample
	respCampaign survey.ResponseCampaign
	responses    []survey.ResponseRecord
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.NewEmpty(t)
	// 00082 currently has a pre-existing unterminated dollar-quoted function.
	// Apply the known-good substrate through 00070, then apply this lane's
	// migration directly so the survey tests still exercise real PostgreSQL.
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply migrations through 00070: %v", err)
	}
	if _, err := db.Provider(t).ApplyVersion(context.Background(), 122, true); err != nil {
		t.Fatalf("apply migration 00122: %v", err)
	}
	id, other := uuid.New(), uuid.New()
	for _, tenantID := range []uuid.UUID{id, other} {
		db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
			VALUES ($1, $2, 'survey-cell', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenantID, "survey-"+tenantID.String(), "survey "+tenantID.String())
	}
	ref := func(kind, id string) values.EntityRef {
		return values.EntityRef{Tenant: values.TenantId("test-tenant"), Kind: values.Kind(kind), Id: id}
	}
	qb := survey.QuestionBankRevision{
		QuestionBankID: "bank-1", Revision: 1, CreatedAt: values.NewInstant(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)),
		Questions: []survey.Question{{QuestionID: "q-1", Kind: survey.QuestionKindChoice, Text: "How are you?", Required: true, Choices: []string{"good", "bad"}}},
	}
	sv := survey.SurveyRevision{SurveyID: "survey-1", Revision: 1, Title: "Experience", QuestionBankRef: ref("question_bank", "550e8400-e29b-41d4-a716-446655440002"), CreatedAt: qb.CreatedAt}
	camp := survey.CampaignRevision{CampaignID: "campaign-1", Revision: 1, SurveyRef: ref("survey", "550e8400-e29b-41d4-a716-446655440001"), PopulationSnapshotRef: ref("population_snapshot", "550e8400-e29b-41d4-a716-446655440004"), WindowStart: values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)), WindowEnd: values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), AnonymityThreshold: 5, ChannelRefs: []values.EntityRef{ref("channel", "550e8400-e29b-41d4-a716-446655440005")}, CreatedAt: qb.CreatedAt}
	binding := survey.PopulationBindingRef{DefinitionID: "population-1", RevisionVersion: "7", Digest: strings.Repeat("b", 64)}
	rule := survey.SamplingRule{Kind: survey.SamplingKindWhole, Nonresponse: survey.NonresponsePolicyExcludeFromAnalysis}
	sample, err := survey.FreezeSample(camp.CampaignID, binding, rule, []string{"member-1", "member-2", "member-3", "member-4", "member-5"}, false, 0, qb.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	responseCampaign := survey.ResponseCampaign{Campaign: camp, FormDefinition: survey.FormDefinitionRef{Ref: "form-1", Version: 1, Digest: strings.Repeat("f", 64)}, Disclosure: survey.DisclosurePseudonymous, AllowRevision: false}
	responses := make([]survey.ResponseRecord, 0, 5)
	for i := 1; i <= 5; i++ {
		member := "member-" + string(rune('0'+i))
		record, err := survey.NewResponseStore().RecordResponse(responseCampaign, survey.ResponseInput{SubjectRef: member, FormDefinition: responseCampaign.FormDefinition, Answers: []survey.ResponseAnswer{{QuestionID: "q-1", Value: "good"}}, SubmittedAt: values.NewInstant(time.Date(2026, 9, i, 12, 0, 0, 0, time.UTC))}, survey.PseudonymPortFunc(func(subject, _, _ string) (string, error) { return subject, nil }), survey.FormValidatorFunc(func(survey.FormDefinitionRef, []survey.ResponseAnswer) (string, error) {
			return strings.Repeat("e", 64), nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		responses = append(responses, record)
	}
	return fixture{db: db, tenant: id, other: other, bank: qb, survey: sv, camp: camp, sample: sample, respCampaign: responseCampaign, responses: responses}
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return conn
}

func scopedTx(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, tenantID); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal(err)
	}
	return tx
}

func saveDefinitions(t *testing.T, store surveystore.Store, f fixture) {
	t.Helper()
	ctx := context.Background()
	for _, save := range []func() error{func() error { return store.SaveQuestionBank(ctx, f.tenant.String(), f.bank) }, func() error { return store.SaveSurvey(ctx, f.tenant.String(), f.survey) }, func() error { return store.SaveCampaign(ctx, f.tenant.String(), f.camp) }, func() error { return store.SaveCampaignSample(ctx, f.tenant.String(), f.sample) }} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
}

func launch() survey.LaunchRecord {
	return survey.LaunchRecord{CampaignID: "campaign-1", CampaignRevision: 1, CampaignDigest: strings.Repeat("c", 64), Purpose: messagetemplate.Purpose("SURVEY"), SampleDigest: strings.Repeat("e", 64), MembershipProtected: false, TemplateKey: "survey-template", TemplateVersion: 1, TemplateDigest: strings.Repeat("t", 64), AudienceDigest: strings.Repeat("a", 64), DeliveryPlanDigest: strings.Repeat("d", 64), LaunchedBy: "requester", Approver: "approver", LaunchedAt: values.NewInstant(time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)), Digest: strings.Repeat("f", 64)}
}

func TestTodo_PERSIST_SURVEY_001(t *testing.T) {
	f := newFixture(t)
	conn := appConn(t, f.db)
	tx := scopedTx(t, conn, f.tenant)
	store := surveystore.New(tx)
	saveDefinitions(t, store, f)
	if err := store.AppendLaunch(context.Background(), f.tenant.String(), launch(), 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendResponse(context.Background(), f.tenant.String(), f.camp.CampaignID, f.camp.Revision, "member-1", f.responses[0], 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_SURVEY_001_Integration(t *testing.T) {
	f := newFixture(t)
	conn := appConn(t, f.db)
	tx := scopedTx(t, conn, f.tenant)
	store := surveystore.New(tx)
	saveDefinitions(t, store, f)
	if err := store.AppendLaunch(context.Background(), f.tenant.String(), launch(), 1); err != nil {
		t.Fatal(err)
	}
	for i, record := range f.responses {
		if err := store.AppendResponse(context.Background(), f.tenant.String(), f.camp.CampaignID, f.camp.Revision, "member-"+string(rune('1'+i)), record, uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := store.LoadQuestionBank(context.Background(), f.tenant.String(), f.bank.QuestionBankID, 1); err != nil || got.Digest() != f.bank.Digest() {
		t.Fatalf("question bank = %v, err = %v", got, err)
	}
	if got, err := store.LoadSurvey(context.Background(), f.tenant.String(), f.survey.SurveyID, 1); err != nil || got.Digest() != f.survey.Digest() {
		t.Fatalf("survey = %v, err = %v", got, err)
	}
	if got, err := store.LoadCampaign(context.Background(), f.tenant.String(), f.camp.CampaignID, 1); err != nil || got.Digest() != f.camp.Digest() {
		t.Fatalf("campaign = %v, err = %v", got, err)
	}
	if got, err := store.LoadCampaignSample(context.Background(), f.tenant.String(), f.camp.CampaignID); err != nil || got.Digest != f.sample.Digest {
		t.Fatalf("sample = %v, err = %v", got, err)
	}
	if got, err := store.ListLaunches(context.Background(), f.tenant.String(), f.camp.CampaignID); err != nil || len(got) != 1 || got[0].Sequence != 1 {
		t.Fatalf("launches = %v, err = %v", got, err)
	}
	if got, err := store.ListResponses(context.Background(), f.tenant.String(), f.camp.CampaignID, 1); err != nil || len(got) != 5 {
		t.Fatalf("responses = %v, err = %v", got, err)
	}
	if got, err := store.Aggregate(context.Background(), f.tenant.String(), f.respCampaign); err != nil || got.Count != 5 {
		t.Fatalf("aggregate = %v, err = %v", got, err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_SURVEY_001_Fault(t *testing.T) {
	f := newFixture(t)
	conn := appConn(t, f.db)
	tx := scopedTx(t, conn, f.tenant)
	store := surveystore.New(tx)
	if err := store.SaveQuestionBank(context.Background(), f.tenant.String(), f.bank); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCampaignSample(context.Background(), f.tenant.String(), f.sample); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuestionBank(context.Background(), f.tenant.String(), f.bank); !errors.Is(err, surveystore.ErrDuplicate) || surveystore.CodeOf(err) != surveystore.CodeDuplicate {
		t.Fatalf("duplicate revision = %v, code %s", err, surveystore.CodeOf(err))
	}
	if err := store.AppendResponse(context.Background(), f.tenant.String(), f.camp.CampaignID, f.camp.Revision, "member-outside", f.responses[0], 99); !errors.Is(err, surveystore.ErrInvalid) {
		t.Fatalf("outside-sample response = %v, want typed invalid error", err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The port exposes a stable stale-CAS type for future caller-fenced live
	// operations; this migration intentionally has no mutable CAS table.
	if surveystore.CodeOf(surveystore.ErrStaleCAS) != surveystore.CodeStaleCAS {
		t.Fatal("stale CAS code is not stable")
	}
}

func TestTodo_PERSIST_SURVEY_001_Security(t *testing.T) {
	f := newFixture(t)
	conn := appConn(t, f.db)
	tx := scopedTx(t, conn, f.tenant)
	store := surveystore.New(tx)
	saveDefinitions(t, store, f)
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	otherTx := scopedTx(t, conn, f.other)
	defer otherTx.Rollback(context.Background())
	otherStore := surveystore.New(otherTx)
	if _, err := otherStore.LoadQuestionBank(context.Background(), f.tenant.String(), f.bank.QuestionBankID, 1); !errors.Is(err, surveystore.ErrNotFound) {
		t.Fatalf("cross-tenant question bank = %v", err)
	}
	var count int
	if err := otherTx.QueryRow(context.Background(), `SELECT count(*) FROM survey_campaign_revision`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant campaign rows = %d", count)
	}
}

func TestTodo_PERSIST_SURVEY_001_Recovery(t *testing.T) {
	f := newFixture(t)
	conn := appConn(t, f.db)
	tx := scopedTx(t, conn, f.tenant)
	store := surveystore.New(tx)
	saveDefinitions(t, store, f)
	if err := store.AppendResponse(context.Background(), f.tenant.String(), f.camp.CampaignID, 1, "member-1", f.responses[0], 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	fresh := appConn(t, f.db)
	freshTx := scopedTx(t, fresh, f.tenant)
	defer freshTx.Rollback(context.Background())
	freshStore := surveystore.New(freshTx)
	if _, err := freshStore.LoadCampaignSample(context.Background(), f.tenant.String(), f.camp.CampaignID); err != nil {
		t.Fatal(err)
	}
	if rows, err := freshStore.ListResponses(context.Background(), f.tenant.String(), f.camp.CampaignID, 1); err != nil || len(rows) != 1 {
		t.Fatalf("reloaded responses = %v, err = %v", rows, err)
	}
}

func TestTodo_PERSIST_SURVEY_001_Mutation(t *testing.T) {
	f := newFixture(t)
	conn := appConn(t, f.db)
	tx := scopedTx(t, conn, f.tenant)
	store := surveystore.New(tx)
	saveDefinitions(t, store, f)
	if err := store.AppendLaunch(context.Background(), f.tenant.String(), launch(), 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendResponse(context.Background(), f.tenant.String(), f.camp.CampaignID, 1, "member-1", f.responses[0], 1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE survey_launch_record SET purpose = 'changed' WHERE campaign_id = 'campaign-1'`, `DELETE FROM survey_launch_record WHERE campaign_id = 'campaign-1'`, `UPDATE survey_response_record SET response_key = 'changed' WHERE campaign_id = 'campaign-1'`, `DELETE FROM survey_response_record WHERE campaign_id = 'campaign-1'`, `UPDATE survey_campaign_revision SET title = 'changed' WHERE campaign_id = 'campaign-1'`, `DELETE FROM survey_campaign_sample WHERE campaign_id = 'campaign-1'`} {
		attempt := scopedTx(t, conn, f.tenant)
		if _, err := attempt.Exec(context.Background(), statement); err == nil {
			t.Fatalf("mutation succeeded: %s", statement)
		}
		_ = attempt.Rollback(context.Background())
	}
}
