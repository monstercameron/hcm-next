// Package skillstore persists the skill ontology revisions and worker-skill
// evidence defined by internal/domains/skill over migration 00116.
package skillstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/skill"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the transaction-opening capability used by Store.
type DB interface{ dbport.Beginner }

// Store is the PostgreSQL implementation of [skill.Store]. Each operation
// scopes its own transaction so the tenant setting cannot leak across uses of
// a pooled connection.
type Store struct{ db DB }

var _ skill.Store = (*Store)(nil)
var _ skill.SkillEvidenceReader = (*Store)(nil)

// New returns a PostgreSQL-backed skill store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenant skill.TenantID, fn func(dbport.Tx, uuid.UUID) error) error {
	if ctx == nil {
		return refuse("SKILL_INVALID_CONTEXT", "context", "context is required", skill.ErrStoreRefused)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("skillstore: begin transaction: %w", err)
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
		return fmt.Errorf("skillstore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(tenant skill.TenantID) (uuid.UUID, error) {
	if tenant == nil || tenant.String() == "" {
		return uuid.Nil, refuse("SKILL_INVALID_TENANT", "tenant_id", "tenant id is required", skill.ErrStoreRefused)
	}
	tenantID, err := uuid.Parse(tenant.String())
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, refuse("SKILL_INVALID_TENANT", "tenant_id", "tenant id must be a non-nil UUID", skill.ErrStoreRefused)
	}
	return tenantID, nil
}

func refuse(code, field, reason string, cause error) error {
	return &skill.RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

func storedDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") {
		return value
	}
	return "sha256:" + value
}

func entityID(ref values.EntityRef, expected values.Kind) (string, error) {
	if err := ref.Validate(); err != nil {
		return "", err
	}
	if ref.Kind != expected {
		return "", fmt.Errorf("skillstore: %s has kind %q, want %q", ref, ref.Kind, expected)
	}
	return ref.Id, nil
}

func sequence(revision values.RevisionToken) (uint64, error) {
	if err := revision.Validate(); err != nil {
		return 0, err
	}
	value, ok := revision.Sequence()
	if !ok || value == 0 {
		return 0, refuse(skill.ErrStaleRevision.Error(), "revision", "skill persistence requires a positive sequence revision", skill.ErrStaleRevision)
	}
	return value, nil
}

func validateTenantRef(tenant skill.TenantID, ref values.EntityRef, field string) error {
	if err := checkTenant(tenant); err != nil {
		return err
	}
	if ref.Tenant.String() != tenant.String() {
		return refuse("SKILL_TENANT_MISMATCH", field, "reference belongs to another tenant", skill.ErrStoreRefused)
	}
	return nil
}

func checkTenant(tenant skill.TenantID) error {
	if tenant == nil || tenant.String() == "" {
		return refuse("SKILL_INVALID_TENANT", "tenant_id", "tenant id is required", skill.ErrStoreRefused)
	}
	return nil
}

type definitionWire struct {
	ParentRefs       []string                 `json:"parent_refs"`
	Aliases          []string                 `json:"aliases"`
	ProficiencyScale []skill.ProficiencyLevel `json:"proficiency_scale"`
}

func marshalDefinition(definition skill.SkillDefinitionRevision) (definitionWire, error) {
	ref := definition.SkillRef
	if ref.Validate() != nil {
		ref = definition.SkillID
	}
	if _, err := entityID(ref, skill.KindSkill); err != nil {
		return definitionWire{}, err
	}
	parents := make([]string, 0, len(definition.ParentRefs))
	for _, parent := range definition.ParentRefs {
		id, err := entityID(parent, skill.KindSkill)
		if err != nil {
			return definitionWire{}, err
		}
		parents = append(parents, id)
	}
	return definitionWire{
		ParentRefs:       append([]string{}, parents...),
		Aliases:          append([]string{}, definition.Aliases...),
		ProficiencyScale: append([]skill.ProficiencyLevel{}, definition.ProficiencyScale...),
	}, nil
}

func unmarshalDefinition(tenant, skillRef, ontologyRef string, revision, supersedes int64, name, parentJSON, aliasJSON, scaleJSON, digest string) (skill.SkillDefinitionRevision, error) {
	var wire definitionWire
	if parentJSON != "" {
		if err := json.Unmarshal([]byte(parentJSON), &wire.ParentRefs); err != nil {
			return skill.SkillDefinitionRevision{}, fmt.Errorf("skillstore: decode parent_refs: %w", err)
		}
	}
	if aliasJSON != "" {
		if err := json.Unmarshal([]byte(aliasJSON), &wire.Aliases); err != nil {
			return skill.SkillDefinitionRevision{}, fmt.Errorf("skillstore: decode aliases: %w", err)
		}
	}
	if scaleJSON != "" {
		if err := json.Unmarshal([]byte(scaleJSON), &wire.ProficiencyScale); err != nil {
			return skill.SkillDefinitionRevision{}, fmt.Errorf("skillstore: decode proficiency_scale: %w", err)
		}
	}
	ref := values.EntityRef{Tenant: values.TenantId(tenant), Kind: skill.KindSkill, Id: skillRef}
	parents := make([]values.EntityRef, 0, len(wire.ParentRefs))
	for _, parent := range wire.ParentRefs {
		parents = append(parents, values.EntityRef{Tenant: values.TenantId(tenant), Kind: skill.KindSkill, Id: parent})
	}
	rev, err := values.NewSequenceRevision("skill/"+skillRef, uint64(revision))
	if err != nil {
		return skill.SkillDefinitionRevision{}, err
	}
	var supersedesRevision values.RevisionToken
	if supersedes != 0 {
		supersedesRevision, err = values.NewSequenceRevision("skill/"+skillRef, uint64(supersedes))
		if err != nil {
			return skill.SkillDefinitionRevision{}, err
		}
	}
	return skill.NewSkillDefinition(skill.SkillDefinitionRevision{
		SkillRef: ref, Revision: rev, Supersedes: supersedesRevision, Name: name,
		ParentRefs: parents, Aliases: wire.Aliases, ProficiencyScale: wire.ProficiencyScale,
		CanonicalDigest: domainDigest(digest),
	})
}

// SaveOntology stores one ontology revision and its immutable definition rows.
func (s *Store) SaveOntology(ctx context.Context, tenant skill.TenantID, ontology skill.SkillOntologyRevision) error {
	if err := checkTenant(tenant); err != nil {
		return err
	}
	if err := validateTenantRef(tenant, ontology.OntologyID, "ontology_id"); err != nil {
		return err
	}
	for i, definition := range ontology.Skills {
		if definition.CanonicalDigest == "" {
			normalized, err := skill.NewSkillDefinition(definition)
			if err != nil {
				return err
			}
			ontology.Skills[i] = normalized
		}
	}
	if err := ontology.Validate(); err != nil {
		return err
	}
	ontologyID, err := entityID(ontology.OntologyID, values.Kind("skill_ontology"))
	if err != nil {
		return err
	}
	ontologyRevision, err := sequence(ontology.Revision)
	if err != nil {
		return err
	}
	if ontology.CanonicalDigest == "" {
		ontology, err = skill.NewSkillOntology(ontology)
		if err != nil {
			return err
		}
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var latest int64
		err := tx.QueryRow(ctx, `
			SELECT revision FROM skill_ontology_revision
			WHERE tenant_id=$1 AND ontology_id=$2
			ORDER BY revision DESC LIMIT 1`, tenantID, ontologyID).Scan(&latest)
		if errors.Is(err, dbport.ErrNoRows) {
			if ontologyRevision != 1 {
				return refuse(skill.ErrStaleRevision.Error(), "revision", "the first ontology revision must be revision 1", skill.ErrStaleRevision)
			}
		} else if err != nil {
			return fmt.Errorf("skillstore: inspect ontology chain: %w", err)
		} else if int64(ontologyRevision) <= latest {
			if int64(ontologyRevision) == latest {
				return refuse(skill.ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", skill.ErrDuplicateRevision)
			}
			return refuse(skill.ErrStaleRevision.Error(), "revision", "ontology revision is behind the current chain", skill.ErrStaleRevision)
		} else if int64(ontologyRevision) != latest+1 {
			return refuse(skill.ErrStaleRevision.Error(), "revision", "ontology revision does not extend the current chain", skill.ErrStaleRevision)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO skill_ontology_revision (row_id, tenant_id, ontology_id, revision, canonical_digest)
			VALUES ($1,$2,$3,$4,$5)`, uuid.New(), tenantID, ontologyID, int64(ontologyRevision), storedDigest(ontology.CanonicalDigest)); err != nil {
			return mapRevisionError(err)
		}
		for _, definition := range ontology.Skills {
			if err := saveDefinition(ctx, tx, tenantID, ontologyID, definition); err != nil {
				return err
			}
		}
		return nil
	})
}

func saveDefinition(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, ontologyID string, definition skill.SkillDefinitionRevision) error {
	ref := definition.SkillRef
	if ref.Validate() != nil {
		ref = definition.SkillID
	}
	skillID, err := entityID(ref, skill.KindSkill)
	if err != nil {
		return err
	}
	revision, err := sequence(definition.Revision)
	if err != nil {
		return err
	}
	var supersedes any
	if definition.Supersedes.IsSpecified() {
		if definition.Supersedes.Stream() != definition.Revision.Stream() {
			return refuse(skill.ErrStaleRevision.Error(), "supersedes", "supersedes must name a revision of the same skill", skill.ErrStaleRevision)
		}
		value, ok := definition.Supersedes.Sequence()
		if !ok || value == 0 {
			return refuse(skill.ErrStaleRevision.Error(), "supersedes", "supersedes must be a sequence revision", skill.ErrStaleRevision)
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM skill_definition_revision WHERE tenant_id=$1 AND skill_ref=$2 AND revision=$3)`, tenantID, skillID, int64(value)).Scan(&exists); err != nil {
			return fmt.Errorf("skillstore: inspect definition predecessor: %w", err)
		}
		if !exists {
			return refuse(skill.ErrStaleRevision.Error(), "supersedes", "superseded definition revision was not found", skill.ErrStaleRevision)
		}
		supersedes = int64(value)
	}
	wire, err := marshalDefinition(definition)
	if err != nil {
		return err
	}
	parents, err := json.Marshal(wire.ParentRefs)
	if err != nil {
		return err
	}
	aliases, err := json.Marshal(wire.Aliases)
	if err != nil {
		return err
	}
	scale, err := json.Marshal(wire.ProficiencyScale)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO skill_definition_revision (
			row_id, tenant_id, skill_ref, ontology_ref, revision, supersedes, name,
			parent_refs, aliases, proficiency_scale, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb,$11)
		ON CONFLICT DO NOTHING`, uuid.New(), tenantID, skillID, ontologyID, int64(revision), supersedes,
		definition.Name, string(parents), string(aliases), string(scale), storedDigest(definition.CanonicalDigest))
	if err != nil {
		return mapRevisionError(err)
	}
	return nil
}

func mapRevisionError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return refuse(skill.ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", skill.ErrDuplicateRevision)
	}
	return fmt.Errorf("skillstore: store revision: %w", err)
}

// LoadOntology loads one ontology revision and its definitions.
func (s *Store) LoadOntology(ctx context.Context, tenant skill.TenantID, ontologyID values.EntityRef, revision uint64) (skill.SkillOntologyRevision, error) {
	var out skill.SkillOntologyRevision
	if err := validateTenantRef(tenant, ontologyID, "ontology_id"); err != nil {
		return out, err
	}
	id, err := entityID(ontologyID, values.Kind("skill_ontology"))
	if err != nil {
		return out, err
	}
	if revision == 0 {
		return out, refuse(skill.ErrNotFound.Error(), "revision", "revision must be positive", skill.ErrNotFound)
	}
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var storedRevision int64
		var digest string
		if err := tx.QueryRow(ctx, `SELECT revision, canonical_digest FROM skill_ontology_revision WHERE tenant_id=$1 AND ontology_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&storedRevision, &digest); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return refuse(skill.ErrNotFound.Error(), "ontology_id", "ontology revision was not found", skill.ErrNotFound)
			}
			return fmt.Errorf("skillstore: load ontology: %w", err)
		}
		var revErr error
		out.Revision, revErr = values.NewSequenceRevision("ontology", uint64(storedRevision))
		if revErr != nil {
			return revErr
		}
		out.OntologyID = ontologyID
		out.CanonicalDigest = domainDigest(digest)
		rows, err := tx.Query(ctx, `
			SELECT skill_ref, revision, supersedes, name, parent_refs::text, aliases::text,
				proficiency_scale::text, canonical_digest
			FROM skill_definition_revision
			WHERE tenant_id=$1 AND ontology_ref=$2 ORDER BY skill_ref, revision`, tenantID, id)
		if err != nil {
			return fmt.Errorf("skillstore: list skill definitions: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var skillRef, name, parentJSON, aliasJSON, scaleJSON, definitionDigest string
			var definitionRevision int64
			var supersedes *int64
			if err := rows.Scan(&skillRef, &definitionRevision, &supersedes, &name, &parentJSON, &aliasJSON, &scaleJSON, &definitionDigest); err != nil {
				return fmt.Errorf("skillstore: scan skill definition: %w", err)
			}
			var predecessor int64
			if supersedes != nil {
				predecessor = *supersedes
			}
			definition, err := unmarshalDefinition(tenant.String(), skillRef, id, definitionRevision, predecessor, name, parentJSON, aliasJSON, scaleJSON, definitionDigest)
			if err != nil {
				return err
			}
			out.Skills = append(out.Skills, definition)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("skillstore: list skill definitions: %w", err)
		}
		return nil
	})
	if err != nil {
		return skill.SkillOntologyRevision{}, err
	}
	return out, nil
}

// AppendEvidence records one immutable worker-skill evidence event.
func (s *Store) AppendEvidence(ctx context.Context, tenant skill.TenantID, evidence skill.WorkerSkillEvidence, eventSequence uint64) error {
	if err := validateTenantRef(tenant, evidence.EvidenceID, "evidence_id"); err != nil {
		return err
	}
	if err := evidence.Validate(); err != nil {
		return err
	}
	if eventSequence == 0 {
		return refuse("SKILL_INVALID_EVENT_SEQUENCE", "event_sequence", "event sequence must be positive", skill.ErrStoreRefused)
	}
	workerID, err := uuid.Parse(evidence.Worker.Id)
	if err != nil {
		return refuse("SKILL_INVALID_WORKER", "worker_ref", "worker id must be a UUID", skill.ErrStoreRefused)
	}
	skillID, err := entityID(evidence.SkillRef, skill.KindSkill)
	if err != nil {
		return err
	}
	evidenceID, err := entityID(evidence.EvidenceID, values.Kind("skill_evidence"))
	if err != nil {
		return err
	}
	from, to, err := intervalBounds(evidence.Effective)
	if err != nil {
		return err
	}
	level := any(nil)
	if evidence.Level > 0 {
		level = strconv.Itoa(evidence.Level)
	}
	proficiency := any(nil)
	if evidence.Proficiency != "" {
		proficiency = float64(proficiencyRank(evidence.Proficiency))
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO worker_skill_evidence (
				row_id, tenant_id, evidence_id, worker_ref, skill_ref, level, proficiency,
				evidence_kind, evidence_ref, verified, disputed, effective_from, effective_to,
				canonical_digest, event_sequence)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
			ON CONFLICT DO NOTHING`, uuid.New(), tenantID, evidenceID, workerID, skillID, level, proficiency,
			string(evidence.EvidenceKind), evidence.EvidenceRef, evidence.Verified, evidence.Disputed,
			from, to, storedDigest(evidence.CanonicalDigest), int64(eventSequence))
		if err != nil {
			return mapEvidenceError(err)
		}
		return nil
	})
}

func mapEvidenceError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return refuse(skill.ErrDuplicateEvidence.Error(), "event_sequence", "evidence event sequence is already stored", skill.ErrDuplicateEvidence)
	}
	return fmt.Errorf("skillstore: append evidence: %w", err)
}

func proficiencyRank(value skill.ProficiencyLevel) int {
	for i, candidate := range skill.DefaultProficiencyScale() {
		if candidate == value {
			return i + 1
		}
	}
	return 0
}

func intervalBounds(interval values.EffectiveInterval) (*time.Time, *time.Time, error) {
	if err := interval.Validate(); err != nil {
		return nil, nil, err
	}
	if start, ok := interval.StartDate(); ok {
		from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
		var to *time.Time
		if end, hasEnd := interval.EndDate(); hasEnd {
			value := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
			to = &value
		}
		return &from, to, nil
	}
	if start, ok := interval.StartInstant(); ok {
		from := start.Time()
		var to *time.Time
		if end, hasEnd := interval.EndInstant(); hasEnd {
			value := end.Time()
			to = &value
		}
		return &from, to, nil
	}
	return nil, nil, values.ErrIntervalUnset
}

func intervalFromBounds(from, to *time.Time) (values.EffectiveInterval, error) {
	if from == nil {
		return values.EffectiveInterval{}, fmt.Errorf("skillstore: effective_from is required")
	}
	fromUTC := from.UTC()
	start, err := values.NewLocalDate(fromUTC.Year(), fromUTC.Month(), fromUTC.Day())
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	calendar := values.CalendarRef{Ref: "gregorian", Version: "1"}
	if to == nil {
		return values.NewOpenLocalDateInterval(start, calendar)
	}
	toUTC := to.UTC()
	end, err := values.NewLocalDate(toUTC.Year(), toUTC.Month(), toUTC.Day())
	if err != nil {
		return values.EffectiveInterval{}, err
	}
	return values.NewLocalDateInterval(start, end, calendar)
}

// EvidenceAt implements the read port used by the skill resolver.
func (s *Store) EvidenceAt(ctx context.Context, query skill.EvidenceQuery) ([]skill.WorkerSkillEvidence, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	workerID, err := uuid.Parse(query.Worker.Id)
	if err != nil {
		return nil, refuse("SKILL_INVALID_WORKER", "worker_ref", "worker id must be a UUID", skill.ErrStoreRefused)
	}
	var out []skill.WorkerSkillEvidence
	err = s.withTenant(ctx, query.Worker.Tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		rows, err := tx.Query(ctx, `
			SELECT evidence_id, skill_ref, level, proficiency, evidence_kind, evidence_ref,
				verified, disputed, effective_from, effective_to, canonical_digest
			FROM worker_skill_evidence
			WHERE tenant_id=$1 AND worker_ref=$2 ORDER BY event_sequence`, tenantID, workerID)
		if err != nil {
			return fmt.Errorf("skillstore: list worker evidence: %w", err)
		}
		defer rows.Close()
		allowed := make(map[string]struct{}, len(query.SkillRefs))
		for _, ref := range query.SkillRefs {
			allowed[ref.Id] = struct{}{}
		}
		for rows.Next() {
			var evidenceID, skillID, evidenceKind, evidenceRef, digest string
			var level, proficiency *string
			var verified, disputed bool
			var from, to *time.Time
			if err := rows.Scan(&evidenceID, &skillID, &level, &proficiency, &evidenceKind, &evidenceRef, &verified, &disputed, &from, &to, &digest); err != nil {
				return fmt.Errorf("skillstore: scan worker evidence: %w", err)
			}
			if len(allowed) > 0 {
				if _, ok := allowed[skillID]; !ok {
					continue
				}
			}
			value, err := decodeEvidence(query.Worker.Tenant.String(), evidenceID, workerID, skillID, level, proficiency, evidenceKind, evidenceRef, verified, disputed, from, to, digest)
			if err != nil {
				return err
			}
			out = append(out, value)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("skillstore: list worker evidence: %w", err)
		}
		return nil
	})
	return out, err
}

func decodeEvidence(tenant, evidenceID string, workerID uuid.UUID, skillID string, level, proficiency *string, evidenceKind, evidenceRef string, verified, disputed bool, from, to *time.Time, digest string) (skill.WorkerSkillEvidence, error) {
	worker := values.EntityRef{Tenant: values.TenantId(tenant), Kind: skill.KindWorker, Id: workerID.String()}
	ref := values.EntityRef{Tenant: values.TenantId(tenant), Kind: skill.KindSkill, Id: skillID}
	id := values.EntityRef{Tenant: values.TenantId(tenant), Kind: values.Kind("skill_evidence"), Id: evidenceID}
	interval, err := intervalFromBounds(from, to)
	if err != nil {
		return skill.WorkerSkillEvidence{}, err
	}
	value := skill.WorkerSkillEvidence{EvidenceID: id, Worker: worker, SkillRef: ref, EvidenceKind: skill.EvidenceKind(evidenceKind), EvidenceRef: evidenceRef, Verified: verified, Disputed: disputed, Effective: interval, CanonicalDigest: domainDigest(digest)}
	if level != nil && *level != "" {
		value.Level, err = strconv.Atoi(*level)
		if err != nil {
			return skill.WorkerSkillEvidence{}, fmt.Errorf("skillstore: decode evidence level: %w", err)
		}
	}
	if proficiency != nil && *proficiency != "" {
		var rank float64
		rank, err = strconv.ParseFloat(*proficiency, 64)
		if err != nil {
			return skill.WorkerSkillEvidence{}, fmt.Errorf("skillstore: decode evidence proficiency: %w", err)
		}
		if rank >= 1 && rank <= 5 {
			value.Proficiency = skill.DefaultProficiencyScale()[int(rank)-1]
		}
	}
	if err := value.Validate(); err != nil {
		return skill.WorkerSkillEvidence{}, fmt.Errorf("skillstore: stored evidence is invalid: %w", err)
	}
	return value, nil
}
