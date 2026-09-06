package skill

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	// ErrStoreRefused is the common classification for persistence-boundary refusals.
	ErrStoreRefused = errors.New("skill: store operation refused")
	// ErrDuplicateRevision identifies an immutable revision identity already stored.
	ErrDuplicateRevision = errors.New("SKILL_DUPLICATE_REVISION")
	// ErrStaleRevision identifies a revision that does not extend its predecessor.
	ErrStaleRevision = errors.New("SKILL_STALE_REVISION")
	// ErrDuplicateEvidence identifies an occupied evidence event sequence.
	ErrDuplicateEvidence = errors.New("SKILL_DUPLICATE_EVIDENCE")
	// ErrNotFound identifies an absent tenant-scoped value.
	ErrNotFound = errors.New("SKILL_NOT_FOUND")
)

// RefusalError preserves a stable code and the field that caused a store
// refusal while remaining compatible with errors.Is.
type RefusalError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *RefusalError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("skill: %s: %s", e.Code, e.Reason)
	}
	return fmt.Sprintf("skill: %s: %s: %s", e.Code, e.Field, e.Reason)
}

func (e *RefusalError) Unwrap() error { return e.Cause }

func (e *RefusalError) Is(target error) bool {
	return target == ErrStoreRefused || target == e.Cause
}

func refuse(code, field, reason string, cause error) error {
	return &RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

// Store is the tenant-aware persistence port for skill ontology revisions and
// worker evidence. Implementations preserve immutable revisions and evidence;
// nothing is updated in place.
type Store interface {
	SaveOntology(context.Context, TenantID, SkillOntologyRevision) error
	LoadOntology(context.Context, TenantID, values.EntityRef, uint64) (SkillOntologyRevision, error)
	AppendEvidence(context.Context, TenantID, WorkerSkillEvidence, uint64) error
	EvidenceAt(context.Context, EvidenceQuery) ([]WorkerSkillEvidence, error)
}

// TenantID keeps the domain port independent of a UUID implementation.
type TenantID interface{ String() string }

// MemoryStore is the pure reference implementation of Store used by kernel
// composition roots and tests that do not need PostgreSQL.
type MemoryStore struct {
	mu         sync.RWMutex
	ontologies map[string]map[uint64]SkillOntologyRevision
	evidence   map[string]map[uint64]WorkerSkillEvidence
}

// NewMemoryStore returns an empty in-memory skill store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		ontologies: make(map[string]map[uint64]SkillOntologyRevision),
		evidence:   make(map[string]map[uint64]WorkerSkillEvidence),
	}
}

func checkStoreContext(ctx context.Context) error {
	if ctx == nil {
		return refuse("SKILL_INVALID_CONTEXT", "context", "context is required", ErrStoreRefused)
	}
	return ctx.Err()
}

func checkStoreTenant(tenant TenantID) error {
	if tenant == nil || tenant.String() == "" {
		return refuse("SKILL_INVALID_TENANT", "tenant_id", "tenant id is required", ErrStoreRefused)
	}
	return nil
}

func tenantKey(tenant TenantID, id string) string { return tenant.String() + "\x00" + id }

func normalizeOntology(ontology SkillOntologyRevision) (SkillOntologyRevision, error) {
	if ontology.CanonicalDigest == "" {
		return NewSkillOntology(ontology)
	}
	if err := ontology.Validate(); err != nil {
		return SkillOntologyRevision{}, err
	}
	ontology.Skills = append([]SkillDefinitionRevision(nil), ontology.Skills...)
	return ontology, nil
}

func normalizeEvidence(evidence WorkerSkillEvidence) (WorkerSkillEvidence, error) {
	if evidence.CanonicalDigest == "" {
		return NewWorkerSkillEvidence(evidence)
	}
	if err := evidence.Validate(); err != nil {
		return WorkerSkillEvidence{}, err
	}
	return evidence, nil
}

func revisionNumber(revision values.RevisionToken) (uint64, error) {
	if err := revision.Validate(); err != nil {
		return 0, err
	}
	sequence, ok := revision.Sequence()
	if !ok || sequence == 0 {
		return 0, refuse(ErrStaleRevision.Error(), "revision", "skill persistence requires a positive sequence revision", ErrStaleRevision)
	}
	return sequence, nil
}

func validateNextRevision(revisions map[uint64]SkillOntologyRevision, revision uint64) error {
	if _, exists := revisions[revision]; exists {
		return refuse(ErrDuplicateRevision.Error(), "revision", "revision identity is already stored", ErrDuplicateRevision)
	}
	if len(revisions) == 0 {
		if revision != 1 {
			return refuse(ErrStaleRevision.Error(), "revision", "the first ontology revision must be revision 1", ErrStaleRevision)
		}
		return nil
	}
	var latest uint64
	for candidate := range revisions {
		if candidate > latest {
			latest = candidate
		}
	}
	if revision != latest+1 {
		return refuse(ErrStaleRevision.Error(), "revision", "ontology revision does not extend the current chain", ErrStaleRevision)
	}
	return nil
}

func cloneOntology(ontology SkillOntologyRevision) SkillOntologyRevision {
	ontology.Skills = append([]SkillDefinitionRevision(nil), ontology.Skills...)
	for i := range ontology.Skills {
		ontology.Skills[i].ParentRefs = append([]values.EntityRef(nil), ontology.Skills[i].ParentRefs...)
		ontology.Skills[i].Aliases = append([]string(nil), ontology.Skills[i].Aliases...)
		ontology.Skills[i].ProficiencyScale = append([]ProficiencyLevel(nil), ontology.Skills[i].ProficiencyScale...)
	}
	return ontology
}

// SaveOntology implements Store.
func (s *MemoryStore) SaveOntology(ctx context.Context, tenant TenantID, ontology SkillOntologyRevision) error {
	if err := checkStoreContext(ctx); err != nil {
		return err
	}
	if err := checkStoreTenant(tenant); err != nil {
		return err
	}
	if ontology.OntologyID.Tenant.String() != tenant.String() {
		return refuse("SKILL_TENANT_MISMATCH", "ontology_id", "ontology is owned by another tenant", ErrStoreRefused)
	}
	ontology, err := normalizeOntology(ontology)
	if err != nil {
		return err
	}
	revision, err := revisionNumber(ontology.Revision)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantKey(tenant, ontology.OntologyID.Id)
	if s.ontologies == nil {
		s.ontologies = make(map[string]map[uint64]SkillOntologyRevision)
	}
	if s.ontologies[key] == nil {
		s.ontologies[key] = make(map[uint64]SkillOntologyRevision)
	}
	if err := validateNextRevision(s.ontologies[key], revision); err != nil {
		return err
	}
	for _, definition := range ontology.Skills {
		if definition.Supersedes.IsSpecified() && definition.Supersedes.Stream() != definition.Revision.Stream() {
			return refuse(ErrStaleRevision.Error(), "supersedes", "supersedes must name a revision of the same skill", ErrStaleRevision)
		}
	}
	s.ontologies[key][revision] = cloneOntology(ontology)
	return nil
}

// LoadOntology implements Store.
func (s *MemoryStore) LoadOntology(ctx context.Context, tenant TenantID, ontologyID values.EntityRef, revision uint64) (SkillOntologyRevision, error) {
	if err := checkStoreContext(ctx); err != nil {
		return SkillOntologyRevision{}, err
	}
	if err := checkStoreTenant(tenant); err != nil {
		return SkillOntologyRevision{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	otho, ok := s.ontologies[tenantKey(tenant, ontologyID.Id)][revision]
	if !ok {
		return SkillOntologyRevision{}, refuse(ErrNotFound.Error(), "ontology_id", "ontology revision was not found", ErrNotFound)
	}
	return cloneOntology(otho), nil
}

// AppendEvidence implements Store.
func (s *MemoryStore) AppendEvidence(ctx context.Context, tenant TenantID, evidence WorkerSkillEvidence, eventSequence uint64) error {
	if err := checkStoreContext(ctx); err != nil {
		return err
	}
	if err := checkStoreTenant(tenant); err != nil {
		return err
	}
	if eventSequence == 0 {
		return refuse("SKILL_INVALID_EVENT_SEQUENCE", "event_sequence", "event sequence must be positive", ErrStoreRefused)
	}
	evidence, err := normalizeEvidence(evidence)
	if err != nil {
		return err
	}
	if evidence.Worker.Tenant.String() != tenant.String() || evidence.SkillRef.Tenant.String() != tenant.String() {
		return refuse("SKILL_TENANT_MISMATCH", "tenant_id", "evidence is owned by another tenant", ErrStoreRefused)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantKey(tenant, evidence.Worker.String()+"\x00"+evidence.SkillRef.String())
	if s.evidence == nil {
		s.evidence = make(map[string]map[uint64]WorkerSkillEvidence)
	}
	if s.evidence[key] == nil {
		s.evidence[key] = make(map[uint64]WorkerSkillEvidence)
	}
	if _, exists := s.evidence[key][eventSequence]; exists {
		return refuse(ErrDuplicateEvidence.Error(), "event_sequence", "evidence event sequence is already stored", ErrDuplicateEvidence)
	}
	s.evidence[key][eventSequence] = evidence
	return nil
}

// EvidenceAt implements SkillEvidenceReader and returns detached evidence.
func (s *MemoryStore) EvidenceAt(ctx context.Context, query EvidenceQuery) ([]WorkerSkillEvidence, error) {
	if err := checkStoreContext(ctx); err != nil {
		return nil, err
	}
	if err := query.Validate(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := make(map[string]struct{}, len(query.SkillRefs))
	for _, ref := range query.SkillRefs {
		allowed[ref.String()] = struct{}{}
	}
	var out []WorkerSkillEvidence
	for _, records := range s.evidence {
		for _, evidence := range records {
			if evidence.Worker != query.Worker || (len(allowed) > 0 && !containsSkill(allowed, evidence.SkillRef.String())) {
				continue
			}
			out = append(out, evidence)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EvidenceID.String() < out[j].EvidenceID.String() })
	return append([]WorkerSkillEvidence(nil), out...), nil
}

func containsSkill(allowed map[string]struct{}, ref string) bool {
	_, ok := allowed[ref]
	return ok
}

var _ Store = (*MemoryStore)(nil)
var _ SkillEvidenceReader = (*MemoryStore)(nil)
