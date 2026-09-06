// Package fixtures holds the shared P1A domain test corpus: a small worker
// population, a versioned pay-band catalog, and the compensation-change
// scenarios ported from the legacy TypeScript/Go implementation, together with
// in-memory implementations of the domain read ports.
//
// Semantic owner: domains (shared). Phase: P1A.
//
// It exists so that the People, Rewards and Promotion packages test against
// one corpus rather than three private ones that drift apart, and so that the
// legacy regression cases stay recognisably the legacy cases: the amounts,
// currencies, dates and business reasons in testdata are the values the
// retired implementation used, kept verbatim.
//
// The stores here are deterministic and in-memory. They read no clock and
// touch no network, which is what makes a digest computed over a fixture
// scenario reproducible.
package fixtures

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Tenant is the fixture tenant slug.
const Tenant values.TenantId = "harborcare-demo"

// MoneyScale and MoneyRounding are the declared money contract of the corpus.
const (
	MoneyScale                        = 2
	MoneyRounding values.RoundingMode = values.RoundingHalfEven
	PercentScale  int32               = 4
)

//go:embed testdata/workers.json
var workersJSON []byte

//go:embed testdata/bands.json
var bandsJSON []byte

//go:embed testdata/legacy-compensation-scenarios.json
var legacyScenariosJSON []byte

// workerFile is the on-disk shape of the worker corpus.
type workerFile struct {
	Calendar struct {
		Ref     string `json:"ref"`
		Version string `json:"version"`
	} `json:"calendar"`
	Workers []workerRecord `json:"workers"`
}

// workerRecord is one fixture worker.
type workerRecord struct {
	Key                    string `json:"key"`
	ID                     string `json:"id"`
	WorkerNumber           string `json:"worker_number"`
	LifecycleStatus        string `json:"lifecycle_status"`
	EmploymentStatus       string `json:"employment_status"`
	LegalName              string `json:"legal_name"`
	PreferredName          string `json:"preferred_name"`
	EmploymentID           string `json:"employment_id"`
	LegalEntity            string `json:"legal_entity"`
	WorkerType             string `json:"worker_type"`
	HireDate               string `json:"hire_date"`
	AssignmentID           string `json:"assignment_id"`
	JobCode                string `json:"job_code"`
	Grade                  string `json:"grade"`
	OrgUnit                string `json:"org_unit"`
	PositionID             string `json:"position_id"`
	Location               string `json:"location"`
	PayZone                string `json:"pay_zone"`
	FTE                    string `json:"fte"`
	ManagerRelationshipRef string `json:"manager_relationship_ref"`
	EffectiveFrom          string `json:"effective_from"`
	RevisionStream         string `json:"revision_stream"`
	RevisionSequence       uint64 `json:"revision_sequence"`
	KnownAt                string `json:"known_at"`
	RecordedAt             string `json:"recorded_at"`
	SourceSystem           string `json:"source_system"`
	AuthorityKind          string `json:"authority_kind"`
	AuthorityPolicy        string `json:"authority_policy"`
	EvidenceRef            string `json:"evidence_ref"`
}

// bandFile is the on-disk shape of the pay-band catalog.
type bandFile struct {
	CatalogVersion  string       `json:"catalog_version"`
	SourceSystem    string       `json:"source_system"`
	AuthorityPolicy string       `json:"authority_policy"`
	RecordedAt      string       `json:"recorded_at"`
	Bands           []bandRecord `json:"bands"`
}

// bandRecord is one fixture pay band.
type bandRecord struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	JobCode     string `json:"job_code"`
	Grade       string `json:"grade"`
	PayZone     string `json:"pay_zone"`
	Currency    string `json:"currency"`
	Minimum     string `json:"minimum"`
	Midpoint    string `json:"midpoint"`
	Maximum     string `json:"maximum"`
	Blocking    bool   `json:"blocking"`
	EvidenceRef string `json:"evidence_ref"`
}

// LegacyScenario is one ported compensation-change case.
type LegacyScenario struct {
	Name             string   `json:"name"`
	LegacyCase       string   `json:"legacy_case"`
	CurrentAmount    string   `json:"current_amount"`
	ProposedAmount   string   `json:"proposed_amount"`
	CurrentCurrency  string   `json:"current_currency"`
	ProposedCurrency string   `json:"proposed_currency"`
	BonusTarget      string   `json:"bonus_target_percent"`
	EffectiveAt      string   `json:"effective_at"`
	EvaluationAt     string   `json:"evaluation_at"`
	BusinessReason   string   `json:"business_reason"`
	ExpectStatus     string   `json:"expect_status"`
	ExpectCodes      []string `json:"expect_codes"`
	ForbidCodes      []string `json:"forbid_codes"`
}

// LegacyScenarioSet is the ported legacy corpus plus its shared context.
type LegacyScenarioSet struct {
	Source string `json:"source"`
	Note   string `json:"note"`
	Worker string `json:"worker"`
	Target struct {
		JobCode string `json:"job_code"`
		Grade   string `json:"grade"`
		PayZone string `json:"pay_zone"`
	} `json:"target"`
	BudgetAvailable string           `json:"budget_available"`
	Scenarios       []LegacyScenario `json:"scenarios"`
}

var (
	loadOnce  sync.Once
	loadErr   error
	workers   workerFile
	bands     bandFile
	scenarios LegacyScenarioSet
)

// load parses the embedded corpus exactly once.
func load() error {
	loadOnce.Do(func() {
		if err := json.Unmarshal(workersJSON, &workers); err != nil {
			loadErr = fmt.Errorf("fixtures: workers.json: %w", err)
			return
		}
		if err := json.Unmarshal(bandsJSON, &bands); err != nil {
			loadErr = fmt.Errorf("fixtures: bands.json: %w", err)
			return
		}
		if err := json.Unmarshal(legacyScenariosJSON, &scenarios); err != nil {
			loadErr = fmt.Errorf("fixtures: legacy-compensation-scenarios.json: %w", err)
		}
	})
	return loadErr
}

// Calendar returns the versioned business calendar the corpus is dated under.
func Calendar() (values.CalendarRef, error) {
	if err := load(); err != nil {
		return values.CalendarRef{}, err
	}
	return values.CalendarRef{Ref: workers.Calendar.Ref, Version: workers.Calendar.Version}, nil
}

// WorkerRef returns the entity reference of a fixture worker by key.
func WorkerRef(key string) (values.EntityRef, error) {
	if err := load(); err != nil {
		return values.EntityRef{}, err
	}
	for _, w := range workers.Workers {
		if w.Key == key {
			return values.EntityRef{Tenant: Tenant, Kind: people.KindWorker, Id: w.ID}, nil
		}
	}
	return values.EntityRef{}, fmt.Errorf("fixtures: no worker keyed %q", key)
}

// WorkerProfile is one corpus worker as a listing row: its stable key, its
// entity id and the placement fields a surface shows before it has run a
// governed read.
//
// It is deliberately not the whole record. Anything a caller has to be
// authorized to see -- and that is every field of a real worker read -- comes
// from people.ExplainWorkerState through the capability gateway, never from
// here. This type exists so a surface can offer "which worker?" as a choice,
// which is a different question from "what is true about this worker?".
type WorkerProfile struct {
	Key           string
	ID            string
	LegalName     string
	PreferredName string
	WorkerNumber  string
	JobCode       string
	Grade         string
	OrgUnit       string
	PositionID    string
	Location      string
	PayZone       string
	HireDate      string
}

// DisplayName is the name a surface addresses this worker by: the preferred
// name when there is one, otherwise the legal name.
func (w WorkerProfile) DisplayName() string {
	if w.PreferredName != "" {
		return w.PreferredName
	}
	return w.LegalName
}

// Workers returns every corpus worker as a listing row, in the corpus's own
// declared order. The order is the file's, not a sort: the corpus is a fixed
// population, and reordering it would change what "the first worker" means in
// every test that names one.
func Workers() ([]WorkerProfile, error) {
	if err := load(); err != nil {
		return nil, err
	}
	out := make([]WorkerProfile, 0, len(workers.Workers))
	for _, w := range workers.Workers {
		out = append(out, WorkerProfile{
			Key: w.Key, ID: w.ID,
			LegalName: w.LegalName, PreferredName: w.PreferredName, WorkerNumber: w.WorkerNumber,
			JobCode: w.JobCode, Grade: w.Grade, OrgUnit: w.OrgUnit, PositionID: w.PositionID,
			Location: w.Location, PayZone: w.PayZone, HireDate: w.HireDate,
		})
	}
	return out, nil
}

// BandScope is one pay band's placement scope plus the currency it is
// denominated in.
type BandScope struct {
	JobCode  string
	Grade    string
	PayZone  string
	Currency string
}

// BandScopes returns the scope of every band in the catalog, in catalog order.
//
// It exists so a surface that lets someone place a worker can offer exactly
// the placements the simulation can evaluate. A worker placed outside every
// band is a worker whose promotion the rewards engine can only answer
// "no band found" for, which is a refusal at simulation time for a mistake
// that was made at creation time -- and a mistake this table makes it possible
// to refuse at creation time instead.
func BandScopes() ([]BandScope, error) {
	if err := load(); err != nil {
		return nil, err
	}
	out := make([]BandScope, 0, len(bands.Bands))
	for _, b := range bands.Bands {
		out = append(out, BandScope{JobCode: b.JobCode, Grade: b.Grade, PayZone: b.PayZone, Currency: b.Currency})
	}
	return out, nil
}

// LegacyScenarios returns the ported legacy compensation corpus.
func LegacyScenarios() (LegacyScenarioSet, error) {
	if err := load(); err != nil {
		return LegacyScenarioSet{}, err
	}
	return scenarios, nil
}

// Money parses a fixture amount at the corpus money contract.
func Money(text, currency string) (values.Money, error) {
	return values.NewMoney(text, currency, MoneyScale, MoneyRounding)
}

// Percent parses a fixture bonus target expressed as a fraction.
func Percent(fraction string) (values.Percentage, error) {
	return values.NewPercentage(fraction, PercentScale, MoneyRounding)
}

// instant parses an RFC3339 fixture timestamp.
func instant(text string) (values.Instant, error) {
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return values.Instant{}, fmt.Errorf("fixtures: timestamp %q: %w", text, err)
	}
	return values.NewInstant(t), nil
}

// authorityKind maps the fixture token to the evidence vocabulary.
func authorityKind(token string) evidence.AuthorityKind {
	switch token {
	case "EXTERNAL_OBSERVATION":
		return evidence.AuthorityExternalObservation
	case "DERIVED":
		return evidence.AuthorityDerived
	default:
		return evidence.AuthorityLocal
	}
}

// fieldValues returns the fixture worker's field values keyed by FieldID.
func (w workerRecord) fieldValues() map[people.FieldID]string {
	return map[people.FieldID]string{
		people.FieldWorkerNumber:     w.WorkerNumber,
		people.FieldLifecycleStatus:  w.LifecycleStatus,
		people.FieldLegalName:        w.LegalName,
		people.FieldPreferredName:    w.PreferredName,
		people.FieldEmploymentID:     w.EmploymentID,
		people.FieldLegalEntity:      w.LegalEntity,
		people.FieldWorkerType:       w.WorkerType,
		people.FieldHireDate:         w.HireDate,
		people.FieldEmploymentStatus: w.EmploymentStatus,
		people.FieldAssignmentID:     w.AssignmentID,
		people.FieldJobCode:          w.JobCode,
		people.FieldGrade:            w.Grade,
		people.FieldOrgUnit:          w.OrgUnit,
		people.FieldPositionID:       w.PositionID,
		people.FieldLocation:         w.Location,
		people.FieldPayZone:          w.PayZone,
		people.FieldFTE:              w.FTE,
		people.FieldManagerRelation:  w.ManagerRelationshipRef,
	}
}

// MemoryWorkerFacts is an in-memory people.WorkerFacts implementation over the
// fixture corpus. It answers exactly the projection it is asked for and
// nothing wider, which is what lets a test prove that ExplainWorkerState never
// loads a field the caller was denied.
type MemoryWorkerFacts struct {
	records  map[string]workerRecord
	calendar values.CalendarRef
	// Missing names fields the store deliberately has no assertion for, so an
	// authorized-but-unasserted field can be exercised.
	Missing map[people.FieldID]bool
}

// NewMemoryWorkerFacts builds the in-memory reader over the fixture corpus.
func NewMemoryWorkerFacts() (*MemoryWorkerFacts, error) {
	if err := load(); err != nil {
		return nil, err
	}
	cal, err := Calendar()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]workerRecord, len(workers.Workers))
	for _, w := range workers.Workers {
		byID[w.ID] = w
	}
	return &MemoryWorkerFacts{records: byID, calendar: cal, Missing: map[people.FieldID]bool{}}, nil
}

// WorkerFactsAt implements people.WorkerFacts.
func (m *MemoryWorkerFacts) WorkerFactsAt(_ context.Context, q people.FactQuery) (people.FactSet, error) {
	if err := q.Validate(); err != nil {
		return people.FactSet{}, err
	}
	record, ok := m.records[q.Worker.Id]
	if !ok {
		return people.FactSet{Worker: q.Worker, Exists: false}, nil
	}

	revision, err := values.NewSequenceRevision(record.RevisionStream, record.RevisionSequence)
	if err != nil {
		return people.FactSet{}, err
	}
	effectiveFrom, err := values.ParseLocalDate(record.EffectiveFrom)
	if err != nil {
		return people.FactSet{}, err
	}
	interval, err := values.NewOpenLocalDateInterval(effectiveFrom, m.calendar)
	if err != nil {
		return people.FactSet{}, err
	}
	knownInstant, err := instant(record.KnownAt)
	if err != nil {
		return people.FactSet{}, err
	}
	knownAt, err := values.NewKnownAt(knownInstant)
	if err != nil {
		return people.FactSet{}, err
	}
	recordedInstant, err := instant(record.RecordedAt)
	if err != nil {
		return people.FactSet{}, err
	}
	recordedAt, err := values.NewRecordedAt(recordedInstant)
	if err != nil {
		return people.FactSet{}, err
	}

	authority := evidence.SourceAuthority{
		Kind:      authorityKind(record.AuthorityKind),
		System:    record.SourceSystem,
		PolicyRef: record.AuthorityPolicy,
	}
	provenance := evidence.Provenance{
		Source:      record.SourceSystem,
		EvidenceRef: record.EvidenceRef,
		RecordedAt:  recordedAt,
	}

	all := record.fieldValues()
	facts := make([]people.Fact, 0, len(q.Fields))
	for _, field := range q.Fields {
		if m.Missing[field] {
			continue
		}
		raw, present := all[field]
		value := values.Value(raw)
		if !present || raw == "" {
			value = values.Absent[string]()
		}
		facts = append(facts, people.Fact{
			Field:      field,
			Value:      value,
			Effective:  interval,
			KnownAt:    knownAt,
			Revision:   revision,
			Authority:  authority,
			Provenance: provenance,
		})
	}
	return people.FactSet{
		Worker:    q.Worker,
		Exists:    true,
		Facts:     facts,
		Watermark: revision,
	}, nil
}

// MemoryBandCatalog is an in-memory rewards.PayBandCatalog over the fixture
// catalog.
type MemoryBandCatalog struct {
	bands           []bandRecord
	catalogVersion  string
	sourceSystem    string
	authorityPolicy string
	recordedAt      values.RecordedAt
	// Fail, when set, is returned instead of a lookup result. It exists so a
	// test can prove that a catalog fault is an error rather than a silent
	// "no band".
	Fail error
}

// NewMemoryBandCatalog builds the in-memory catalog over the fixture corpus.
func NewMemoryBandCatalog() (*MemoryBandCatalog, error) {
	if err := load(); err != nil {
		return nil, err
	}
	at, err := instant(bands.RecordedAt)
	if err != nil {
		return nil, err
	}
	recordedAt, err := values.NewRecordedAt(at)
	if err != nil {
		return nil, err
	}
	return &MemoryBandCatalog{
		bands:           bands.Bands,
		catalogVersion:  bands.CatalogVersion,
		sourceSystem:    bands.SourceSystem,
		authorityPolicy: bands.AuthorityPolicy,
		recordedAt:      recordedAt,
	}, nil
}

// CatalogVersion returns the pinned catalog version.
func (c *MemoryBandCatalog) CatalogVersion() string { return c.catalogVersion }

// LookupBand implements rewards.PayBandCatalog.
func (c *MemoryBandCatalog) LookupBand(_ context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if c.Fail != nil {
		return rewards.BandRecord{}, c.Fail
	}
	if err := q.Validate(); err != nil {
		return rewards.BandRecord{}, err
	}
	for _, b := range c.bands {
		if b.JobCode != q.JobCode || b.Grade != q.Grade || b.PayZone != q.PayZone || b.Currency != q.Currency {
			continue
		}
		band, err := toBand(b)
		if err != nil {
			return rewards.BandRecord{}, err
		}
		return rewards.BandRecord{
			Band:           band,
			CatalogVersion: c.catalogVersion,
			Blocking:       b.Blocking,
			Authority: evidence.SourceAuthority{
				Kind:      evidence.AuthorityLocal,
				System:    c.sourceSystem,
				PolicyRef: c.authorityPolicy,
			},
			Provenance: evidence.Provenance{
				Source:      c.sourceSystem,
				EvidenceRef: b.EvidenceRef,
				RecordedAt:  c.recordedAt,
			},
		}, nil
	}
	return rewards.BandRecord{}, fmt.Errorf("%w: %s/%s/%s in %s",
		rewards.ErrBandNotFound, q.JobCode, q.Grade, q.PayZone, q.Currency)
}

// toBand converts a fixture record into an engine band.
func toBand(b bandRecord) (payband.Band, error) {
	minimum, err := Money(b.Minimum, b.Currency)
	if err != nil {
		return payband.Band{}, err
	}
	midpoint, err := Money(b.Midpoint, b.Currency)
	if err != nil {
		return payband.Band{}, err
	}
	maximum, err := Money(b.Maximum, b.Currency)
	if err != nil {
		return payband.Band{}, err
	}
	return payband.Band{
		ID:       b.ID,
		Version:  b.Version,
		Scope:    payband.Scope{JobCode: b.JobCode, Grade: b.Grade, PayZone: b.PayZone},
		Minimum:  minimum,
		Midpoint: midpoint,
		Maximum:  maximum,
	}, nil
}

// Band returns one fixture band by id, for tests that exercise the pure engine
// without a catalog.
func Band(id string) (payband.Band, error) {
	if err := load(); err != nil {
		return payband.Band{}, err
	}
	for _, b := range bands.Bands {
		if b.ID == id {
			return toBand(b)
		}
	}
	return payband.Band{}, fmt.Errorf("fixtures: no band %q", id)
}

// AllowAll returns an authorization decision that permits the given fields and
// discloses the subject. It is the "nothing is being tested about AuthZ here"
// decision; tests that exercise denial build their own.
func AllowAll(policyVersion, purpose string, fields []people.FieldID) people.AuthorizationDecision {
	rulings := make(map[people.FieldID]people.FieldRuling, len(fields))
	for _, f := range fields {
		rulings[f] = people.FieldRuling{Effect: people.EffectAllow}
	}
	return people.AuthorizationDecision{
		PolicyVersion:      policyVersion,
		Purpose:            purpose,
		SubjectDisclosable: true,
		Fields:             rulings,
	}
}

// DenyFields returns the same decision with the named fields denied.
func DenyFields(d people.AuthorizationDecision, reason string, denied ...people.FieldID) people.AuthorizationDecision {
	rulings := make(map[people.FieldID]people.FieldRuling, len(d.Fields))
	maps.Copy(rulings, d.Fields)
	for _, f := range denied {
		rulings[f] = people.FieldRuling{Effect: people.EffectDeny, Reason: reason}
	}
	d.Fields = rulings
	return d
}

// WithheldSubject returns a decision that refuses to confirm the subject at all.
func WithheldSubject(d people.AuthorizationDecision, reason string) people.AuthorizationDecision {
	d.SubjectDisclosable = false
	d.SubjectDenialReason = reason
	return d
}
