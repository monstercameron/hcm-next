package survey

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// StoreCode is the stable classification of a survey persistence result.
type StoreCode string

const (
	StoreCodeInvalid   StoreCode = "INVALID"
	StoreCodeDuplicate StoreCode = "DUPLICATE"
	StoreCodeNotFound  StoreCode = "NOT_FOUND"
	StoreCodeStaleCAS  StoreCode = "STALE_CAS"
)

// StoreError is a typed persistence error shared by the in-memory and SQL
// implementations of Repository.
type StoreError struct {
	Code   StoreCode
	Detail string
}

func (e *StoreError) Error() string {
	if e == nil {
		return "survey: persistence error"
	}
	if e.Detail == "" {
		return "survey: " + string(e.Code)
	}
	return fmt.Sprintf("survey: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Is(target error) bool {
	t, ok := target.(*StoreError)
	return ok && e != nil && t != nil && e.Code == t.Code
}

var (
	ErrInvalid   = &StoreError{Code: StoreCodeInvalid}
	ErrDuplicate = &StoreError{Code: StoreCodeDuplicate}
	ErrNotFound  = &StoreError{Code: StoreCodeNotFound}
	ErrStaleCAS  = &StoreError{Code: StoreCodeStaleCAS}
)

// CodeOf returns the stable persistence code carried by err.
func CodeOf(err error) StoreCode {
	var typed *StoreError
	if errors.As(err, &typed) && typed != nil {
		return typed.Code
	}
	return ""
}

// LaunchRecordEntry and ResponseRecordEntry retain the storage sequence while
// keeping the domain records free of database ordering metadata.
type LaunchRecordEntry struct {
	Sequence uint64
	Record   LaunchRecord
}

type ResponseRecordEntry struct {
	Sequence uint64
	Record   ResponseRecord
}

// Repository is the tenant-scoped persistence port for the survey domain.
// Implementations receive the tenant identity separately because it is a
// database boundary, not part of the tenant-neutral domain values.
type Repository interface {
	SaveQuestionBank(ctx context.Context, tenantID string, value QuestionBankRevision) error
	LoadQuestionBank(ctx context.Context, tenantID, bankID string, revision uint64) (QuestionBankRevision, error)
	SaveSurvey(ctx context.Context, tenantID string, value SurveyRevision) error
	LoadSurvey(ctx context.Context, tenantID, surveyID string, revision uint64) (SurveyRevision, error)
	SaveCampaign(ctx context.Context, tenantID string, value CampaignRevision) error
	LoadCampaign(ctx context.Context, tenantID, campaignID string, revision uint64) (CampaignRevision, error)
	SaveCampaignSample(ctx context.Context, tenantID string, value CampaignSample) error
	LoadCampaignSample(ctx context.Context, tenantID, campaignID string) (CampaignSample, error)
	AppendLaunch(ctx context.Context, tenantID string, value LaunchRecord, sequence uint64) error
	ListLaunches(ctx context.Context, tenantID, campaignID string) ([]LaunchRecordEntry, error)
	AppendResponse(ctx context.Context, tenantID, campaignID string, campaignRevision uint64, respondingMemberRef string, value ResponseRecord, sequence uint64) error
	ListResponses(ctx context.Context, tenantID, campaignID string, campaignRevision uint64) ([]ResponseRecordEntry, error)
	Aggregate(ctx context.Context, tenantID string, campaign ResponseCampaign) (Aggregate, error)
}

// AggregateResponses applies the domain's cohort suppression rule to a set of
// append-only records. It is exported so a durable adapter can keep the read
// model's threshold guard in the domain rather than in a SQL view.
func AggregateResponses(campaign ResponseCampaign, records []ResponseRecord) (Aggregate, error) {
	if err := campaign.Validate(); err != nil {
		return Aggregate{}, err
	}
	cohort := aggregateCohort(campaign, records)
	if len(cohort) < campaign.MinimumCohort() {
		return Aggregate{}, ErrAggregateCohortTooSmall
	}
	breakdown := make(map[string]map[string]int)
	for _, record := range cohort {
		for _, answer := range record.Answers {
			if breakdown[answer.QuestionID] == nil {
				breakdown[answer.QuestionID] = make(map[string]int)
			}
			breakdown[answer.QuestionID][answer.Value]++
		}
	}
	result := Aggregate{
		CampaignID:       campaign.Campaign.CampaignID,
		CampaignRevision: campaign.Campaign.Revision,
		FormDefinition:   campaign.FormDefinition,
		Count:            len(cohort),
		Breakdown:        breakdown,
	}
	result.Digest = aggregateDigest(result)
	return result, nil
}

// MemoryStore is the kernel-pure Repository implementation used by callers
// that do not need PostgreSQL. Its write rules mirror the durable adapter.
type MemoryStore struct {
	mu            sync.RWMutex
	questionBanks map[string]QuestionBankRevision
	surveys       map[string]SurveyRevision
	campaigns     map[string]CampaignRevision
	samples       map[string]CampaignSample
	launches      map[string][]LaunchRecordEntry
	responses     map[string][]ResponseRecordEntry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		questionBanks: make(map[string]QuestionBankRevision),
		surveys:       make(map[string]SurveyRevision), campaigns: make(map[string]CampaignRevision),
		samples: make(map[string]CampaignSample), launches: make(map[string][]LaunchRecordEntry),
		responses: make(map[string][]ResponseRecordEntry),
	}
}

func tenantKey(tenantID, id string, revision uint64) string {
	return tenantID + "\x00" + id + "\x00" + fmt.Sprint(revision)
}
func campaignKey(tenantID, id string, revision uint64) string {
	return tenantKey(tenantID, id, revision)
}
func sampleKey(tenantID, id string) string { return tenantID + "\x00" + id }

func validTenant(tenantID string) error {
	if tenantID == "" {
		return &StoreError{Code: StoreCodeInvalid, Detail: "tenant id is required"}
	}
	return nil
}

func copyQuestions(in []Question) []Question {
	out := append([]Question(nil), in...)
	for i := range out {
		out[i].Choices = append([]string(nil), out[i].Choices...)
	}
	return out
}

func copySample(in CampaignSample) CampaignSample {
	in.MemberIDs = append([]string(nil), in.MemberIDs...)
	in.Rule.Strata = append([]StratumQuota(nil), in.Rule.Strata...)
	return in
}

func copyResponse(in ResponseRecord) ResponseRecord {
	in.Answers = append([]ResponseAnswer(nil), in.Answers...)
	return in
}

func (s *MemoryStore) SaveQuestionBank(_ context.Context, tenantID string, value QuestionBankRevision) error {
	if s == nil {
		return &StoreError{Code: StoreCodeInvalid, Detail: "store is nil"}
	}
	if err := validTenant(tenantID); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	k := tenantKey(tenantID, value.QuestionBankID, value.Revision)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.questionBanks[k]; ok {
		return &StoreError{Code: StoreCodeDuplicate, Detail: "question bank revision already exists"}
	}
	value.Questions = copyQuestions(value.Questions)
	s.questionBanks[k] = value
	return nil
}

func (s *MemoryStore) LoadQuestionBank(_ context.Context, tenantID, bankID string, revision uint64) (QuestionBankRevision, error) {
	if s == nil {
		return QuestionBankRevision{}, ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return QuestionBankRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.questionBanks[tenantKey(tenantID, bankID, revision)]
	if !ok {
		return QuestionBankRevision{}, &StoreError{Code: StoreCodeNotFound, Detail: "question bank revision does not exist"}
	}
	value.Questions = copyQuestions(value.Questions)
	return value, nil
}

func (s *MemoryStore) SaveSurvey(_ context.Context, tenantID string, value SurveyRevision) error {
	if s == nil {
		return ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	k := tenantKey(tenantID, value.SurveyID, value.Revision)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.surveys[k]; ok {
		return &StoreError{Code: StoreCodeDuplicate, Detail: "survey revision already exists"}
	}
	s.surveys[k] = value
	return nil
}

func (s *MemoryStore) LoadSurvey(_ context.Context, tenantID, surveyID string, revision uint64) (SurveyRevision, error) {
	if s == nil {
		return SurveyRevision{}, ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return SurveyRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.surveys[tenantKey(tenantID, surveyID, revision)]
	if !ok {
		return SurveyRevision{}, &StoreError{Code: StoreCodeNotFound, Detail: "survey revision does not exist"}
	}
	return value, nil
}

func (s *MemoryStore) SaveCampaign(_ context.Context, tenantID string, value CampaignRevision) error {
	if s == nil {
		return ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	k := campaignKey(tenantID, value.CampaignID, value.Revision)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.campaigns[k]; ok {
		return &StoreError{Code: StoreCodeDuplicate, Detail: "campaign revision already exists"}
	}
	s.campaigns[k] = value
	return nil
}

func (s *MemoryStore) LoadCampaign(_ context.Context, tenantID, campaignID string, revision uint64) (CampaignRevision, error) {
	if s == nil {
		return CampaignRevision{}, ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return CampaignRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.campaigns[campaignKey(tenantID, campaignID, revision)]
	if !ok {
		return CampaignRevision{}, &StoreError{Code: StoreCodeNotFound, Detail: "campaign revision does not exist"}
	}
	return value, nil
}

func validSample(value CampaignSample) error {
	if value.CampaignID == "" {
		return &StoreError{Code: StoreCodeInvalid, Detail: "campaign id is required"}
	}
	want, err := FreezeSample(value.CampaignID, value.Binding, value.Rule, value.MemberIDs, value.MembershipProtected, value.Count, value.FrozenAt)
	if err != nil || want.Digest != value.Digest {
		return &StoreError{Code: StoreCodeInvalid, Detail: "campaign sample digest is invalid"}
	}
	return nil
}

func (s *MemoryStore) SaveCampaignSample(_ context.Context, tenantID string, value CampaignSample) error {
	if s == nil {
		return ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return err
	}
	if err := validSample(value); err != nil {
		return err
	}
	k := sampleKey(tenantID, value.CampaignID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.samples[k]; ok {
		return &StoreError{Code: StoreCodeDuplicate, Detail: "campaign sample already exists"}
	}
	s.samples[k] = copySample(value)
	return nil
}

func (s *MemoryStore) LoadCampaignSample(_ context.Context, tenantID, campaignID string) (CampaignSample, error) {
	if s == nil {
		return CampaignSample{}, ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return CampaignSample{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.samples[sampleKey(tenantID, campaignID)]
	if !ok {
		return CampaignSample{}, &StoreError{Code: StoreCodeNotFound, Detail: "campaign sample does not exist"}
	}
	return copySample(value), nil
}

func (s *MemoryStore) AppendLaunch(_ context.Context, tenantID string, value LaunchRecord, sequence uint64) error {
	if s == nil {
		return ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return err
	}
	if value.CampaignID == "" || value.CampaignRevision == 0 || value.Digest == "" || sequence == 0 {
		return &StoreError{Code: StoreCodeInvalid, Detail: "launch identity and positive sequence are required"}
	}
	k := sampleKey(tenantID, value.CampaignID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, prior := range s.launches[k] {
		if prior.Sequence == sequence {
			return &StoreError{Code: StoreCodeDuplicate, Detail: "launch event sequence already exists"}
		}
	}
	s.launches[k] = append(s.launches[k], LaunchRecordEntry{Sequence: sequence, Record: value})
	return nil
}

func (s *MemoryStore) ListLaunches(_ context.Context, tenantID, campaignID string) ([]LaunchRecordEntry, error) {
	if s == nil {
		return nil, ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]LaunchRecordEntry(nil), s.launches[sampleKey(tenantID, campaignID)]...)
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}

func (s *MemoryStore) AppendResponse(_ context.Context, tenantID, campaignID string, campaignRevision uint64, respondingMemberRef string, value ResponseRecord, sequence uint64) error {
	if s == nil {
		return ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return err
	}
	if campaignID == "" || campaignRevision == 0 || sequence == 0 || value.CampaignID != campaignID || value.CampaignRevision != campaignRevision || value.ResponseKey == "" || value.Digest == "" {
		return &StoreError{Code: StoreCodeInvalid, Detail: "response identity and positive sequence are required"}
	}
	if err := value.Verify(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sample, ok := s.samples[sampleKey(tenantID, campaignID)]
	if !ok {
		return &StoreError{Code: StoreCodeNotFound, Detail: "campaign sample does not exist"}
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
			return &StoreError{Code: StoreCodeInvalid, Detail: "responding member is outside frozen sample"}
		}
	}
	k := sampleKey(tenantID, campaignID)
	for _, prior := range s.responses[k] {
		if prior.Sequence == sequence && prior.Record.ResponseKey == value.ResponseKey {
			return &StoreError{Code: StoreCodeDuplicate, Detail: "response event sequence already exists"}
		}
	}
	s.responses[k] = append(s.responses[k], ResponseRecordEntry{Sequence: sequence, Record: copyResponse(value)})
	return nil
}

func (s *MemoryStore) ListResponses(_ context.Context, tenantID, campaignID string, campaignRevision uint64) ([]ResponseRecordEntry, error) {
	if s == nil {
		return nil, ErrInvalid
	}
	if err := validTenant(tenantID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ResponseRecordEntry
	for _, entry := range s.responses[sampleKey(tenantID, campaignID)] {
		if entry.Record.CampaignRevision == campaignRevision {
			entry.Record = copyResponse(entry.Record)
			out = append(out, entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, nil
}

func (s *MemoryStore) Aggregate(ctx context.Context, tenantID string, campaign ResponseCampaign) (Aggregate, error) {
	entries, err := s.ListResponses(ctx, tenantID, campaign.Campaign.CampaignID, campaign.Campaign.Revision)
	if err != nil {
		return Aggregate{}, err
	}
	records := make([]ResponseRecord, len(entries))
	for i := range entries {
		records[i] = entries[i].Record
	}
	return AggregateResponses(campaign, records)
}

var _ Repository = (*MemoryStore)(nil)
