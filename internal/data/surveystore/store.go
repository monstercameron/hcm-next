// Package surveystore persists the survey domain through the caller-owned
// dbport connection or transaction. It never begins, commits, or scopes a
// transaction; callers must apply tenancy.WithTenant before using an app-role
// executor.
package surveystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/domains/survey"
	"github.com/monstercameron/hcm-next/internal/engines/messagetemplate"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type Executor interface {
	dbport.Execer
	dbport.Querier
}

type Error = survey.StoreError
type ErrorCode = survey.StoreCode

const (
	CodeInvalid   = survey.StoreCodeInvalid
	CodeDuplicate = survey.StoreCodeDuplicate
	CodeNotFound  = survey.StoreCodeNotFound
	CodeStaleCAS  = survey.StoreCodeStaleCAS
)

var (
	ErrInvalid   = survey.ErrInvalid
	ErrDuplicate = survey.ErrDuplicate
	ErrNotFound  = survey.ErrNotFound
	ErrStaleCAS  = survey.ErrStaleCAS
)

type Store struct{ ex Executor }

func New(ex Executor) Store { return Store{ex: ex} }

func (s Store) SaveQuestionBank(ctx context.Context, tenantRef string, value survey.QuestionBankRevision) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	questions, err := json.Marshal(value.Questions)
	if err != nil {
		return invalid("encode question bank questions")
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO survey_question_bank_revision
			(row_id, tenant_id, bank_id, revision, created_at, questions)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, value.QuestionBankID,
		int64(value.Revision), value.CreatedAt.Time(), string(questions))
	if err != nil {
		return fmt.Errorf("surveystore: save question bank: %w", err)
	}
	if affected == 0 {
		return duplicate("question bank revision already exists")
	}
	return nil
}

func (s Store) LoadQuestionBank(ctx context.Context, tenantRef, bankID string, revision uint64) (survey.QuestionBankRevision, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return survey.QuestionBankRevision{}, err
	}
	if strings.TrimSpace(bankID) == "" || revision == 0 {
		return survey.QuestionBankRevision{}, invalid("bank id and positive revision are required")
	}
	var stored survey.QuestionBankRevision
	var rev int64
	var created time.Time
	var questions string
	err = s.ex.QueryRow(ctx, `
		SELECT bank_id, revision, created_at, questions::text
		FROM survey_question_bank_revision
		WHERE tenant_id = $1 AND bank_id = $2 AND revision = $3`, tenantID, bankID, int64(revision)).Scan(
		&stored.QuestionBankID, &rev, &created, &questions)
	if err != nil {
		return survey.QuestionBankRevision{}, readError(err, "question bank revision does not exist")
	}
	if rev < 1 {
		return survey.QuestionBankRevision{}, invalid("stored question bank revision is invalid")
	}
	if err := json.Unmarshal([]byte(questions), &stored.Questions); err != nil {
		return survey.QuestionBankRevision{}, fmt.Errorf("surveystore: decode question bank: %w", err)
	}
	stored.Revision = uint64(rev)
	stored.CreatedAt = values.NewInstant(created.UTC())
	if err := stored.Validate(); err != nil {
		return survey.QuestionBankRevision{}, invalid("stored question bank failed domain validation")
	}
	return stored, nil
}

type surveyRow struct {
	ID, Title, Description, QuestionBankRef string
	Revision                                int64
	CreatedAt                               time.Time
}

func (s Store) SaveSurvey(ctx context.Context, tenantRef string, value survey.SurveyRevision) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO survey_revision
			(row_id, tenant_id, survey_id, revision, title, description, question_bank_ref, created_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, value.SurveyID, int64(value.Revision), value.Title,
		value.Description, value.QuestionBankRef.String(), value.CreatedAt.Time())
	if err != nil {
		return fmt.Errorf("surveystore: save survey: %w", err)
	}
	if affected == 0 {
		return duplicate("survey revision already exists")
	}
	return nil
}

func (s Store) LoadSurvey(ctx context.Context, tenantRef, surveyID string, revision uint64) (survey.SurveyRevision, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return survey.SurveyRevision{}, err
	}
	if strings.TrimSpace(surveyID) == "" || revision == 0 {
		return survey.SurveyRevision{}, invalid("survey id and positive revision are required")
	}
	var row surveyRow
	err = s.ex.QueryRow(ctx, `
		SELECT survey_id, revision, title, COALESCE(description, ''), question_bank_ref, created_at
		FROM survey_revision WHERE tenant_id = $1 AND survey_id = $2 AND revision = $3`, tenantID, surveyID, int64(revision)).Scan(
		&row.ID, &row.Revision, &row.Title, &row.Description, &row.QuestionBankRef, &row.CreatedAt)
	if err != nil {
		return survey.SurveyRevision{}, readError(err, "survey revision does not exist")
	}
	var ref values.EntityRef
	if err := ref.UnmarshalText([]byte(row.QuestionBankRef)); err != nil {
		return survey.SurveyRevision{}, invalid("stored question bank reference is invalid")
	}
	value := survey.SurveyRevision{SurveyID: row.ID, Revision: uint64(row.Revision), Title: row.Title, Description: row.Description, QuestionBankRef: ref, CreatedAt: values.NewInstant(row.CreatedAt.UTC())}
	if err := value.Validate(); err != nil {
		return survey.SurveyRevision{}, invalid("stored survey failed domain validation")
	}
	return value, nil
}

func (s Store) SaveCampaign(ctx context.Context, tenantRef string, value survey.CampaignRevision) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	surveyRef := value.SurveyRef.String()
	populationRef := value.PopulationSnapshotRef.String()
	channels := make([]string, len(value.ChannelRefs))
	for i, ref := range value.ChannelRefs {
		channels[i] = ref.String()
	}
	channelJSON, err := json.Marshal(channels)
	if err != nil {
		return invalid("encode campaign channel references")
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO survey_campaign_revision
			(row_id, tenant_id, campaign_id, revision, survey_ref, population_snapshot_ref,
			 window_start, window_end, anonymity_threshold, channel_refs, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, value.CampaignID, int64(value.Revision), surveyRef,
		populationRef, value.WindowStart.Time(), value.WindowEnd.Time(), value.AnonymityThreshold,
		string(channelJSON), value.CreatedAt.Time())
	if err != nil {
		return fmt.Errorf("surveystore: save campaign: %w", err)
	}
	if affected == 0 {
		return duplicate("campaign revision already exists")
	}
	return nil
}

func (s Store) LoadCampaign(ctx context.Context, tenantRef, campaignID string, revision uint64) (survey.CampaignRevision, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return survey.CampaignRevision{}, err
	}
	if strings.TrimSpace(campaignID) == "" || revision == 0 {
		return survey.CampaignRevision{}, invalid("campaign id and positive revision are required")
	}
	var (
		stored                   survey.CampaignRevision
		surveyRef, populationRef string
		channelJSON              string
	)
	var rev int64
	var start, end, created time.Time
	err = s.ex.QueryRow(ctx, `
		SELECT campaign_id, revision, survey_ref, COALESCE(population_snapshot_ref, ''),
			window_start, window_end, anonymity_threshold, COALESCE(channel_refs::text, '[]'), created_at
		FROM survey_campaign_revision
		WHERE tenant_id = $1 AND campaign_id = $2 AND revision = $3`, tenantID, campaignID, int64(revision)).Scan(
		&stored.CampaignID, &rev, &surveyRef, &populationRef, &start, &end, &stored.AnonymityThreshold, &channelJSON, &created)
	if err != nil {
		return survey.CampaignRevision{}, readError(err, "campaign revision does not exist")
	}
	stored.Revision = uint64(rev)
	if err := unmarshalRef(surveyRef, &stored.SurveyRef); err != nil {
		return survey.CampaignRevision{}, invalid("stored survey reference is invalid")
	}
	if err := unmarshalRef(populationRef, &stored.PopulationSnapshotRef); err != nil {
		return survey.CampaignRevision{}, invalid("stored population snapshot reference is invalid")
	}
	var channels []string
	if err := json.Unmarshal([]byte(channelJSON), &channels); err != nil {
		return survey.CampaignRevision{}, invalid("stored campaign channel references are invalid")
	}
	for _, encoded := range channels {
		var ref values.EntityRef
		if err := unmarshalRef(encoded, &ref); err != nil {
			return survey.CampaignRevision{}, invalid("stored campaign channel reference is invalid")
		}
		stored.ChannelRefs = append(stored.ChannelRefs, ref)
	}
	stored.WindowStart, stored.WindowEnd, stored.CreatedAt = values.NewInstant(start.UTC()), values.NewInstant(end.UTC()), values.NewInstant(created.UTC())
	if err := stored.Validate(); err != nil {
		return survey.CampaignRevision{}, invalid("stored campaign failed domain validation")
	}
	return stored, nil
}

type sampleBinding struct{ DefinitionID, RevisionVersion, Digest string }

func (s Store) SaveCampaignSample(ctx context.Context, tenantRef string, value survey.CampaignSample) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := validateSample(value); err != nil {
		return err
	}
	binding, err := json.Marshal(sampleBinding{value.Binding.DefinitionID, value.Binding.RevisionVersion, value.Binding.Digest})
	if err != nil {
		return invalid("encode sample binding")
	}
	rule, err := json.Marshal(value.Rule)
	if err != nil {
		return invalid("encode sampling rule")
	}
	members, err := json.Marshal(value.MemberIDs)
	if err != nil {
		return invalid("encode sample membership")
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO survey_campaign_sample
			(row_id, tenant_id, campaign_id, binding, rule, member_ids, membership_protected,
			 count, frozen_at, digest)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6::jsonb, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, value.CampaignID, string(binding), string(rule), string(members),
		value.MembershipProtected, value.Count, value.FrozenAt.Time(), storageDigest(value.Digest))
	if err != nil {
		return fmt.Errorf("surveystore: save campaign sample: %w", err)
	}
	if affected == 0 {
		return duplicate("campaign sample already exists")
	}
	return nil
}

func (s Store) LoadCampaignSample(ctx context.Context, tenantRef, campaignID string) (survey.CampaignSample, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return survey.CampaignSample{}, err
	}
	if strings.TrimSpace(campaignID) == "" {
		return survey.CampaignSample{}, invalid("campaign id is required")
	}
	var (
		value                              survey.CampaignSample
		bindingJSON, ruleJSON, membersJSON string
		protected                          bool
		frozen                             time.Time
	)
	err = s.ex.QueryRow(ctx, `
		SELECT campaign_id, binding::text, rule::text, member_ids::text, membership_protected,
			count, frozen_at, digest
		FROM survey_campaign_sample WHERE tenant_id = $1 AND campaign_id = $2`, tenantID, campaignID).Scan(
		&value.CampaignID, &bindingJSON, &ruleJSON, &membersJSON, &protected, &value.Count, &frozen, &value.Digest)
	if err != nil {
		return survey.CampaignSample{}, readError(err, "campaign sample does not exist")
	}
	var binding sampleBinding
	if err := json.Unmarshal([]byte(bindingJSON), &binding); err != nil {
		return survey.CampaignSample{}, invalid("stored sample binding is invalid")
	}
	if err := json.Unmarshal([]byte(ruleJSON), &value.Rule); err != nil {
		return survey.CampaignSample{}, invalid("stored sampling rule is invalid")
	}
	if err := json.Unmarshal([]byte(membersJSON), &value.MemberIDs); err != nil {
		return survey.CampaignSample{}, invalid("stored sample membership is invalid")
	}
	value.Binding = survey.PopulationBindingRef{DefinitionID: binding.DefinitionID, RevisionVersion: binding.RevisionVersion, Digest: binding.Digest}
	value.MembershipProtected, value.FrozenAt = protected, values.NewInstant(frozen.UTC())
	value.Digest = domainDigest(value.Digest)
	if err := validateSample(value); err != nil {
		return survey.CampaignSample{}, err
	}
	return value, nil
}

func (s Store) AppendLaunch(ctx context.Context, tenantRef string, value survey.LaunchRecord, sequence uint64) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := validLaunch(value, sequence); err != nil {
		return err
	}
	policy, err := json.Marshal(value.ReminderPolicy)
	if err != nil {
		return invalid("encode launch reminder policy")
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO survey_launch_record
			(row_id, tenant_id, campaign_id, campaign_revision, campaign_digest, purpose,
			 sample_digest, membership_protected, template_key, template_version, template_digest,
			 audience_digest, delivery_plan_digest, launched_by, approver, launched_at,
			 reminder_policy, digest, event_sequence)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::content_digest, NULLIF($6, ''),
			NULLIF($7, '')::content_digest, $8, NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''),
			NULLIF($12, '')::content_digest, NULLIF($13, '')::content_digest, NULLIF($14, ''),
			NULLIF($15, ''), $16, $17::jsonb, $18, $19)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, value.CampaignID, int64(value.CampaignRevision),
		storageDigest(value.CampaignDigest), string(value.Purpose), storageDigest(value.SampleDigest), value.MembershipProtected,
		value.TemplateKey, strconv.Itoa(value.TemplateVersion), value.TemplateDigest, storageDigest(value.AudienceDigest),
		storageDigest(value.DeliveryPlanDigest), value.LaunchedBy, value.Approver, value.LaunchedAt.Time(), string(policy), storageDigest(value.Digest), int64(sequence))
	if err != nil {
		return fmt.Errorf("surveystore: append launch: %w", err)
	}
	if affected == 0 {
		return duplicate("launch event sequence already exists")
	}
	return nil
}

func (s Store) ListLaunches(ctx context.Context, tenantRef, campaignID string) ([]survey.LaunchRecordEntry, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(campaignID) == "" {
		return nil, invalid("campaign id is required")
	}
	rows, err := s.ex.Query(ctx, `
		SELECT campaign_id, campaign_revision, COALESCE(campaign_digest::text, ''), COALESCE(purpose, ''),
			COALESCE(sample_digest::text, ''), membership_protected, COALESCE(template_key, ''),
			COALESCE(template_version, '0'), COALESCE(template_digest, ''), COALESCE(audience_digest::text, ''),
			COALESCE(delivery_plan_digest::text, ''), COALESCE(launched_by, ''), COALESCE(approver, ''),
			launched_at, COALESCE(reminder_policy::text, '{}'), COALESCE(digest::text, ''), event_sequence
		FROM survey_launch_record WHERE tenant_id = $1 AND campaign_id = $2 ORDER BY event_sequence`, tenantID, campaignID)
	if err != nil {
		return nil, fmt.Errorf("surveystore: list launches: %w", err)
	}
	defer rows.Close()
	var out []survey.LaunchRecordEntry
	for rows.Next() {
		var value survey.LaunchRecord
		var rev, sequence int64
		var purpose, policyJSON, versionText string
		var at time.Time
		if err := rows.Scan(&value.CampaignID, &rev, &value.CampaignDigest, &purpose, &value.SampleDigest, &value.MembershipProtected, &value.TemplateKey, &versionText, &value.TemplateDigest, &value.AudienceDigest, &value.DeliveryPlanDigest, &value.LaunchedBy, &value.Approver, &at, &policyJSON, &value.Digest, &sequence); err != nil {
			return nil, fmt.Errorf("surveystore: scan launch: %w", err)
		}
		version, err := strconv.Atoi(versionText)
		if err != nil {
			return nil, invalid("stored launch template version is invalid")
		}
		value.CampaignDigest = domainDigest(value.CampaignDigest)
		value.SampleDigest = domainDigest(value.SampleDigest)
		value.AudienceDigest = domainDigest(value.AudienceDigest)
		value.DeliveryPlanDigest = domainDigest(value.DeliveryPlanDigest)
		value.Digest = domainDigest(value.Digest)
		value.CampaignRevision, value.TemplateVersion, value.Purpose, value.LaunchedAt = uint64(rev), version, surveyPurpose(purpose), values.NewInstant(at.UTC())
		if err := json.Unmarshal([]byte(policyJSON), &value.ReminderPolicy); err != nil {
			return nil, invalid("stored launch reminder policy is invalid")
		}
		out = append(out, survey.LaunchRecordEntry{Sequence: uint64(sequence), Record: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("surveystore: list launches: %w", err)
	}
	return out, nil
}

func surveyPurpose(value string) messagetemplate.Purpose { return messagetemplate.Purpose(value) }

func (s Store) AppendResponse(ctx context.Context, tenantRef, campaignID string, campaignRevision uint64, respondingMemberRef string, value survey.ResponseRecord, sequence uint64) error {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return err
	}
	if err := validResponse(campaignID, campaignRevision, value, sequence); err != nil {
		return err
	}
	sample, err := s.LoadCampaignSample(ctx, tenantRef, campaignID)
	if err != nil {
		return err
	}
	if !sample.MembershipProtected {
		member := respondingMemberRef
		if member == "" {
			member = value.ResponseKey
		}
		found := false
		for _, id := range sample.MemberIDs {
			if id == member {
				found = true
				break
			}
		}
		if !found {
			return invalid("responding member is outside frozen sample")
		}
	}
	form, err := json.Marshal(value.FormDefinition)
	if err != nil {
		return invalid("encode response form definition")
	}
	answers, err := json.Marshal(value.Answers)
	if err != nil {
		return invalid("encode response answers")
	}
	affected, err := s.ex.Exec(ctx, `
		INSERT INTO survey_response_record
			(row_id, tenant_id, campaign_id, campaign_revision, form_definition, response_key,
			 response_revision, answers, validation_digest, submitted_at, digest, event_sequence)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8::jsonb, NULLIF($9, '')::content_digest,
			$10, $11, $12)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, campaignID, int64(campaignRevision), string(form), value.ResponseKey,
		value.ResponseRevision, string(answers), storageDigest(value.ValidationDigest), value.SubmittedAt.Time(), storageDigest(value.Digest), int64(sequence))
	if err != nil {
		return fmt.Errorf("surveystore: append response: %w", err)
	}
	if affected == 0 {
		return duplicate("response event sequence already exists")
	}
	return nil
}

func (s Store) ListResponses(ctx context.Context, tenantRef, campaignID string, campaignRevision uint64) ([]survey.ResponseRecordEntry, error) {
	tenantID, err := s.ready(tenantRef)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(campaignID) == "" || campaignRevision == 0 {
		return nil, invalid("campaign id and positive revision are required")
	}
	rows, err := s.ex.Query(ctx, `
		SELECT campaign_id, campaign_revision, form_definition::text, response_key, response_revision,
			answers::text, COALESCE(validation_digest::text, ''), submitted_at, digest::text, event_sequence
		FROM survey_response_record WHERE tenant_id = $1 AND campaign_id = $2 AND campaign_revision = $3
		ORDER BY event_sequence`, tenantID, campaignID, int64(campaignRevision))
	if err != nil {
		return nil, fmt.Errorf("surveystore: list responses: %w", err)
	}
	defer rows.Close()
	var out []survey.ResponseRecordEntry
	for rows.Next() {
		var value survey.ResponseRecord
		var formJSON, answersJSON string
		var rev, sequence int64
		var at time.Time
		if err := rows.Scan(&value.CampaignID, &rev, &formJSON, &value.ResponseKey, &value.ResponseRevision, &answersJSON, &value.ValidationDigest, &at, &value.Digest, &sequence); err != nil {
			return nil, fmt.Errorf("surveystore: scan response: %w", err)
		}
		if err := json.Unmarshal([]byte(formJSON), &value.FormDefinition); err != nil {
			return nil, invalid("stored response form definition is invalid")
		}
		if err := json.Unmarshal([]byte(answersJSON), &value.Answers); err != nil {
			return nil, invalid("stored response answers are invalid")
		}
		value.CampaignRevision, value.SubmittedAt = uint64(rev), values.NewInstant(at.UTC())
		value.Digest = domainDigest(value.Digest)
		if err := value.Verify(); err != nil {
			return nil, invalid("stored response failed digest validation")
		}
		out = append(out, survey.ResponseRecordEntry{Sequence: uint64(sequence), Record: value})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("surveystore: list responses: %w", err)
	}
	return out, nil
}

func (s Store) Aggregate(ctx context.Context, tenantRef string, campaign survey.ResponseCampaign) (survey.Aggregate, error) {
	entries, err := s.ListResponses(ctx, tenantRef, campaign.Campaign.CampaignID, campaign.Campaign.Revision)
	if err != nil {
		return survey.Aggregate{}, err
	}
	records := make([]survey.ResponseRecord, len(entries))
	for i := range entries {
		records[i] = entries[i].Record
	}
	return survey.AggregateResponses(campaign, records)
}

func validateSample(value survey.CampaignSample) error {
	want, err := survey.FreezeSample(value.CampaignID, value.Binding, value.Rule, value.MemberIDs, value.MembershipProtected, value.Count, value.FrozenAt)
	if err != nil || want.Digest != value.Digest {
		return invalid("campaign sample digest is invalid")
	}
	return nil
}

func validLaunch(value survey.LaunchRecord, sequence uint64) error {
	if value.CampaignID == "" || value.CampaignRevision == 0 || value.Digest == "" || sequence == 0 {
		return invalid("launch identity and positive sequence are required")
	}
	if err := value.LaunchedAt.Validate(); err != nil {
		return invalid("launch time is invalid")
	}
	return nil
}

func validResponse(campaignID string, campaignRevision uint64, value survey.ResponseRecord, sequence uint64) error {
	if campaignID == "" || campaignRevision == 0 || sequence == 0 || value.CampaignID != campaignID || value.CampaignRevision != campaignRevision {
		return invalid("response campaign identity and positive sequence are required")
	}
	if err := value.Verify(); err != nil {
		return err
	}
	return nil
}

func (s Store) ready(tenantRef string) (uuid.UUID, error) {
	if s.ex == nil {
		return uuid.Nil, invalid("store has no executor")
	}
	if strings.TrimSpace(tenantRef) == "" {
		return uuid.Nil, invalid("tenant id is required")
	}
	id, err := uuid.Parse(tenantRef)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid("tenant id must be a non-nil UUID")
	}
	return id, nil
}

func unmarshalRef(encoded string, target *values.EntityRef) error {
	if encoded == "" {
		return errors.New("empty reference")
	}
	return target.UnmarshalText([]byte(encoded))
}

func readError(err error, detail string) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return &survey.StoreError{Code: survey.StoreCodeNotFound, Detail: detail}
	}
	return fmt.Errorf("surveystore: %s: %w", detail, err)
}

func invalid(detail string) error {
	return &survey.StoreError{Code: survey.StoreCodeInvalid, Detail: detail}
}
func duplicate(detail string) error {
	return &survey.StoreError{Code: survey.StoreCodeDuplicate, Detail: detail}
}

// content_digest stores bare lowercase SHA-256 hex, while canonicalbytes
// returns the tagged domain spelling "sha256:<hex>".
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func CodeOf(err error) ErrorCode { return survey.CodeOf(err) }

var _ survey.Repository = Store{}
