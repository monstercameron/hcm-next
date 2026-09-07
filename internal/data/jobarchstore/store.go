// Package jobarchstore persists the tenant-scoped job architecture revision
// families and binding snapshots from migration 00048.
package jobarchstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/jobarch"
)

// DB is the transaction capability this adapter needs. The adapter owns each
// transaction so tenant context is established before any row is touched.
type DB interface{ dbport.Beginner }

// Store implements jobarch.Store over PostgreSQL.
type Store struct{ db DB }

var _ jobarch.Store = (*Store)(nil)

// New returns a PostgreSQL job-architecture store over db.
func New(db DB) *Store { return &Store{db: db} }

type revisionRef struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type lineageEnvelope struct {
	RootID     string          `json:"root_id"`
	Supersedes string          `json:"supersedes"`
	ParentID   string          `json:"parent_id"`
	Payload    json.RawMessage `json:"payload"`
}

func invalid(detail string) error {
	return &jobarch.StoreError{Code: jobarch.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &jobarch.StoreError{Code: jobarch.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &jobarch.StoreError{Code: jobarch.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual, detail string) error {
	return &jobarch.StoreError{Code: jobarch.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	if tenantID == uuid.Nil {
		return invalid("tenant id is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("jobarchstore: begin transaction: %w", err)
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
		return fmt.Errorf("jobarchstore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(tenantID)
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a valid uuid", tenantID))
	}
	return tid, nil
}

func (s *Store) Save(ctx context.Context, tenantID string, architecture jobarch.ArchitectureRevision, expectedRevision string) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if architecture.CanonicalDigest == "" {
		architecture, err = jobarch.NewArchitectureRevision(architecture)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := architecture.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		return saveTx(ctx, tx, tid, architecture, expectedRevision)
	})
}

func (s *Store) Load(ctx context.Context, tenantID string, architectureID, revision string) (jobarch.ArchitectureRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return jobarch.ArchitectureRevision{}, err
	}
	var out jobarch.ArchitectureRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		out, err = loadTx(ctx, tx, tid, architectureID, revision)
		return err
	})
	return out, err
}

func (s *Store) Current(ctx context.Context, tenantID string, architectureID string) (jobarch.ArchitectureRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return jobarch.ArchitectureRevision{}, err
	}
	var out jobarch.ArchitectureRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		revision, err := currentRevisionTx(ctx, tx, tid, architectureID)
		if err != nil {
			return err
		}
		out, err = loadTx(ctx, tx, tid, architectureID, revision)
		return err
	})
	return out, err
}

func (s *Store) List(ctx context.Context, tenantID string, architectureID string) ([]jobarch.ArchitectureRevision, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var out []jobarch.ArchitectureRevision
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT revision FROM job_architecture_revision
			WHERE tenant_id = $1 AND architecture_id = $2
			ORDER BY revision`, tid, architectureID)
		if err != nil {
			return fmt.Errorf("jobarchstore: list architecture %s: %w", architectureID, err)
		}
		defer rows.Close()
		var revisions []string
		for rows.Next() {
			var revision string
			if err := rows.Scan(&revision); err != nil {
				return fmt.Errorf("jobarchstore: scan architecture revision: %w", err)
			}
			revisions = append(revisions, revision)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("jobarchstore: list architecture %s: %w", architectureID, err)
		}
		if len(revisions) == 0 {
			return notFound(fmt.Sprintf("architecture %s", architectureID))
		}
		out = make([]jobarch.ArchitectureRevision, 0, len(revisions))
		for _, revision := range revisions {
			architecture, loadErr := loadTx(ctx, tx, tid, architectureID, revision)
			if loadErr != nil {
				return loadErr
			}
			out = append(out, architecture)
		}
		return nil
	})
	return out, err
}

func saveTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, architecture jobarch.ArchitectureRevision, expectedRevision string) error {
	// Serialise the compare-and-swap decision for one tenant/architecture pair.
	// The lock is transaction-scoped and does not add a table or mutable state.
	lockKey := tenantID.String() + ":" + architecture.ID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("jobarchstore: lock architecture %s: %w", architecture.ID, err)
	}

	exists, err := architectureRevisionExists(ctx, tx, tenantID, architecture.ID, architecture.Revision)
	if err != nil {
		return err
	}
	if exists {
		return duplicate(fmt.Sprintf("architecture %s revision %s", architecture.ID, architecture.Revision))
	}
	actual, hasCurrent, err := currentRevisionMaybeTx(ctx, tx, tenantID, architecture.ID)
	if err != nil {
		return err
	}
	if expectedRevision == "" {
		if hasCurrent {
			return stale(expectedRevision, actual, fmt.Sprintf("architecture %s already has current revision %s", architecture.ID, actual))
		}
		if architecture.SupersedesRevision != "" {
			return invalid("initial architecture cannot supersede a revision")
		}
	} else {
		if !hasCurrent || actual != expectedRevision || architecture.SupersedesRevision != expectedRevision {
			return stale(expectedRevision, actual, fmt.Sprintf("architecture %s current revision is %s", architecture.ID, actual))
		}
	}

	if err := insertFamily(ctx, tx, tenantID, architecture.Families); err != nil {
		return err
	}
	if err := insertLevel(ctx, tx, tenantID, architecture.Levels); err != nil {
		return err
	}
	if err := insertGrade(ctx, tx, tenantID, architecture.Grades); err != nil {
		return err
	}
	if err := insertProfile(ctx, tx, tenantID, architecture.Profiles); err != nil {
		return err
	}

	families := make([]revisionRef, 0, len(architecture.Families))
	for _, family := range architecture.Families {
		families = append(families, revisionRef{ID: family.FamilyIDOrID(), Revision: family.Revision})
	}
	levels := make([]revisionRef, 0, len(architecture.Levels))
	for _, level := range architecture.Levels {
		levels = append(levels, revisionRef{ID: level.LevelIDOrID(), Revision: level.Revision})
	}
	grades := make([]revisionRef, 0, len(architecture.Grades))
	for _, grade := range architecture.Grades {
		grades = append(grades, revisionRef{ID: grade.GradeIDOrID(), Revision: grade.Revision})
	}
	profiles := make([]revisionRef, 0, len(architecture.Profiles))
	for _, profile := range architecture.Profiles {
		profiles = append(profiles, revisionRef{ID: profile.ProfileIDOrID(), Revision: profile.Revision})
	}
	familyJSON, _ := json.Marshal(families)
	levelJSON, _ := json.Marshal(levels)
	gradeJSON, _ := json.Marshal(grades)
	profileJSON, _ := json.Marshal(profiles)
	_, err = tx.Exec(ctx, `
		INSERT INTO job_architecture_revision (
			row_id, tenant_id, architecture_id, revision, supersedes_revision,
			families, levels, grades, profiles, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		uuid.New(), tenantID, architecture.ID, architecture.Revision, nullableText(architecture.SupersedesRevision),
		familyJSON, levelJSON, gradeJSON, profileJSON, storageDigest(architecture.CanonicalDigest))
	if err != nil {
		if isUniqueViolation(err) {
			return duplicate(fmt.Sprintf("architecture %s revision %s", architecture.ID, architecture.Revision))
		}
		return fmt.Errorf("jobarchstore: insert architecture %s/%s: %w", architecture.ID, architecture.Revision, err)
	}
	return nil
}

func insertFamily(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, values []jobarch.JobFamilyRevision) error {
	for _, value := range values {
		blob, err := jsonLineage(value.Lineage, value.ParentID, value)
		if err != nil {
			return err
		}
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO job_family_revision (
				row_id, tenant_id, family_id, revision, parent_id, lifecycle,
				effective_from, effective_to, known_from, known_to, lineage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (tenant_id, family_id, revision) DO NOTHING`,
			uuid.New(), tenantID, value.FamilyIDOrID(), value.Revision, nullableText(value.ParentID), value.Lifecycle,
			nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo), nullableTime(value.KnownFrom), nullableTime(value.KnownTo), blob)
		if execErr != nil {
			return fmt.Errorf("jobarchstore: insert family %s/%s: %w", value.FamilyIDOrID(), value.Revision, execErr)
		}
		if affected == 0 {
			if err := sameLineage(ctx, tx, `SELECT lineage FROM job_family_revision WHERE tenant_id=$1 AND family_id=$2 AND revision=$3`, tenantID, value.FamilyIDOrID(), value.Revision, blob); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertLevel(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, values []jobarch.JobLevelRevision) error {
	for _, value := range values {
		blob, err := jsonLineage(value.Lineage, value.FamilyIDOrFamily(), value)
		if err != nil {
			return err
		}
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO job_level_revision (
				row_id, tenant_id, level_id, revision, parent_id, lifecycle,
				effective_from, effective_to, known_from, known_to, lineage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (tenant_id, level_id, revision) DO NOTHING`,
			uuid.New(), tenantID, value.LevelIDOrID(), value.Revision, nullableText(value.FamilyIDOrFamily()), value.Lifecycle,
			nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo), nullableTime(value.KnownFrom), nullableTime(value.KnownTo), blob)
		if execErr != nil {
			return fmt.Errorf("jobarchstore: insert level %s/%s: %w", value.LevelIDOrID(), value.Revision, execErr)
		}
		if affected == 0 {
			if err := sameLineage(ctx, tx, `SELECT lineage FROM job_level_revision WHERE tenant_id=$1 AND level_id=$2 AND revision=$3`, tenantID, value.LevelIDOrID(), value.Revision, blob); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertGrade(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, values []jobarch.JobGradeRevision) error {
	for _, value := range values {
		blob, err := jsonLineage(value.Lineage, value.LevelIDOrRef(), value)
		if err != nil {
			return err
		}
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO job_grade_revision (
				row_id, tenant_id, grade_id, revision, parent_id, lifecycle,
				effective_from, effective_to, known_from, known_to, lineage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (tenant_id, grade_id, revision) DO NOTHING`,
			uuid.New(), tenantID, value.GradeIDOrID(), value.Revision, nullableText(value.LevelIDOrRef()), value.Lifecycle,
			nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo), nullableTime(value.KnownFrom), nullableTime(value.KnownTo), blob)
		if execErr != nil {
			return fmt.Errorf("jobarchstore: insert grade %s/%s: %w", value.GradeIDOrID(), value.Revision, execErr)
		}
		if affected == 0 {
			if err := sameLineage(ctx, tx, `SELECT lineage FROM job_grade_revision WHERE tenant_id=$1 AND grade_id=$2 AND revision=$3`, tenantID, value.GradeIDOrID(), value.Revision, blob); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertProfile(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, values []jobarch.JobProfileRevision) error {
	for _, value := range values {
		blob, err := jsonLineage(value.Lineage, value.FamilyIDOrRef(), value)
		if err != nil {
			return err
		}
		affected, execErr := tx.Exec(ctx, `
			INSERT INTO job_profile_revision (
				row_id, tenant_id, profile_id, revision, parent_id, lifecycle,
				effective_from, effective_to, known_from, known_to, lineage)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (tenant_id, profile_id, revision) DO NOTHING`,
			uuid.New(), tenantID, value.ProfileIDOrID(), value.Revision, nullableText(value.FamilyIDOrRef()), value.Lifecycle,
			nullableTime(value.EffectiveFrom), nullableTime(value.EffectiveTo), nullableTime(value.KnownFrom), nullableTime(value.KnownTo), blob)
		if execErr != nil {
			return fmt.Errorf("jobarchstore: insert profile %s/%s: %w", value.ProfileIDOrID(), value.Revision, execErr)
		}
		if affected == 0 {
			if err := sameLineage(ctx, tx, `SELECT lineage FROM job_profile_revision WHERE tenant_id=$1 AND profile_id=$2 AND revision=$3`, tenantID, value.ProfileIDOrID(), value.Revision, blob); err != nil {
				return err
			}
		}
	}
	return nil
}

func jsonLineage(lineage jobarch.RevisionLineage, parentID string, payload any) ([]byte, error) {
	if profile, ok := payload.(jobarch.JobProfileRevision); ok {
		payload = storageProfile(profile)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jobarchstore: encode revision payload: %w", err)
	}
	return json.Marshal(lineageEnvelope{RootID: lineage.RootID, Supersedes: lineage.Supersedes, ParentID: parentID, Payload: encoded})
}

// storageProfile makes the compatibility-shaped reference structs safe for
// JSON storage. Several governed reference types retain their historical
// embedded VersionedReference alongside direct fields with the same JSON
// names. encoding/json gives the shallow fields precedence, so an embedded-
// only reference would otherwise be stored as an all-zero reference and make
// the architecture digest change on reload.
func storageProfile(profile jobarch.JobProfileRevision) jobarch.JobProfileRevision {
	profile.Requirements = storageRequirements(profile.Requirements)
	profile.QualificationRefs = append([]jobarch.QualificationReference(nil), profile.QualificationRefs...)
	profile.SkillRefs = append([]jobarch.SkillRequirementReference(nil), profile.SkillRefs...)
	profile.CredentialRefs = append([]jobarch.CredentialRequirementReference(nil), profile.CredentialRefs...)
	for i := range profile.QualificationRefs {
		profile.QualificationRefs[i] = storageQualification(profile.QualificationRefs[i])
	}
	for i := range profile.SkillRefs {
		profile.SkillRefs[i] = storageSkill(profile.SkillRefs[i])
	}
	for i := range profile.CredentialRefs {
		profile.CredentialRefs[i] = storageCredential(profile.CredentialRefs[i])
	}
	return profile
}

func storageRequirements(requirements jobarch.JobProfileRequirements) jobarch.JobProfileRequirements {
	requirements.Qualifications = append([]jobarch.QualificationReference(nil), requirements.Qualifications...)
	requirements.QualificationRefs = append([]jobarch.QualificationReference(nil), requirements.QualificationRefs...)
	requirements.Skills = append([]jobarch.SkillRequirementReference(nil), requirements.Skills...)
	requirements.SkillRefs = append([]jobarch.SkillRequirementReference(nil), requirements.SkillRefs...)
	requirements.Credentials = append([]jobarch.CredentialRequirementReference(nil), requirements.Credentials...)
	requirements.CredentialRefs = append([]jobarch.CredentialRequirementReference(nil), requirements.CredentialRefs...)
	for i := range requirements.Qualifications {
		requirements.Qualifications[i] = storageQualification(requirements.Qualifications[i])
	}
	for i := range requirements.QualificationRefs {
		requirements.QualificationRefs[i] = storageQualification(requirements.QualificationRefs[i])
	}
	for i := range requirements.Skills {
		requirements.Skills[i] = storageSkill(requirements.Skills[i])
	}
	for i := range requirements.SkillRefs {
		requirements.SkillRefs[i] = storageSkill(requirements.SkillRefs[i])
	}
	for i := range requirements.Credentials {
		requirements.Credentials[i] = storageCredential(requirements.Credentials[i])
	}
	for i := range requirements.CredentialRefs {
		requirements.CredentialRefs[i] = storageCredential(requirements.CredentialRefs[i])
	}
	return requirements
}

func storageQualification(value jobarch.QualificationReference) jobarch.QualificationReference {
	value.Ref, value.Revision, value.Authority = firstText(value.Ref, value.VersionedReference.Ref), firstText(value.Revision, value.VersionedReference.Revision), firstText(value.Authority, value.VersionedReference.Authority)
	value.EffectiveFrom = firstTime(value.EffectiveFrom, value.VersionedReference.EffectiveFrom)
	value.EffectiveTo = firstTime(value.EffectiveTo, value.VersionedReference.EffectiveTo)
	value.VersionedReference = jobarch.VersionedReference{}
	return value
}

func storageSkill(value jobarch.SkillRequirementReference) jobarch.SkillRequirementReference {
	value.Ref, value.Revision, value.Authority = firstText(value.Ref, value.VersionedReference.Ref), firstText(value.Revision, value.VersionedReference.Revision), firstText(value.Authority, value.VersionedReference.Authority)
	value.EffectiveFrom = firstTime(value.EffectiveFrom, value.VersionedReference.EffectiveFrom)
	value.EffectiveTo = firstTime(value.EffectiveTo, value.VersionedReference.EffectiveTo)
	value.VersionedReference = jobarch.VersionedReference{}
	return value
}

func storageCredential(value jobarch.CredentialRequirementReference) jobarch.CredentialRequirementReference {
	value.Ref, value.Revision, value.Authority = firstText(value.Ref, value.VersionedReference.Ref), firstText(value.Revision, value.VersionedReference.Revision), firstText(value.Authority, value.VersionedReference.Authority)
	value.EffectiveFrom = firstTime(value.EffectiveFrom, value.VersionedReference.EffectiveFrom)
	value.EffectiveTo = firstTime(value.EffectiveTo, value.VersionedReference.EffectiveTo)
	value.VersionedReference = jobarch.VersionedReference{}
	return value
}

func firstText(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func firstTime(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary
	}
	return fallback
}

func sameLineage(ctx context.Context, tx dbport.Tx, query string, tenantID uuid.UUID, id, revision string, expected []byte) error {
	var stored []byte
	if err := tx.QueryRow(ctx, query, tenantID, id, revision).Scan(&stored); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return duplicate(fmt.Sprintf("revision %s/%s disappeared during insert", id, revision))
		}
		return fmt.Errorf("jobarchstore: read existing revision %s/%s: %w", id, revision, err)
	}
	var storedValue, expectedValue any
	if err := json.Unmarshal(stored, &storedValue); err != nil {
		return duplicate(fmt.Sprintf("revision %s/%s stores invalid lineage", id, revision))
	}
	if err := json.Unmarshal(expected, &expectedValue); err != nil {
		return fmt.Errorf("jobarchstore: compare expected lineage %s/%s: %w", id, revision, err)
	}
	if !reflect.DeepEqual(storedValue, expectedValue) {
		return duplicate(fmt.Sprintf("revision %s/%s already stores different meaning", id, revision))
	}
	return nil
}

func loadTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, architectureID, revision string) (jobarch.ArchitectureRevision, error) {
	if tenantID == uuid.Nil || architectureID == "" || revision == "" {
		return jobarch.ArchitectureRevision{}, invalid("tenant, architecture id and revision are required")
	}
	var (
		rowID, storedTenant                           uuid.UUID
		storedID, storedRevision                      string
		supersedes                                    *string
		familyJSON, levelJSON, gradeJSON, profileJSON []byte
		canonicalDigest                               string
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, tenant_id, architecture_id, revision, supersedes_revision,
			families, levels, grades, profiles, canonical_digest
		FROM job_architecture_revision
		WHERE tenant_id=$1 AND architecture_id=$2 AND revision=$3`, tenantID, architectureID, revision).
		Scan(&rowID, &storedTenant, &storedID, &storedRevision, &supersedes, &familyJSON, &levelJSON, &gradeJSON, &profileJSON, &canonicalDigest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return jobarch.ArchitectureRevision{}, notFound(fmt.Sprintf("architecture %s revision %s", architectureID, revision))
		}
		return jobarch.ArchitectureRevision{}, fmt.Errorf("jobarchstore: load architecture %s/%s: %w", architectureID, revision, err)
	}
	_ = rowID
	_ = storedTenant
	families, err := decodeRefs(familyJSON, "families")
	if err != nil {
		return jobarch.ArchitectureRevision{}, err
	}
	levels, err := decodeRefs(levelJSON, "levels")
	if err != nil {
		return jobarch.ArchitectureRevision{}, err
	}
	grades, err := decodeRefs(gradeJSON, "grades")
	if err != nil {
		return jobarch.ArchitectureRevision{}, err
	}
	profiles, err := decodeRefs(profileJSON, "profiles")
	if err != nil {
		return jobarch.ArchitectureRevision{}, err
	}
	architecture := jobarch.ArchitectureRevision{ID: storedID, Revision: storedRevision, CanonicalDigest: domainDigest(canonicalDigest)}
	if supersedes != nil {
		architecture.SupersedesRevision = *supersedes
	}
	for _, ref := range families {
		value, loadErr := loadFamily(ctx, tx, tenantID, ref)
		if loadErr != nil {
			return jobarch.ArchitectureRevision{}, loadErr
		}
		architecture.Families = append(architecture.Families, value)
	}
	for _, ref := range levels {
		value, loadErr := loadLevel(ctx, tx, tenantID, ref)
		if loadErr != nil {
			return jobarch.ArchitectureRevision{}, loadErr
		}
		architecture.Levels = append(architecture.Levels, value)
	}
	for _, ref := range grades {
		value, loadErr := loadGrade(ctx, tx, tenantID, ref)
		if loadErr != nil {
			return jobarch.ArchitectureRevision{}, loadErr
		}
		architecture.Grades = append(architecture.Grades, value)
	}
	for _, ref := range profiles {
		value, loadErr := loadProfile(ctx, tx, tenantID, ref)
		if loadErr != nil {
			return jobarch.ArchitectureRevision{}, loadErr
		}
		architecture.Profiles = append(architecture.Profiles, value)
	}
	if err := architecture.Validate(); err != nil {
		return jobarch.ArchitectureRevision{}, invalid(fmt.Sprintf("stored architecture failed validation: %v", err))
	}
	return architecture, nil
}

func decodeRefs(raw []byte, field string) ([]revisionRef, error) {
	var refs []revisionRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, invalid(fmt.Sprintf("%s is not a reference array: %v", field, err))
	}
	for _, ref := range refs {
		if ref.ID == "" || ref.Revision == "" {
			return nil, invalid(fmt.Sprintf("%s contains an incomplete reference", field))
		}
	}
	return refs, nil
}

func loadFamily(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, ref revisionRef) (jobarch.JobFamilyRevision, error) {
	var blob []byte
	if err := tx.QueryRow(ctx, `SELECT lineage FROM job_family_revision WHERE tenant_id=$1 AND family_id=$2 AND revision=$3`, tenantID, ref.ID, ref.Revision).Scan(&blob); err != nil {
		return jobarch.JobFamilyRevision{}, referenceError("family", ref, err)
	}
	var envelope lineageEnvelope
	if err := json.Unmarshal(blob, &envelope); err != nil {
		return jobarch.JobFamilyRevision{}, invalid(fmt.Sprintf("family %s/%s lineage: %v", ref.ID, ref.Revision, err))
	}
	var value jobarch.JobFamilyRevision
	if err := json.Unmarshal(envelope.Payload, &value); err != nil {
		return jobarch.JobFamilyRevision{}, invalid(fmt.Sprintf("family %s/%s payload: %v", ref.ID, ref.Revision, err))
	}
	return value, nil
}

func loadLevel(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, ref revisionRef) (jobarch.JobLevelRevision, error) {
	var blob []byte
	if err := tx.QueryRow(ctx, `SELECT lineage FROM job_level_revision WHERE tenant_id=$1 AND level_id=$2 AND revision=$3`, tenantID, ref.ID, ref.Revision).Scan(&blob); err != nil {
		return jobarch.JobLevelRevision{}, referenceError("level", ref, err)
	}
	var envelope lineageEnvelope
	if err := json.Unmarshal(blob, &envelope); err != nil {
		return jobarch.JobLevelRevision{}, invalid(fmt.Sprintf("level %s/%s lineage: %v", ref.ID, ref.Revision, err))
	}
	var value jobarch.JobLevelRevision
	if err := json.Unmarshal(envelope.Payload, &value); err != nil {
		return jobarch.JobLevelRevision{}, invalid(fmt.Sprintf("level %s/%s payload: %v", ref.ID, ref.Revision, err))
	}
	return value, nil
}

func loadGrade(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, ref revisionRef) (jobarch.JobGradeRevision, error) {
	var blob []byte
	if err := tx.QueryRow(ctx, `SELECT lineage FROM job_grade_revision WHERE tenant_id=$1 AND grade_id=$2 AND revision=$3`, tenantID, ref.ID, ref.Revision).Scan(&blob); err != nil {
		return jobarch.JobGradeRevision{}, referenceError("grade", ref, err)
	}
	var envelope lineageEnvelope
	if err := json.Unmarshal(blob, &envelope); err != nil {
		return jobarch.JobGradeRevision{}, invalid(fmt.Sprintf("grade %s/%s lineage: %v", ref.ID, ref.Revision, err))
	}
	var value jobarch.JobGradeRevision
	if err := json.Unmarshal(envelope.Payload, &value); err != nil {
		return jobarch.JobGradeRevision{}, invalid(fmt.Sprintf("grade %s/%s payload: %v", ref.ID, ref.Revision, err))
	}
	return value, nil
}

func loadProfile(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, ref revisionRef) (jobarch.JobProfileRevision, error) {
	var blob []byte
	if err := tx.QueryRow(ctx, `SELECT lineage FROM job_profile_revision WHERE tenant_id=$1 AND profile_id=$2 AND revision=$3`, tenantID, ref.ID, ref.Revision).Scan(&blob); err != nil {
		return jobarch.JobProfileRevision{}, referenceError("profile", ref, err)
	}
	var envelope lineageEnvelope
	if err := json.Unmarshal(blob, &envelope); err != nil {
		return jobarch.JobProfileRevision{}, invalid(fmt.Sprintf("profile %s/%s lineage: %v", ref.ID, ref.Revision, err))
	}
	var value jobarch.JobProfileRevision
	if err := json.Unmarshal(envelope.Payload, &value); err != nil {
		return jobarch.JobProfileRevision{}, invalid(fmt.Sprintf("profile %s/%s payload: %v", ref.ID, ref.Revision, err))
	}
	return value, nil
}

func referenceError(kind string, ref revisionRef, err error) error {
	if errors.Is(err, dbport.ErrNoRows) {
		return invalid(fmt.Sprintf("architecture references %s %s/%s outside its tenant", kind, ref.ID, ref.Revision))
	}
	return fmt.Errorf("jobarchstore: load %s %s/%s: %w", kind, ref.ID, ref.Revision, err)
}

func currentRevisionTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, architectureID string) (string, error) {
	revision, ok, err := currentRevisionMaybeTx(ctx, tx, tenantID, architectureID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", notFound(fmt.Sprintf("architecture %s", architectureID))
	}
	return revision, nil
}

func currentRevisionMaybeTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, architectureID string) (string, bool, error) {
	if architectureID == "" {
		return "", false, invalid("architecture id is required")
	}
	var revision string
	err := tx.QueryRow(ctx, `
		SELECT current_row.revision
		FROM job_architecture_revision AS current_row
		WHERE current_row.tenant_id=$1 AND current_row.architecture_id=$2
		  AND NOT EXISTS (
			SELECT 1 FROM job_architecture_revision AS successor
			WHERE successor.tenant_id=current_row.tenant_id
			  AND successor.architecture_id=current_row.architecture_id
			  AND successor.supersedes_revision=current_row.revision)
		ORDER BY current_row.row_id DESC
		LIMIT 1`, tenantID, architectureID).Scan(&revision)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("jobarchstore: find current architecture %s: %w", architectureID, err)
	}
	return revision, true, nil
}

func architectureRevisionExists(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, architectureID, revision string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM job_architecture_revision WHERE tenant_id=$1 AND architecture_id=$2 AND revision=$3)`, tenantID, architectureID, revision).Scan(&exists); err != nil {
		return false, fmt.Errorf("jobarchstore: check architecture %s/%s: %w", architectureID, revision, err)
	}
	return exists, nil
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullableTime(value interface{ IsZero() bool }) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func isUniqueViolation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint")
}

// storageDigest bridges the domain's tagged digest ("sha256:<hex>") to the
// migration 00002 content_digest domain, which stores bare lowercase hex.
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
