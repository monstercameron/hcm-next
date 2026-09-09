// Package careerstore persists the canonical career profile revision families
// from migration 00074. It owns no business decisions: validation and meaning
// remain in internal/domains/career, while this adapter supplies durability,
// tenant scoping and append-only compare-and-swap.
package careerstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/career"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction-opening capability used by Store.
type DB interface{ dbport.Beginner }

// Store is the PostgreSQL implementation of career.Store.
type Store struct{ db DB }

var _ career.Store = (*Store)(nil)

func New(db DB) *Store { return &Store{db: db} }

func invalid(detail string) error   { return career.NewStoreError(career.StoreInvalidCode, detail) }
func notFound(detail string) error  { return career.NewStoreError(career.StoreNotFoundCode, detail) }
func duplicate(detail string) error { return career.NewStoreError(career.StoreDuplicateCode, detail) }

func stale(expected, actual values.RevisionToken) *career.StoreError {
	err := career.NewStoreError(career.StoreStaleCASCode, fmt.Sprintf("expected %s, actual %s", expected.String(), actual.String()))
	err.Expected, err.Actual = expected.String(), actual.String()
	return err
}

func (s *Store) withTenant(ctx context.Context, tenant string, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	tenantID, err := uuid.Parse(tenant)
	if err != nil || tenantID == uuid.Nil {
		return invalid("tenant id must be a non-nil UUID")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("careerstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("careerstore: commit transaction: %w", err)
	}
	return nil
}

type intervalMeta struct {
	Kind            string `json:"kind"`
	CalendarRef     string `json:"calendar_ref,omitempty"`
	CalendarVersion string `json:"calendar_version,omitempty"`
}

type revisionMeta struct {
	RevisionStream   string       `json:"revision_stream"`
	Revision         uint64       `json:"revision"`
	SupersedesStream string       `json:"supersedes_stream,omitempty"`
	Supersedes       uint64       `json:"supersedes,omitempty"`
	Interval         intervalMeta `json:"interval"`
}

type requirementWire struct {
	SkillRef     string `json:"skill_ref"`
	MinimumLevel int    `json:"minimum_level"`
}

type targetWire struct {
	revisionMeta
	JobProfileRevisionStream string            `json:"job_profile_revision_stream"`
	Requirements             []requirementWire `json:"requirements"`
}

type objectiveWire struct {
	revisionMeta
	Owner              string   `json:"owner"`
	State              string   `json:"state"`
	CompletionEvidence []string `json:"completion_evidence_refs"`
}

type assessmentWire struct {
	revisionMeta
	Source string `json:"source"`
}

func revisionWire(revision, supersedes values.RevisionToken, interval values.EffectiveInterval) (revisionMeta, error) {
	sequence, ok := revision.Sequence()
	if !ok || sequence == 0 {
		return revisionMeta{}, invalid("revision must be a positive sequence token")
	}
	if err := revision.Validate(); err != nil {
		return revisionMeta{}, invalid(err.Error())
	}
	meta := revisionMeta{RevisionStream: revision.Stream(), Revision: sequence}
	if supersedes.IsSpecified() {
		previous, previousOK := supersedes.Sequence()
		if !previousOK || previous == 0 {
			return revisionMeta{}, invalid("supersedes must be a positive sequence token")
		}
		meta.SupersedesStream, meta.Supersedes = supersedes.Stream(), previous
	}
	if err := interval.Validate(); err != nil {
		return revisionMeta{}, invalid(fmt.Sprintf("effective interval: %v", err))
	}
	meta.Interval.Kind = interval.Kind().String()
	if interval.Kind() == values.IntervalKindLocalDate {
		meta.Interval.CalendarRef = interval.Calendar().Ref
		meta.Interval.CalendarVersion = interval.Calendar().Version
	}
	return meta, nil
}

func intervalBounds(interval values.EffectiveInterval) (time.Time, *time.Time, error) {
	if err := interval.Validate(); err != nil {
		return time.Time{}, nil, err
	}
	if start, ok := interval.StartInstant(); ok {
		from := start.Time().UTC()
		if end, has := interval.EndInstant(); has {
			to := end.Time().UTC()
			return from, &to, nil
		}
		return from, nil, nil
	}
	start, _ := interval.StartDate()
	from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	if end, has := interval.EndDate(); has {
		to := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
		return from, &to, nil
	}
	return from, nil, nil
}

func intervalFromBounds(from, to *time.Time, meta intervalMeta) (values.EffectiveInterval, error) {
	if from == nil {
		return values.EffectiveInterval{}, invalid("stored effective_from is required")
	}
	fromValue := from.UTC()
	var toValue *time.Time
	if to != nil {
		value := to.UTC()
		toValue = &value
	}
	if meta.Kind == values.IntervalKindInstant.String() {
		start, err := values.NewInstantFromUnix(fromValue.Unix(), int32(fromValue.Nanosecond()))
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		if toValue == nil {
			return values.NewOpenInstantInterval(start)
		}
		end, err := values.NewInstantFromUnix(toValue.Unix(), int32(toValue.Nanosecond()))
		if err != nil {
			return values.EffectiveInterval{}, err
		}
		return values.NewInstantInterval(start, end)
	}
	start, err := values.NewLocalDate(fromValue.Year(), fromValue.Month(), fromValue.Day())
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendar := values.CalendarRef{Ref: meta.CalendarRef, Version: meta.CalendarVersion}
	if toValue == nil {
		return values.NewOpenLocalDateInterval(start, calendar)
	}
	end, err := values.NewLocalDate(toValue.Year(), toValue.Month(), toValue.Day())
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	return values.NewLocalDateInterval(start, end, calendar)
}

func marshalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("careerstore: encode json: %w", err)
	}
	return encoded, nil
}

func refStrings(refs []values.EntityRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ref.String())
	}
	return out
}

func parseRefs(raw []byte, field string) ([]values.EntityRef, error) {
	var encoded []string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, invalid(fmt.Sprintf("%s is not a reference array: %v", field, err))
	}
	out := make([]values.EntityRef, 0, len(encoded))
	for _, item := range encoded {
		var ref values.EntityRef
		if err := ref.UnmarshalText([]byte(item)); err != nil {
			return nil, invalid(fmt.Sprintf("%s contains invalid reference: %v", field, err))
		}
		out = append(out, ref)
	}
	return out, nil
}

func uuidRef(ref values.EntityRef, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(ref.Id)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("%s id must be a non-nil UUID", field))
	}
	return id, nil
}

func storedDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }
func domainDigest(value string) string {
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func assessmentDigest(value career.CareerAssessmentRevision) (string, error) {
	body, err := json.Marshal(struct {
		AssessmentID string                      `json:"assessment_id"`
		Revision     values.RevisionToken        `json:"revision"`
		Worker       values.EntityRef            `json:"worker"`
		TargetRole   values.EntityRef            `json:"target_role"`
		Source       values.EntityRef            `json:"source"`
		Epistemic    career.EpistemicClass       `json:"epistemic"`
		Visibility   career.PreferenceVisibility `json:"visibility"`
		Summary      string                      `json:"summary"`
		Effective    values.EffectiveInterval    `json:"effective"`
	}{value.AssessmentID.String(), value.Revision, value.Worker, value.TargetRole, value.Source, value.Epistemic, value.Visibility, value.Summary, value.Effective})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) SavePreference(ctx context.Context, tenant string, value career.CareerPreferenceProfileRevision, expected values.RevisionToken) error {
	if value.PreferenceID.Tenant != values.TenantId(tenant) || value.Worker.Tenant != values.TenantId(tenant) {
		return invalid("preference references must use the storage tenant")
	}
	if value.CanonicalDigest == "" {
		var err error
		value, err = career.NewCareerPreference(value)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := value.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		worker, err := uuidRef(value.Worker, "worker")
		if err != nil {
			return err
		}
		from, to, err := intervalBounds(value.Timeframe)
		if err != nil {
			return invalid(err.Error())
		}
		meta, err := revisionWire(value.Revision, value.Supersedes, value.Timeframe)
		if err != nil {
			return err
		}
		roles, err := marshalJSON(refStrings(value.TargetRoleRefs))
		if err != nil {
			return err
		}
		timing, err := marshalJSON(meta)
		if err != nil {
			return err
		}
		if err := checkCAS(ctx, tx, tenantID, "career_preference_revision", "preference_id", value.PreferenceID.Id, meta, expected); err != nil {
			return err
		}
		preferenceID, err := uuidRef(value.PreferenceID, "preference")
		if err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `INSERT INTO career_preference_revision (
			row_id, tenant_id, preference_id, revision, supersedes, worker_ref,
			roles, locations, work_arrangements, mobility_preference,
			 timing_constraints, visibility, effective_from, effective_to, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, preferenceID, sequenceValue(value.Revision), nullableSequence(value.Supersedes), worker,
			roles, []byte(`[]`), []byte(`[]`), string(value.Mobility), timing, string(value.Visibility), from, to, storedDigest(value.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("careerstore: insert preference revision: %w", err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("preference %s revision %d", value.PreferenceID.Id, sequenceValue(value.Revision)))
		}
		return nil
	})
}

func (s *Store) LoadPreference(ctx context.Context, tenant, id string, revision values.RevisionToken) (career.CareerPreferenceProfileRevision, error) {
	var out career.CareerPreferenceProfileRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		sequence, err := sequenceNumber(revision)
		if err != nil {
			return err
		}
		var storedPreferenceID, workerID uuid.UUID
		var roles, timing []byte
		var storedRevision int64
		var supersedes *int64
		var from, to *time.Time
		var visibility, mobility, digest string
		err = tx.QueryRow(ctx, `SELECT preference_id, revision, supersedes, worker_ref, roles,
			mobility_preference, timing_constraints, visibility, effective_from, effective_to, canonical_digest
			FROM career_preference_revision WHERE tenant_id=$1 AND preference_id=$2 AND revision=$3`, tenantID, parseID(id), sequence).Scan(
			&storedPreferenceID, &storedRevision, &supersedes, &workerID, &roles, &mobility, &timing, &visibility, &from, &to, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("preference %s revision %d", id, sequence))
			}
			return fmt.Errorf("careerstore: load preference revision: %w", err)
		}
		_ = storedPreferenceID
		var meta revisionMeta
		if err := json.Unmarshal(timing, &meta); err != nil {
			return invalid(fmt.Sprintf("preference metadata: %v", err))
		}
		out.PreferenceID = entityRef(tenant, "career_preference", id)
		out.Worker = entityRef(tenant, "worker", workerID.String())
		out.Revision, err = makeRevision(meta.RevisionStream, uint64(storedRevision))
		if err != nil {
			return err
		}
		out.Supersedes, err = makeOptionalRevision(meta.SupersedesStream, meta.Supersedes)
		if err != nil {
			return err
		}
		out.TargetRoleRefs, err = parseRefs(roles, "roles")
		if err != nil {
			return err
		}
		out.Mobility = career.MobilityWillingness(mobility)
		out.Visibility = career.PreferenceVisibility(visibility)
		out.Timeframe, err = intervalFromBounds(from, to, meta.Interval)
		if err != nil {
			return err
		}
		out.CanonicalDigest = domainDigest(digest)
		if err := out.Validate(); err != nil {
			return invalid(fmt.Sprintf("stored preference failed validation: %v", err))
		}
		return nil
	})
	return out, err
}

func (s *Store) CurrentPreference(ctx context.Context, tenant, id string) (career.CareerPreferenceProfileRevision, error) {
	return s.loadCurrentPreference(ctx, tenant, id)
}

func (s *Store) loadCurrentPreference(ctx context.Context, tenant, id string) (career.CareerPreferenceProfileRevision, error) {
	var revision values.RevisionToken
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var sequence int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM career_preference_revision WHERE tenant_id=$1 AND preference_id=$2 ORDER BY revision DESC LIMIT 1`, tenantID, parseID(id)).Scan(&sequence); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("preference %s", id))
			}
			return err
		}
		var makeErr error
		revision, makeErr = makeRevision("career-preference", uint64(sequence))
		return makeErr
	})
	if err != nil {
		return career.CareerPreferenceProfileRevision{}, err
	}
	return s.LoadPreference(ctx, tenant, id, revision)
}

func (s *Store) ListPreferences(ctx context.Context, tenant, id string) ([]career.CareerPreferenceProfileRevision, error) {
	sequences, err := s.listSequences(ctx, tenant, "career_preference_revision", "preference_id", id)
	if err != nil {
		return nil, err
	}
	out := make([]career.CareerPreferenceProfileRevision, 0, len(sequences))
	for _, revision := range sequences {
		value, loadErr := s.LoadPreference(ctx, tenant, id, revision)
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, value)
	}
	return out, nil
}

func (s *Store) SaveTargetRole(ctx context.Context, tenant string, value career.TargetRoleProfileRevision, expected values.RevisionToken) error {
	if value.TargetRoleID.Tenant != values.TenantId(tenant) || value.Worker.Tenant != values.TenantId(tenant) {
		return invalid("target-role references must use the storage tenant")
	}
	if value.CanonicalDigest == "" {
		var err error
		value, err = career.NewTargetRoleProfile(value)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := value.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		worker, err := uuidRef(value.Worker, "worker")
		if err != nil {
			return err
		}
		profile, err := uuidRef(value.JobProfile, "job_profile")
		if err != nil {
			return err
		}
		from, to, err := intervalBounds(value.Effective)
		if err != nil {
			return invalid(err.Error())
		}
		meta, err := revisionWire(value.Revision, value.Supersedes, value.Effective)
		if err != nil {
			return err
		}
		requirements := make([]requirementWire, 0, len(value.Requirements))
		for _, item := range value.Requirements {
			requirements = append(requirements, requirementWire{SkillRef: item.SkillRef.String(), MinimumLevel: item.MinimumLevel})
		}
		payload, err := marshalJSON(targetWire{revisionMeta: meta, JobProfileRevisionStream: value.JobProfileRevision.Stream(), Requirements: requirements})
		if err != nil {
			return err
		}
		if err := checkCAS(ctx, tx, tenantID, "target_role_revision", "target_role_id", value.TargetRoleID.Id, meta, expected); err != nil {
			return err
		}
		targetRoleID, err := uuidRef(value.TargetRoleID, "target_role")
		if err != nil {
			return err
		}
		profileRevision, err := sequenceNumber(value.JobProfileRevision)
		if err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `INSERT INTO target_role_revision (row_id,tenant_id,target_role_id,revision,supersedes,worker_ref,job_profile_ref,job_profile_revision,priority,rationale,visibility,effective_from,effective_to,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, uuid.New(), tenantID, targetRoleID, sequenceValue(value.Revision), nullableSequence(value.Supersedes), worker, profile, profileRevision, 0, string(payload), string(value.Visibility), from, to, storedDigest(value.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("careerstore: insert target-role revision: %w", err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("target role %s revision %d", value.TargetRoleID.Id, sequenceValue(value.Revision)))
		}
		return nil
	})
}

func (s *Store) LoadTargetRole(ctx context.Context, tenant, id string, revision values.RevisionToken) (career.TargetRoleProfileRevision, error) {
	var out career.TargetRoleProfileRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		sequence, err := sequenceNumber(revision)
		if err != nil {
			return err
		}
		var storedRevision int64
		var supersedes *int64
		var workerID, profileID uuid.UUID
		var profileRevision int64
		var rationale []byte
		var from, to *time.Time
		var visibility, digest string
		err = tx.QueryRow(ctx, `SELECT revision,supersedes,worker_ref,job_profile_ref,job_profile_revision,rationale,visibility,effective_from,effective_to,canonical_digest FROM target_role_revision WHERE tenant_id=$1 AND target_role_id=$2 AND revision=$3`, tenantID, parseID(id), sequence).Scan(&storedRevision, &supersedes, &workerID, &profileID, &profileRevision, &rationale, &visibility, &from, &to, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("target role %s revision %d", id, sequence))
			}
			return fmt.Errorf("careerstore: load target-role revision: %w", err)
		}
		var wire targetWire
		if err := json.Unmarshal(rationale, &wire); err != nil {
			return invalid(fmt.Sprintf("target-role metadata: %v", err))
		}
		out.TargetRoleID = entityRef(tenant, "career_target_role", id)
		out.Worker = entityRef(tenant, "worker", workerID.String())
		out.JobProfile = entityRef(tenant, "job_profile", profileID.String())
		out.Revision, err = makeRevision(wire.RevisionStream, uint64(storedRevision))
		if err != nil {
			return err
		}
		out.Supersedes, err = makeOptionalRevision(wire.SupersedesStream, wire.Supersedes)
		if err != nil {
			return err
		}
		out.JobProfileRevision, err = makeRevision(wire.JobProfileRevisionStream, uint64(profileRevision))
		if err != nil {
			return err
		}
		out.Visibility = career.PreferenceVisibility(visibility)
		out.Effective, err = intervalFromBounds(from, to, wire.Interval)
		if err != nil {
			return err
		}
		for _, req := range wire.Requirements {
			var ref values.EntityRef
			if err := ref.UnmarshalText([]byte(req.SkillRef)); err != nil {
				return invalid(err.Error())
			}
			out.Requirements = append(out.Requirements, career.RoleSkillRequirement{SkillRef: ref, MinimumLevel: req.MinimumLevel})
		}
		out.CanonicalDigest = domainDigest(digest)
		if err := out.Validate(); err != nil {
			return invalid(fmt.Sprintf("stored target role failed validation: %v", err))
		}
		return nil
	})
	return out, err
}

func (s *Store) CurrentTargetRole(ctx context.Context, tenant, id string) (career.TargetRoleProfileRevision, error) {
	return currentTarget(ctx, s, tenant, id)
}
func (s *Store) ListTargetRoles(ctx context.Context, tenant, id string) ([]career.TargetRoleProfileRevision, error) {
	sequences, err := s.listSequences(ctx, tenant, "target_role_revision", "target_role_id", id)
	if err != nil {
		return nil, err
	}
	out := make([]career.TargetRoleProfileRevision, 0, len(sequences))
	for _, revision := range sequences {
		value, loadErr := s.LoadTargetRole(ctx, tenant, id, revision)
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, value)
	}
	return out, nil
}

func currentTarget(ctx context.Context, s *Store, tenant, id string) (career.TargetRoleProfileRevision, error) {
	var seq int64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		e := tx.QueryRow(ctx, `SELECT revision FROM target_role_revision WHERE tenant_id=$1 AND target_role_id=$2 ORDER BY revision DESC LIMIT 1`, tid, parseID(id)).Scan(&seq)
		if errors.Is(e, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("target role %s", id))
		}
		return e
	})
	if err != nil {
		return career.TargetRoleProfileRevision{}, err
	}
	r, err := makeRevision("career-target-role", uint64(seq))
	if err != nil {
		return career.TargetRoleProfileRevision{}, err
	}
	return s.LoadTargetRole(ctx, tenant, id, r)
}

func (s *Store) SaveDevelopmentObjective(ctx context.Context, tenant string, value career.DevelopmentObjectiveProfileRevision, expected values.RevisionToken) error {
	if value.ObjectiveID.Tenant != values.TenantId(tenant) || value.Worker.Tenant != values.TenantId(tenant) {
		return invalid("objective references must use the storage tenant")
	}
	if value.CanonicalDigest == "" {
		var err error
		value, err = career.NewDevelopmentObjective(value)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := value.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		worker, err := uuidRef(value.Worker, "worker")
		if err != nil {
			return err
		}
		target, err := uuidRef(value.TargetRole, "target_role")
		if err != nil {
			return err
		}
		objectiveID, err := uuidRef(value.ObjectiveID, "objective")
		if err != nil {
			return err
		}
		from, to, err := intervalBounds(value.Effective)
		if err != nil {
			return invalid(err.Error())
		}
		meta, err := revisionWire(value.Revision, value.Supersedes, value.Effective)
		if err != nil {
			return err
		}
		payload, err := marshalJSON(objectiveWire{revisionMeta: meta, Owner: value.Owner.String(), State: string(value.State), CompletionEvidence: append([]string(nil), value.CompletionEvidenceRefs...)})
		if err != nil {
			return err
		}
		skills, err := marshalJSON(refStrings(value.SkillRefs))
		if err != nil {
			return err
		}
		if err := checkCAS(ctx, tx, tid, "development_objective_revision", "objective_id", value.ObjectiveID.Id, meta, expected); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `INSERT INTO development_objective_revision (row_id,tenant_id,objective_id,revision,supersedes,worker_ref,target_role_ref,skill_refs,description,owner,visibility,effective_from,effective_to,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, uuid.New(), tid, objectiveID, sequenceValue(value.Revision), nullableSequence(value.Supersedes), worker, target, skills, value.Description, string(payload), string(value.Visibility), from, to, storedDigest(value.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("careerstore: insert objective revision: %w", err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("objective %s revision %d", value.ObjectiveID.Id, sequenceValue(value.Revision)))
		}
		return nil
	})
}

func (s *Store) LoadDevelopmentObjective(ctx context.Context, tenant, id string, revision values.RevisionToken) (career.DevelopmentObjectiveProfileRevision, error) {
	var out career.DevelopmentObjectiveProfileRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		seq, err := sequenceNumber(revision)
		if err != nil {
			return err
		}
		var stored int64
		var supersedes *int64
		var worker, target uuid.UUID
		var skills, owner []byte
		var description, visibility, digest string
		var from, to *time.Time
		err = tx.QueryRow(ctx, `SELECT revision,supersedes,worker_ref,target_role_ref,skill_refs,description,owner,visibility,effective_from,effective_to,canonical_digest FROM development_objective_revision WHERE tenant_id=$1 AND objective_id=$2 AND revision=$3`, tid, parseID(id), seq).Scan(&stored, &supersedes, &worker, &target, &skills, &description, &owner, &visibility, &from, &to, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("objective %s revision %d", id, seq))
			}
			return fmt.Errorf("careerstore: load objective revision: %w", err)
		}
		var wire objectiveWire
		if err := json.Unmarshal(owner, &wire); err != nil {
			return invalid(fmt.Sprintf("objective metadata: %v", err))
		}
		out.ObjectiveID = entityRef(tenant, "development_objective", id)
		out.Worker = entityRef(tenant, "worker", worker.String())
		out.TargetRole = entityRef(tenant, "career_target_role", target.String())
		out.Revision, err = makeRevision(wire.RevisionStream, uint64(stored))
		if err != nil {
			return err
		}
		out.Supersedes, err = makeOptionalRevision(wire.SupersedesStream, wire.Supersedes)
		if err != nil {
			return err
		}
		out.Description = description
		out.Owner = entityRefFromString(wire.Owner)
		out.State = career.ObjectiveState(wire.State)
		out.Visibility = career.PreferenceVisibility(visibility)
		out.Effective, err = intervalFromBounds(from, to, wire.Interval)
		if err != nil {
			return err
		}
		out.CompletionEvidenceRefs = append([]string(nil), wire.CompletionEvidence...)
		out.SkillRefs, err = parseRefs(skills, "skill_refs")
		if err != nil {
			return err
		}
		out.CanonicalDigest = domainDigest(digest)
		if err := out.Validate(); err != nil {
			return invalid(fmt.Sprintf("stored objective failed validation: %v", err))
		}
		return nil
	})
	return out, err
}
func (s *Store) CurrentDevelopmentObjective(ctx context.Context, tenant, id string) (career.DevelopmentObjectiveProfileRevision, error) {
	var seq int64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		e := tx.QueryRow(ctx, `SELECT revision FROM development_objective_revision WHERE tenant_id=$1 AND objective_id=$2 ORDER BY revision DESC LIMIT 1`, tid, parseID(id)).Scan(&seq)
		if errors.Is(e, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("objective %s", id))
		}
		return e
	})
	if err != nil {
		return career.DevelopmentObjectiveProfileRevision{}, err
	}
	r, err := makeRevision("career-objective", uint64(seq))
	if err != nil {
		return career.DevelopmentObjectiveProfileRevision{}, err
	}
	return s.LoadDevelopmentObjective(ctx, tenant, id, r)
}
func (s *Store) ListDevelopmentObjectives(ctx context.Context, tenant, id string) ([]career.DevelopmentObjectiveProfileRevision, error) {
	sequences, err := s.listSequences(ctx, tenant, "development_objective_revision", "objective_id", id)
	if err != nil {
		return nil, err
	}
	out := make([]career.DevelopmentObjectiveProfileRevision, 0, len(sequences))
	for _, revision := range sequences {
		value, loadErr := s.LoadDevelopmentObjective(ctx, tenant, id, revision)
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, value)
	}
	return out, nil
}

func (s *Store) SaveAssessment(ctx context.Context, tenant string, value career.CareerAssessmentRevision, expected values.RevisionToken) error {
	if value.AssessmentID.Tenant != values.TenantId(tenant) || value.Worker.Tenant != values.TenantId(tenant) {
		return invalid("assessment references must use the storage tenant")
	}
	if err := value.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		worker, err := uuidRef(value.Worker, "worker")
		if err != nil {
			return err
		}
		target, err := uuidRef(value.TargetRole, "target_role")
		if err != nil {
			return err
		}
		assessmentID, err := uuidRef(value.AssessmentID, "assessment")
		if err != nil {
			return err
		}
		from, to, err := intervalBounds(value.Effective)
		if err != nil {
			return invalid(err.Error())
		}
		meta, err := revisionWire(value.Revision, values.UnspecifiedRevision(), value.Effective)
		if err != nil {
			return err
		}
		source, err := marshalJSON(assessmentWire{revisionMeta: meta, Source: value.Source.String()})
		if err != nil {
			return err
		}
		digest, err := assessmentDigest(value)
		if err != nil {
			return invalid(err.Error())
		}
		if err := checkCAS(ctx, tx, tid, "career_assessment_revision", "assessment_id", value.AssessmentID.Id, meta, expected); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `INSERT INTO career_assessment_revision (row_id,tenant_id,assessment_id,revision,worker_ref,target_role_ref,source,epistemic_class,summary,visibility,effective_from,effective_to,canonical_digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT DO NOTHING`, uuid.New(), tid, assessmentID, sequenceValue(value.Revision), worker, target, string(source), string(value.Epistemic), value.Summary, string(value.Visibility), from, to, storedDigest(digest))
		if err != nil {
			return fmt.Errorf("careerstore: insert assessment revision: %w", err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("assessment %s revision %d", value.AssessmentID.Id, sequenceValue(value.Revision)))
		}
		return nil
	})
}

func (s *Store) LoadAssessment(ctx context.Context, tenant, id string, revision values.RevisionToken) (career.CareerAssessmentRevision, error) {
	var out career.CareerAssessmentRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		seq, err := sequenceNumber(revision)
		if err != nil {
			return err
		}
		var stored int64
		var worker, target uuid.UUID
		var source []byte
		var epistemic, summary, visibility, digest string
		var from, to *time.Time
		err = tx.QueryRow(ctx, `SELECT revision,worker_ref,target_role_ref,source,epistemic_class,summary,visibility,effective_from,effective_to,canonical_digest FROM career_assessment_revision WHERE tenant_id=$1 AND assessment_id=$2 AND revision=$3`, tid, parseID(id), seq).Scan(&stored, &worker, &target, &source, &epistemic, &summary, &visibility, &from, &to, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("assessment %s revision %d", id, seq))
			}
			return fmt.Errorf("careerstore: load assessment revision: %w", err)
		}
		var wire assessmentWire
		if err := json.Unmarshal(source, &wire); err != nil {
			return invalid(fmt.Sprintf("assessment metadata: %v", err))
		}
		out.AssessmentID = entityRef(tenant, "career_assessment", id)
		out.Worker = entityRef(tenant, "worker", worker.String())
		out.TargetRole = entityRef(tenant, "career_target_role", target.String())
		out.Source = entityRefFromString(wire.Source)
		out.Revision, err = makeRevision(wire.RevisionStream, uint64(stored))
		if err != nil {
			return err
		}
		out.Epistemic = career.EpistemicClass(epistemic)
		out.Summary = summary
		out.Visibility = career.PreferenceVisibility(visibility)
		out.Effective, err = intervalFromBounds(from, to, wire.Interval)
		if err != nil {
			return err
		}
		if err := out.Validate(); err != nil {
			return invalid(fmt.Sprintf("stored assessment failed validation: %v", err))
		}
		computed, err := assessmentDigest(out)
		if err != nil || storedDigest(digest) != storedDigest(computed) {
			return invalid("stored assessment digest mismatch")
		}
		return nil
	})
	return out, err
}
func (s *Store) CurrentAssessment(ctx context.Context, tenant, id string) (career.CareerAssessmentRevision, error) {
	var seq int64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tid uuid.UUID) error {
		e := tx.QueryRow(ctx, `SELECT revision FROM career_assessment_revision WHERE tenant_id=$1 AND assessment_id=$2 ORDER BY revision DESC LIMIT 1`, tid, parseID(id)).Scan(&seq)
		if errors.Is(e, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("assessment %s", id))
		}
		return e
	})
	if err != nil {
		return career.CareerAssessmentRevision{}, err
	}
	r, err := makeRevision("career-assessment", uint64(seq))
	if err != nil {
		return career.CareerAssessmentRevision{}, err
	}
	return s.LoadAssessment(ctx, tenant, id, r)
}
func (s *Store) ListAssessments(ctx context.Context, tenant, id string) ([]career.CareerAssessmentRevision, error) {
	sequences, err := s.listSequences(ctx, tenant, "career_assessment_revision", "assessment_id", id)
	if err != nil {
		return nil, err
	}
	out := make([]career.CareerAssessmentRevision, 0, len(sequences))
	for _, revision := range sequences {
		value, loadErr := s.LoadAssessment(ctx, tenant, id, revision)
		if loadErr != nil {
			return nil, loadErr
		}
		out = append(out, value)
	}
	return out, nil
}

func checkCAS(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, table, key, id string, next revisionMeta, expected values.RevisionToken) error {
	var current int64
	var raw []byte
	query := fmt.Sprintf(`SELECT revision,%s FROM %s WHERE tenant_id=$1 AND %s=$2 ORDER BY revision DESC LIMIT 1`, metadataColumn(table), table, key)
	err := tx.QueryRow(ctx, query, tenantID, parseID(id)).Scan(&current, &raw)
	if errors.Is(err, dbport.ErrNoRows) {
		if expected.IsSpecified() || next.Supersedes > 0 {
			return stale(expected, values.UnspecifiedRevision())
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("careerstore: read %s head: %w", table, err)
	}
	var prior revisionMeta
	if err := json.Unmarshal(raw, &prior); err != nil {
		return invalid(fmt.Sprintf("stored %s metadata: %v", table, err))
	}
	actual, err := makeRevision(prior.RevisionStream, uint64(current))
	if err != nil {
		return err
	}
	if !expected.IsSpecified() {
		if prior.Revision == uint64(current) && prior.Revision == next.Revision {
			return duplicate(fmt.Sprintf("%s %s revision %d", table, id, next.Revision))
		}
		return stale(expected, actual)
	}
	if prior.Revision == uint64(current) && prior.Revision == next.Revision {
		return duplicate(fmt.Sprintf("%s %s revision %d", table, id, next.Revision))
	}
	if !expected.Equal(actual) || (next.Supersedes > 0 && (int64(next.Supersedes) != current || next.SupersedesStream != expected.Stream())) {
		return stale(expected, actual)
	}
	return nil
}
func metadataColumn(table string) string {
	switch table {
	case "career_preference_revision":
		return "timing_constraints"
	case "target_role_revision":
		return "rationale"
	case "development_objective_revision":
		return "owner"
	case "career_assessment_revision":
		return "source"
	default:
		return "source"
	}
}
func (s *Store) listSequences(ctx context.Context, tenant, table, key, id string) ([]values.RevisionToken, error) {
	var sequences []values.RevisionToken
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		query := fmt.Sprintf("SELECT revision FROM %s WHERE tenant_id=$1 AND %s=$2 ORDER BY revision", table, key)
		rows, err := tx.Query(ctx, query, tenantID, parseID(id))
		if err != nil {
			return fmt.Errorf("careerstore: list %s: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var sequence int64
			if err := rows.Scan(&sequence); err != nil {
				return err
			}
			revision, makeErr := makeRevision("career-"+key, uint64(sequence))
			if makeErr != nil {
				return makeErr
			}
			sequences = append(sequences, revision)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(sequences) == 0 {
		return nil, notFound(fmt.Sprintf("%s %s", key, id))
	}
	return sequences, nil
}

func sequenceNumber(revision values.RevisionToken) (int64, error) {
	n, ok := revision.Sequence()
	if !ok || n == 0 || n > uint64(^uint64(0)>>1) {
		return 0, invalid("revision must be a positive sequence token")
	}
	return int64(n), nil
}
func sequenceValue(revision values.RevisionToken) int64 { n, _ := revision.Sequence(); return int64(n) }
func nullableSequence(revision values.RevisionToken) any {
	if !revision.IsSpecified() {
		return nil
	}
	return sequenceValue(revision)
}
func parseID(id string) uuid.UUID             { v, _ := uuid.Parse(id); return v }
func mustUUID(ref values.EntityRef) uuid.UUID { v, _ := uuid.Parse(ref.Id); return v }
func entityRef(tenant, kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind(kind), Id: id}
}
func entityRefFromString(text string) values.EntityRef {
	var ref values.EntityRef
	_ = ref.UnmarshalText([]byte(text))
	return ref
}
func makeRevision(stream string, sequence uint64) (values.RevisionToken, error) {
	if stream == "" {
		stream = "career"
	}
	return values.NewSequenceRevision(stream, sequence)
}
func makeOptionalRevision(stream string, sequence uint64) (values.RevisionToken, error) {
	if sequence == 0 {
		return values.UnspecifiedRevision(), nil
	}
	return makeRevision(stream, sequence)
}
