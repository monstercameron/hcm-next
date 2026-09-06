// Package employeerelationsstore persists the six employee-relations
// revision families from migration 00088.
package employeerelationsstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/employeerelations"
)

// DB is the transaction capability needed by the adapter.
type DB interface{ dbport.Beginner }

// Store implements the tenant-aware employee-relations repository.
type Store struct{ db DB }

var _ employeerelations.Repository = (*Store)(nil)

// ScopedStore is a compatibility adapter for the domain's original
// persistence-free Store port. New composition roots should prefer Store and
// its explicit tenant/context methods; ScopedStore is useful where an older
// caller already binds one tenant at construction time.
type ScopedStore struct {
	store  *Store
	tenant string
}

var _ employeerelations.Store = (*ScopedStore)(nil)

// New returns a PostgreSQL-backed employee-relations store.
func New(db DB) *Store { return &Store{db: db} }

// NewScoped returns the legacy Store-port adapter bound to tenantID.
func NewScoped(db DB, tenantID string) *ScopedStore {
	return &ScopedStore{store: New(db), tenant: tenantID}
}

func (s *ScopedStore) SaveAllegation(r employeerelations.AllegationRevision) error {
	return s.store.SaveAllegation(context.Background(), s.tenant, r, r.ParentRevision)
}
func (s *ScopedStore) GetAllegation(id string, revision uint64) (employeerelations.AllegationRevision, bool) {
	value, err := s.store.LoadAllegation(context.Background(), s.tenant, id, revision)
	return value, err == nil
}
func (s *ScopedStore) SaveInvestigation(r employeerelations.InvestigationRevision) error {
	return s.store.SaveInvestigation(context.Background(), s.tenant, r, r.ParentRevision)
}
func (s *ScopedStore) GetInvestigation(id string, revision uint64) (employeerelations.InvestigationRevision, bool) {
	value, err := s.store.LoadInvestigation(context.Background(), s.tenant, id, revision)
	return value, err == nil
}
func (s *ScopedStore) SaveInterview(r employeerelations.InterviewRevision) error {
	return s.store.SaveInterview(context.Background(), s.tenant, r, r.ParentRevision)
}
func (s *ScopedStore) GetInterview(id string, revision uint64) (employeerelations.InterviewRevision, bool) {
	value, err := s.store.LoadInterview(context.Background(), s.tenant, id, revision)
	return value, err == nil
}
func (s *ScopedStore) SaveFinding(r employeerelations.FindingRevision) error {
	return s.store.SaveFinding(context.Background(), s.tenant, r, r.ParentRevision)
}
func (s *ScopedStore) GetFinding(id string, revision uint64) (employeerelations.FindingRevision, bool) {
	value, err := s.store.LoadFinding(context.Background(), s.tenant, id, revision)
	return value, err == nil
}
func (s *ScopedStore) SaveDiscipline(r employeerelations.DisciplineRevision) error {
	return s.store.SaveDiscipline(context.Background(), s.tenant, r, r.ParentRevision)
}
func (s *ScopedStore) GetDiscipline(id string, revision uint64) (employeerelations.DisciplineRevision, bool) {
	value, err := s.store.LoadDiscipline(context.Background(), s.tenant, id, revision)
	return value, err == nil
}
func (s *ScopedStore) SaveGrievance(r employeerelations.GrievanceRevision) error {
	return s.store.SaveGrievance(context.Background(), s.tenant, r, r.ParentRevision)
}
func (s *ScopedStore) GetGrievance(id string, revision uint64) (employeerelations.GrievanceRevision, bool) {
	value, err := s.store.LoadGrievance(context.Background(), s.tenant, id, revision)
	return value, err == nil
}

func invalid(detail string) error {
	return &employeerelations.StoreError{Code: employeerelations.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &employeerelations.StoreError{Code: employeerelations.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &employeerelations.StoreError{Code: employeerelations.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &employeerelations.StoreError{Code: employeerelations.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func parseTenant(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid("tenant id must be a non-nil UUID")
	}
	return id, nil
}

func parseRef(field, value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid(field + " must be a non-nil UUID")
	}
	return id, nil
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("employeerelationsstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("employeerelationsstore: commit transaction: %w", err)
	}
	return nil
}

type envelope struct {
	tenant, rowID, caseRef, compartmentRef, id uuid.UUID
	revision                                   uint64
	parentRevision                             any
	parentDigest                               any
	participants                               []byte
	digest                                     string
}

func makeEnvelope(tenant string, id, caseRef, compartmentRef string, revision, parent uint64, parentDigest, digest string, participants any) (envelope, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return envelope{}, err
	}
	rowID, err := parseRef("id", id)
	if err != nil {
		return envelope{}, err
	}
	caseID, err := parseRef("case_ref", caseRef)
	if err != nil {
		return envelope{}, err
	}
	compartmentID, err := parseRef("compartment_ref", compartmentRef)
	if err != nil {
		return envelope{}, err
	}
	participantsJSON, err := json.Marshal(participants)
	if err != nil {
		return envelope{}, invalid("participants cannot be encoded")
	}
	out := envelope{
		tenant: tenantID, rowID: uuid.New(), caseRef: caseID, compartmentRef: compartmentID,
		id: rowID, revision: revision, participants: participantsJSON, digest: storageDigest(digest),
	}
	if parent != 0 {
		out.parentRevision = int64(parent)
		out.parentDigest = storageDigest(parentDigest)
	}
	return out, nil
}

type insertFunc func(context.Context, dbport.Tx, envelope) error
type relationCheck func(context.Context, dbport.Tx, envelope) error

func (s *Store) save(ctx context.Context, e envelope, table, idColumn string, expected uint64, insert insertFunc, relation relationCheck) error {
	return s.withTenant(ctx, e.tenant, func(tx dbport.Tx) error {
		lockKey := e.tenant.String() + ":" + table + ":" + e.id.String() + ":" + e.caseRef.String()
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return fmt.Errorf("employeerelationsstore: lock %s: %w", table, err)
		}
		var latest int64
		query := fmt.Sprintf("SELECT COALESCE(MAX(revision),0) FROM %s WHERE tenant_id=$1 AND case_ref=$2 AND %s=$3", table, idColumn)
		if err := tx.QueryRow(ctx, query, e.tenant, e.caseRef, e.id).Scan(&latest); err != nil {
			return fmt.Errorf("employeerelationsstore: current %s revision: %w", table, err)
		}
		if latest > 0 && e.revision <= uint64(latest) {
			return duplicate(fmt.Sprintf("%s %s revision %d", table, e.id, e.revision))
		}
		if expected != uint64(latest) || e.revision != uint64(latest)+1 {
			return stale(expected, uint64(latest), fmt.Sprintf("%s %s current revision is %d", table, e.id, latest))
		}
		if relation != nil {
			if err := relation(ctx, tx, e); err != nil {
				return err
			}
		}
		if err := insert(ctx, tx, e); err != nil {
			if isUniqueViolation(err) {
				return duplicate(fmt.Sprintf("%s %s revision %d", table, e.id, e.revision))
			}
			return fmt.Errorf("employeerelationsstore: insert %s %s: %w", table, e.id, err)
		}
		return nil
	})
}

func insertCommon(table, idColumn string, columns string, values func(envelope) []any) insertFunc {
	return func(ctx context.Context, tx dbport.Tx, e envelope) error {
		extraCount := strings.Count(columns, ",") + 1
		canonicalPlaceholder := fmt.Sprintf("$%d", 10+extraCount)
		query := fmt.Sprintf(`INSERT INTO %s (
            tenant_id, row_id, case_ref, compartment_ref, %s, revision,
            parent_revision, parent_digest, participants, %s, canonical_digest)
            VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,%s,%s)`, table, idColumn, columns, placeholders(extraCount), canonicalPlaceholder)
		args := []any{e.tenant, e.rowID, e.caseRef, e.compartmentRef, e.id, int64(e.revision), e.parentRevision, e.parentDigest, e.participants}
		args = append(args, values(e)...)
		args = append(args, e.digest)
		_, err := tx.Exec(ctx, query, args...)
		return err
	}
}

func placeholders(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = fmt.Sprintf("$%d", i+10)
	}
	return strings.Join(parts, ",")
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint")
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }
func domainDigest(value string) string {
	if value == "" {
		return ""
	}
	return "sha256:" + storageDigest(value)
}

func (s *Store) SaveAllegation(ctx context.Context, tenant string, record employeerelations.AllegationRevision, expected uint64) error {
	normalized, err := employeerelations.NewAllegationRevision(record)
	if err != nil {
		return invalid(err.Error())
	}
	if _, err := parseRef("reporter_ref", normalized.ReporterRef); err != nil {
		return err
	}
	if _, err := parseRef("subject_ref", normalized.SubjectRef); err != nil {
		return err
	}
	e, err := makeEnvelope(tenant, normalized.ID, normalized.CaseRef, normalized.CompartmentRef, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest, normalized.Participants)
	if err != nil {
		return err
	}
	return s.save(ctx, e, "er_allegation_revision", "allegation_id", expected,
		insertCommon("er_allegation_revision", "allegation_id", "reporter_ref, subject_ref, summary, status, retaliation_safeguard", func(e envelope) []any {
			reporter, _ := uuid.Parse(normalized.ReporterRef)
			subject, _ := uuid.Parse(normalized.SubjectRef)
			return []any{reporter, subject, normalized.Summary, string(normalized.Status), normalized.RetaliationSafeguard}
		}), nil)
}

func (s *Store) SaveInvestigation(ctx context.Context, tenant string, record employeerelations.InvestigationRevision, expected uint64) error {
	normalized, err := employeerelations.NewInvestigationRevision(record)
	if err != nil {
		return invalid(err.Error())
	}
	for field, value := range map[string]string{"allegation_ref": normalized.AllegationRef, "investigator_ref": normalized.InvestigatorRef, "authority_ref": normalized.AuthorityRef} {
		if _, err := parseRef(field, value); err != nil {
			return err
		}
	}
	e, err := makeEnvelope(tenant, normalized.ID, normalized.CaseRef, normalized.CompartmentRef, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest, normalized.Participants)
	if err != nil {
		return err
	}
	return s.save(ctx, e, "er_investigation_revision", "investigation_id", expected,
		insertCommon("er_investigation_revision", "investigation_id", "allegation_ref, investigator_ref, authority_ref, purpose, scope, status", func(e envelope) []any {
			allegation, _ := uuid.Parse(normalized.AllegationRef)
			investigator, _ := uuid.Parse(normalized.InvestigatorRef)
			authority, _ := uuid.Parse(normalized.AuthorityRef)
			return []any{allegation, investigator, authority, nullableText(normalized.Purpose), nullableText(normalized.Scope), string(normalized.Status)}
		}), nil)
}

func (s *Store) SaveInterview(ctx context.Context, tenant string, record employeerelations.InterviewRevision, expected uint64) error {
	normalized, err := employeerelations.NewInterviewRevision(record)
	if err != nil {
		return invalid(err.Error())
	}
	for field, value := range map[string]string{"investigation_ref": normalized.InvestigationRef, "interviewer_ref": normalized.InterviewerRef, "interviewee_ref": normalized.IntervieweeRef} {
		if _, err := parseRef(field, value); err != nil {
			return err
		}
	}
	e, err := makeEnvelope(tenant, normalized.ID, normalized.CaseRef, normalized.CompartmentRef, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest, normalized.Participants)
	if err != nil {
		return err
	}
	return s.save(ctx, e, "er_interview_revision", "interview_id", expected,
		insertCommon("er_interview_revision", "interview_id", "investigation_ref, interviewer_ref, interviewee_ref, statement, status", func(e envelope) []any {
			investigation, _ := uuid.Parse(normalized.InvestigationRef)
			interviewer, _ := uuid.Parse(normalized.InterviewerRef)
			interviewee, _ := uuid.Parse(normalized.IntervieweeRef)
			return []any{investigation, interviewer, interviewee, nullableText(normalized.Statement), string(normalized.Status)}
		}), nil)
}

func (s *Store) SaveFinding(ctx context.Context, tenant string, record employeerelations.FindingRevision, expected uint64) error {
	normalized, err := employeerelations.NewFindingRevision(record)
	if err != nil {
		return invalid(err.Error())
	}
	for field, value := range map[string]string{"investigation_ref": normalized.InvestigationRef, "investigator_ref": normalized.InvestigatorRef, "subject_ref": normalized.SubjectRef, "reporter_ref": normalized.ReporterRef} {
		if _, err := parseRef(field, value); err != nil {
			return err
		}
	}
	e, err := makeEnvelope(tenant, normalized.ID, normalized.CaseRef, normalized.CompartmentRef, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest, normalized.Participants)
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(normalized.EvidenceRefs)
	if err != nil {
		return invalid("evidence_refs cannot be encoded")
	}
	return s.save(ctx, e, "er_finding_revision", "finding_id", expected,
		insertCommon("er_finding_revision", "finding_id", "investigation_ref, investigator_ref, subject_ref, reporter_ref, evidence_standard, evidence_refs, disposition, rationale", func(e envelope) []any {
			investigation, _ := uuid.Parse(normalized.InvestigationRef)
			investigator, _ := uuid.Parse(normalized.InvestigatorRef)
			subject, _ := uuid.Parse(normalized.SubjectRef)
			reporter, _ := uuid.Parse(normalized.ReporterRef)
			return []any{investigation, investigator, subject, reporter, string(normalized.EvidenceStandard), evidence, string(normalized.Disposition), nullableText(normalized.Rationale)}
		}), nil)
}

func (s *Store) SaveDiscipline(ctx context.Context, tenant string, record employeerelations.DisciplineRevision, expected uint64) error {
	normalized, err := employeerelations.NewDisciplineRevision(record)
	if err != nil {
		return invalid(err.Error())
	}
	for field, value := range map[string]string{"finding_ref": normalized.FindingRef, "subject_ref": normalized.SubjectRef, "legal_review_ref": normalized.LegalReviewRef, "representation_review_ref": normalized.RepresentationReviewRef} {
		if _, err := parseRef(field, value); err != nil {
			return err
		}
	}
	e, err := makeEnvelope(tenant, normalized.ID, normalized.CaseRef, normalized.CompartmentRef, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest, normalized.Participants)
	if err != nil {
		return err
	}
	return s.save(ctx, e, "er_discipline_revision", "discipline_id", expected,
		insertCommon("er_discipline_revision", "discipline_id", "finding_ref, subject_ref, action, legal_review_ref, representation_review_ref, status", func(e envelope) []any {
			finding, _ := uuid.Parse(normalized.FindingRef)
			subject, _ := uuid.Parse(normalized.SubjectRef)
			legal, _ := uuid.Parse(normalized.LegalReviewRef)
			representation, _ := uuid.Parse(normalized.RepresentationReviewRef)
			return []any{finding, subject, normalized.Action, legal, representation, string(normalized.Status)}
		}), sameCaseReference("er_finding_revision", "finding_id", normalized.FindingRef, "discipline"))
}

func (s *Store) SaveGrievance(ctx context.Context, tenant string, record employeerelations.GrievanceRevision, expected uint64) error {
	normalized, err := employeerelations.NewGrievanceRevision(record)
	if err != nil {
		return invalid(err.Error())
	}
	for field, value := range map[string]string{"decision_ref": normalized.DecisionRef, "grievant_ref": normalized.GrievantRef} {
		if _, err := parseRef(field, value); err != nil {
			return err
		}
	}
	e, err := makeEnvelope(tenant, normalized.ID, normalized.CaseRef, normalized.CompartmentRef, normalized.Revision, normalized.ParentRevision, normalized.ParentDigest, normalized.CanonicalDigest, normalized.Participants)
	if err != nil {
		return err
	}
	return s.save(ctx, e, "er_grievance_revision", "grievance_id", expected,
		insertCommon("er_grievance_revision", "grievance_id", "decision_ref, grievant_ref, grounds, outcome, status", func(e envelope) []any {
			decision, _ := uuid.Parse(normalized.DecisionRef)
			grievant, _ := uuid.Parse(normalized.GrievantRef)
			return []any{decision, grievant, nullableText(normalized.Grounds), nullableText(normalized.Outcome), string(normalized.Status)}
		}), sameCaseReference("er_discipline_revision", "discipline_id", normalized.DecisionRef, "decision"))
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func sameCaseReference(table, identityColumn, reference, kind string) relationCheck {
	return func(ctx context.Context, tx dbport.Tx, e envelope) error {
		var sameCase, anywhere bool
		referenceID, err := uuid.Parse(reference)
		if err != nil || referenceID == uuid.Nil {
			return invalid(kind + " reference must be a non-nil UUID")
		}
		query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE tenant_id=$1 AND case_ref=$2 AND %s=$3),
            EXISTS (SELECT 1 FROM %s WHERE tenant_id=$1 AND %s=$3)`, table, identityColumn, table, identityColumn)
		if err := tx.QueryRow(ctx, query, e.tenant, e.caseRef, referenceID).Scan(&sameCase, &anywhere); err != nil {
			return fmt.Errorf("employeerelationsstore: check %s reference: %w", kind, err)
		}
		if anywhere && !sameCase {
			return invalid(fmt.Sprintf("%s reference belongs to another case", kind))
		}
		return nil
	}
}

type loadedCommon struct {
	caseRef, compartmentRef, id uuid.UUID
	revision                    int64
	parentRevision              *int64
	parentDigest                *string
	participants                []byte
	digest                      string
}

func scanCommon(row dbport.Row, extra ...any) (loadedCommon, error) {
	var out loadedCommon
	dests := []any{&out.caseRef, &out.compartmentRef, &out.id, &out.revision, &out.parentRevision, &out.parentDigest, &out.participants}
	dests = append(dests, extra...)
	dests = append(dests, &out.digest)
	if err := row.Scan(dests...); err != nil {
		return loadedCommon{}, err
	}
	return out, nil
}

func (s *Store) LoadAllegation(ctx context.Context, tenant, id string, revision uint64) (employeerelations.AllegationRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return employeerelations.AllegationRevision{}, err
	}
	idValue, err := parseRef("id", id)
	if err != nil {
		return employeerelations.AllegationRevision{}, err
	}
	var out employeerelations.AllegationRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var reporter, subject uuid.UUID
		var summary, status string
		var safeguard bool
		common, scanErr := scanCommon(tx.QueryRow(ctx, `SELECT case_ref, compartment_ref, allegation_id, revision, parent_revision, parent_digest, participants, reporter_ref, subject_ref, summary, status, retaliation_safeguard, canonical_digest FROM er_allegation_revision WHERE tenant_id=$1 AND allegation_id=$2 AND revision=$3`, tenantID, idValue, int64(revision)), &reporter, &subject, &summary, &status, &safeguard)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("allegation %s revision %d", id, revision))
		}
		if scanErr != nil {
			return fmt.Errorf("employeerelationsstore: load allegation: %w", scanErr)
		}
		if err := decodeCommon(common, &out.ID, &out.CaseRef, &out.CompartmentRef, &out.Revision, &out.ParentRevision, &out.ParentDigest, &out.Participants, &out.CanonicalDigest); err != nil {
			return err
		}
		out.ReporterRef, out.SubjectRef, out.Summary, out.Status, out.RetaliationSafeguard = reporter.String(), subject.String(), summary, employeerelations.AllegationStatus(status), safeguard
		if err := out.Validate(); err != nil {
			return fmt.Errorf("employeerelationsstore: stored allegation failed validation: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) LoadInvestigation(ctx context.Context, tenant, id string, revision uint64) (employeerelations.InvestigationRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return employeerelations.InvestigationRevision{}, err
	}
	idValue, err := parseRef("id", id)
	if err != nil {
		return employeerelations.InvestigationRevision{}, err
	}
	var out employeerelations.InvestigationRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var allegation, investigator, authority uuid.UUID
		var purpose, scope, status *string
		common, scanErr := scanCommon(tx.QueryRow(ctx, `SELECT case_ref, compartment_ref, investigation_id, revision, parent_revision, parent_digest, participants, allegation_ref, investigator_ref, authority_ref, purpose, scope, status, canonical_digest FROM er_investigation_revision WHERE tenant_id=$1 AND investigation_id=$2 AND revision=$3`, tenantID, idValue, int64(revision)), &allegation, &investigator, &authority, &purpose, &scope, &status)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("investigation %s revision %d", id, revision))
		}
		if scanErr != nil {
			return fmt.Errorf("employeerelationsstore: load investigation: %w", scanErr)
		}
		if err := decodeCommon(common, &out.ID, &out.CaseRef, &out.CompartmentRef, &out.Revision, &out.ParentRevision, &out.ParentDigest, &out.Participants, &out.CanonicalDigest); err != nil {
			return err
		}
		out.AllegationRef, out.InvestigatorRef, out.AuthorityRef = allegation.String(), investigator.String(), authority.String()
		out.Purpose, out.Scope, out.Status = deref(purpose), deref(scope), employeerelations.InvestigationStatus(deref(status))
		if err := out.Validate(); err != nil {
			return fmt.Errorf("employeerelationsstore: stored investigation failed validation: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) LoadInterview(ctx context.Context, tenant, id string, revision uint64) (employeerelations.InterviewRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return employeerelations.InterviewRevision{}, err
	}
	idValue, err := parseRef("id", id)
	if err != nil {
		return employeerelations.InterviewRevision{}, err
	}
	var out employeerelations.InterviewRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var investigation, interviewer, interviewee uuid.UUID
		var statement, status *string
		common, scanErr := scanCommon(tx.QueryRow(ctx, `SELECT case_ref, compartment_ref, interview_id, revision, parent_revision, parent_digest, participants, investigation_ref, interviewer_ref, interviewee_ref, statement, status, canonical_digest FROM er_interview_revision WHERE tenant_id=$1 AND interview_id=$2 AND revision=$3`, tenantID, idValue, int64(revision)), &investigation, &interviewer, &interviewee, &statement, &status)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("interview %s revision %d", id, revision))
		}
		if scanErr != nil {
			return fmt.Errorf("employeerelationsstore: load interview: %w", scanErr)
		}
		if err := decodeCommon(common, &out.ID, &out.CaseRef, &out.CompartmentRef, &out.Revision, &out.ParentRevision, &out.ParentDigest, &out.Participants, &out.CanonicalDigest); err != nil {
			return err
		}
		out.InvestigationRef, out.InterviewerRef, out.IntervieweeRef = investigation.String(), interviewer.String(), interviewee.String()
		out.Statement, out.Status = deref(statement), employeerelations.InterviewStatus(deref(status))
		if err := out.Validate(); err != nil {
			return fmt.Errorf("employeerelationsstore: stored interview failed validation: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) LoadFinding(ctx context.Context, tenant, id string, revision uint64) (employeerelations.FindingRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return employeerelations.FindingRevision{}, err
	}
	idValue, err := parseRef("id", id)
	if err != nil {
		return employeerelations.FindingRevision{}, err
	}
	var out employeerelations.FindingRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var investigation, investigator, subject, reporter uuid.UUID
		var evidenceStandard *string
		var evidenceRefs []byte
		var disposition, rationale *string
		common, scanErr := scanCommon(tx.QueryRow(ctx, `SELECT case_ref, compartment_ref, finding_id, revision, parent_revision, parent_digest, participants, investigation_ref, investigator_ref, subject_ref, reporter_ref, evidence_standard, evidence_refs, disposition, rationale, canonical_digest FROM er_finding_revision WHERE tenant_id=$1 AND finding_id=$2 AND revision=$3`, tenantID, idValue, int64(revision)), &investigation, &investigator, &subject, &reporter, &evidenceStandard, &evidenceRefs, &disposition, &rationale)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("finding %s revision %d", id, revision))
		}
		if scanErr != nil {
			return fmt.Errorf("employeerelationsstore: load finding: %w", scanErr)
		}
		if err := decodeCommon(common, &out.ID, &out.CaseRef, &out.CompartmentRef, &out.Revision, &out.ParentRevision, &out.ParentDigest, &out.Participants, &out.CanonicalDigest); err != nil {
			return err
		}
		if err := json.Unmarshal(evidenceRefs, &out.EvidenceRefs); err != nil {
			return invalid("stored evidence_refs is invalid JSON")
		}
		out.InvestigationRef, out.InvestigatorRef, out.SubjectRef, out.ReporterRef = investigation.String(), investigator.String(), subject.String(), reporter.String()
		out.EvidenceStandard, out.Disposition, out.Rationale = employeerelations.EvidenceStandard(deref(evidenceStandard)), employeerelations.FindingDisposition(deref(disposition)), deref(rationale)
		if err := out.Validate(); err != nil {
			return fmt.Errorf("employeerelationsstore: stored finding failed validation: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) LoadDiscipline(ctx context.Context, tenant, id string, revision uint64) (employeerelations.DisciplineRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return employeerelations.DisciplineRevision{}, err
	}
	idValue, err := parseRef("id", id)
	if err != nil {
		return employeerelations.DisciplineRevision{}, err
	}
	var out employeerelations.DisciplineRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var finding, subject, legal, representation uuid.UUID
		var action, status string
		common, scanErr := scanCommon(tx.QueryRow(ctx, `SELECT case_ref, compartment_ref, discipline_id, revision, parent_revision, parent_digest, participants, finding_ref, subject_ref, action, legal_review_ref, representation_review_ref, status, canonical_digest FROM er_discipline_revision WHERE tenant_id=$1 AND discipline_id=$2 AND revision=$3`, tenantID, idValue, int64(revision)), &finding, &subject, &action, &legal, &representation, &status)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("discipline %s revision %d", id, revision))
		}
		if scanErr != nil {
			return fmt.Errorf("employeerelationsstore: load discipline: %w", scanErr)
		}
		if err := decodeCommon(common, &out.ID, &out.CaseRef, &out.CompartmentRef, &out.Revision, &out.ParentRevision, &out.ParentDigest, &out.Participants, &out.CanonicalDigest); err != nil {
			return err
		}
		out.FindingRef, out.SubjectRef, out.Action, out.LegalReviewRef, out.RepresentationReviewRef, out.Status = finding.String(), subject.String(), action, legal.String(), representation.String(), employeerelations.DisciplineStatus(status)
		if err := out.Validate(); err != nil {
			return fmt.Errorf("employeerelationsstore: stored discipline failed validation: %w", err)
		}
		return nil
	})
	return out, err
}

func (s *Store) LoadGrievance(ctx context.Context, tenant, id string, revision uint64) (employeerelations.GrievanceRevision, error) {
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return employeerelations.GrievanceRevision{}, err
	}
	idValue, err := parseRef("id", id)
	if err != nil {
		return employeerelations.GrievanceRevision{}, err
	}
	var out employeerelations.GrievanceRevision
	err = s.withTenant(ctx, tenantID, func(tx dbport.Tx) error {
		var decision, grievant uuid.UUID
		var grounds, outcome, status *string
		common, scanErr := scanCommon(tx.QueryRow(ctx, `SELECT case_ref, compartment_ref, grievance_id, revision, parent_revision, parent_digest, participants, decision_ref, grievant_ref, grounds, outcome, status, canonical_digest FROM er_grievance_revision WHERE tenant_id=$1 AND grievance_id=$2 AND revision=$3`, tenantID, idValue, int64(revision)), &decision, &grievant, &grounds, &outcome, &status)
		if errors.Is(scanErr, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("grievance %s revision %d", id, revision))
		}
		if scanErr != nil {
			return fmt.Errorf("employeerelationsstore: load grievance: %w", scanErr)
		}
		if err := decodeCommon(common, &out.ID, &out.CaseRef, &out.CompartmentRef, &out.Revision, &out.ParentRevision, &out.ParentDigest, &out.Participants, &out.CanonicalDigest); err != nil {
			return err
		}
		out.DecisionRef, out.GrievantRef, out.Grounds, out.Outcome, out.Status = decision.String(), grievant.String(), deref(grounds), deref(outcome), employeerelations.GrievanceStatus(deref(status))
		if err := out.Validate(); err != nil {
			return fmt.Errorf("employeerelationsstore: stored grievance failed validation: %w", err)
		}
		return nil
	})
	return out, err
}

func decodeCommon(in loadedCommon, id, caseRef, compartmentRef *string, revision, parentRevision *uint64, parentDigest *string, participants any, digest *string) error {
	*id, *caseRef, *compartmentRef = in.id.String(), in.caseRef.String(), in.compartmentRef.String()
	if in.revision <= 0 {
		return invalid("stored revision is not positive")
	}
	*revision = uint64(in.revision)
	if in.parentRevision != nil {
		*parentRevision = uint64(*in.parentRevision)
	}
	if in.parentDigest != nil {
		*parentDigest = domainDigest(*in.parentDigest)
	}
	if err := json.Unmarshal(in.participants, participants); err != nil {
		return invalid("stored participants is invalid JSON")
	}
	*digest = domainDigest(in.digest)
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
