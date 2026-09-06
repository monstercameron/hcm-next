package skill

import (
	"context"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// EvidenceQuery is the bounded read request for worker skill evidence.
type EvidenceQuery struct {
	Worker    values.EntityRef
	AsOf      values.LocalDate
	SkillRefs []values.EntityRef
}

func (q EvidenceQuery) Validate() error {
	if err := q.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrInvalidResolution, err)
	}
	if q.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: worker kind %q is not worker", ErrInvalidResolution, q.Worker.Kind)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as_of: %v", ErrInvalidResolution, err)
	}
	for i, ref := range q.SkillRefs {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: skill_refs[%d]: %v", ErrInvalidResolution, i, err)
		}
		if ref.Kind != KindSkill {
			return fmt.Errorf("%w: skill_refs[%d] kind %q is not skill", ErrInvalidResolution, i, ref.Kind)
		}
		if ref.Tenant != q.Worker.Tenant {
			return fmt.Errorf("%w: skill_refs[%d] tenant differs", ErrInvalidResolution, i)
		}
	}
	return nil
}

// SkillEvidenceReader is the only source of worker assertions used by a
// resolver. It is a read port; it has no persistence or mutation operations.
type SkillEvidenceReader interface {
	EvidenceAt(context.Context, EvidenceQuery) ([]WorkerSkillEvidence, error)
}

// EvidenceReader is a vocabulary alias for callers that prefer the shorter
// port name.
type EvidenceReader = SkillEvidenceReader

// FakeSkillEvidenceReader is a deterministic in-memory port for unit tests
// and composition roots. The resolver still validates every returned record.
type FakeSkillEvidenceReader struct {
	Evidence []WorkerSkillEvidence
	Err      error
}

func (f FakeSkillEvidenceReader) EvidenceAt(ctx context.Context, q EvidenceQuery) ([]WorkerSkillEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	if f.Err != nil {
		return nil, f.Err
	}
	selected := make([]WorkerSkillEvidence, 0, len(f.Evidence))
	allowed := make(map[string]struct{}, len(q.SkillRefs))
	for _, ref := range q.SkillRefs {
		allowed[ref.String()] = struct{}{}
	}
	for _, evidence := range f.Evidence {
		if evidence.Worker != q.Worker {
			continue
		}
		if len(allowed) != 0 {
			if _, ok := allowed[evidence.SkillRef.String()]; !ok {
				continue
			}
		}
		selected = append(selected, evidence)
	}
	return append([]WorkerSkillEvidence(nil), selected...), nil
}

// InMemoryEvidenceReader is an alias retained for explicit port naming.
type InMemoryEvidenceReader = FakeSkillEvidenceReader

// ResolveRequest pins the ontology and equivalence revision used for a read.
type ResolveRequest struct {
	Worker       values.EntityRef
	AsOf         values.LocalDate
	SkillRefs    []values.EntityRef
	Ontology     SkillOntologyRevision
	Equivalences []EquivalenceRule
}

func (r ResolveRequest) Validate() error {
	if err := (EvidenceQuery{Worker: r.Worker, AsOf: r.AsOf, SkillRefs: r.SkillRefs}).Validate(); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(r.SkillRefs))
	for i, ref := range r.SkillRefs {
		if _, ok := seen[ref.String()]; ok {
			return fmt.Errorf("%w: skill_refs[%d] is duplicated", ErrInvalidResolution, i)
		}
		seen[ref.String()] = struct{}{}
	}
	if err := r.Ontology.Validate(); err != nil {
		return fmt.Errorf("%w: ontology: %v", ErrInvalidResolution, err)
	}
	if err := validateEquivalences(r.Ontology, r.Equivalences); err != nil {
		return fmt.Errorf("%w: equivalences: %v", ErrInvalidResolution, err)
	}
	known := make(map[string]struct{}, len(r.Ontology.Skills))
	for _, definition := range r.Ontology.Skills {
		known[definition.ref().String()] = struct{}{}
	}
	for i, ref := range r.SkillRefs {
		if _, ok := known[ref.String()]; !ok {
			return fmt.Errorf("%w: skill_refs[%d] is not in ontology", ErrInvalidResolution, i)
		}
	}
	return nil
}

// Resolver evaluates evidence against one pinned ontology and equivalence set.
type Resolver struct {
	Reader       SkillEvidenceReader
	Ontology     SkillOntologyRevision
	Equivalences []EquivalenceRule
}

// NewResolver accepts either (reader) or (ontology, equivalences, reader).
// The variadic form keeps composition roots concise while allowing a pinned
// resolver to satisfy career's read-only port.
func NewResolver(args ...any) Resolver {
	var out Resolver
	for _, arg := range args {
		switch value := arg.(type) {
		case SkillEvidenceReader:
			out.Reader = value
		case SkillOntologyRevision:
			out.Ontology = value
		case []EquivalenceRule:
			out.Equivalences = append([]EquivalenceRule(nil), value...)
		}
	}
	return out
}

func NewPinnedResolver(ontology SkillOntologyRevision, equivalences []EquivalenceRule, reader SkillEvidenceReader) Resolver {
	return Resolver{Reader: reader, Ontology: ontology, Equivalences: append([]EquivalenceRule(nil), equivalences...)}
}

// ProficiencyResult is the effective outcome for a skill. Level is the rank
// in the selected ontology scale; Proficiency is its closed vocabulary name.
type ProficiencyResult struct {
	SkillRef       values.EntityRef
	Level          int
	Proficiency    ProficiencyLevel
	Status         EvidenceStatus
	EvidenceCount  int
	ViaEquivalence bool
}

// Resolution is detached from the reader and safe to retain as an audit
// input. Evidence references are intentionally not copied into it.
type Resolution struct {
	Worker          values.EntityRef
	AsOf            values.LocalDate
	OntologyDigest  string
	Proficiencies   []ProficiencyResult
	CanonicalDigest string
}

func (r Resolution) Validate() error {
	if err := r.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrInvalidResolution, err)
	}
	if err := r.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as_of: %v", ErrInvalidResolution, err)
	}
	if r.OntologyDigest == "" || len(r.Proficiencies) == 0 {
		return fmt.Errorf("%w: ontology_digest and proficiencies are required", ErrInvalidResolution)
	}
	seen := make(map[string]struct{}, len(r.Proficiencies))
	for i, p := range r.Proficiencies {
		if err := p.SkillRef.Validate(); err != nil {
			return fmt.Errorf("%w: proficiencies[%d].skill_ref: %v", ErrInvalidResolution, i, err)
		}
		if p.SkillRef.Tenant != r.Worker.Tenant {
			return fmt.Errorf("%w: proficiencies[%d].skill_ref tenant differs", ErrInvalidResolution, i)
		}
		if !p.Status.Valid() {
			return fmt.Errorf("%w: proficiencies[%d].status is invalid", ErrInvalidResolution, i)
		}
		if p.Level < 0 || p.EvidenceCount < 0 {
			return fmt.Errorf("%w: proficiencies[%d] has negative measure", ErrInvalidResolution, i)
		}
		if p.Status == StatusUnknown && (p.Level != 0 || p.EvidenceCount != 0) {
			return fmt.Errorf("%w: unknown proficiency has measures", ErrInvalidResolution)
		}
		if _, ok := seen[p.SkillRef.String()]; ok {
			return fmt.Errorf("%w: duplicate skill result %s", ErrInvalidResolution, p.SkillRef)
		}
		seen[p.SkillRef.String()] = struct{}{}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidResolution)
	}
	return nil
}

func (r Resolution) canonicalBody() []byte {
	if err := r.validateWithoutDigest(); err != nil {
		return nil
	}
	items := append([]ProficiencyResult(nil), r.Proficiencies...)
	sort.Slice(items, func(i, j int) bool { return items[i].SkillRef.String() < items[j].SkillRef.String() })
	w := canonicalbytes.New("hcmnext.domains.skill.Resolution", schemaVersion).
		Value("worker", r.Worker).Value("as_of", r.AsOf).String("ontology_digest", r.OntologyDigest).Count("proficiencies", len(items))
	for _, p := range items {
		w.Value("skill_ref", p.SkillRef).Int("level", int64(p.Level)).String("proficiency", string(p.Proficiency)).
			String("status", string(p.Status)).Int("evidence_count", int64(p.EvidenceCount)).Bool("via_equivalence", p.ViaEquivalence)
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (r Resolution) validateWithoutDigest() error {
	digest := r.CanonicalDigest
	r.CanonicalDigest = ""
	_ = digest
	return r.Validate()
}
func (r Resolution) computedDigest() string { return canonicalbytes.Digest(r.canonicalBody()) }
func (r Resolution) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}
func (r Resolution) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r Resolution) Explain() string { return Explain(r) }

type evidenceCandidate struct {
	level          int
	status         EvidenceStatus
	evidenceCount  int
	viaEquivalence bool
}

func statusFor(e WorkerSkillEvidence, active bool) EvidenceStatus {
	if e.Disputed {
		return StatusDisputed
	}
	if !active {
		return StatusExpired
	}
	if e.Verified && e.EvidenceKind != EvidenceSelfReport {
		return StatusVerified
	}
	return StatusAsserted
}

func containsDate(interval values.EffectiveInterval, date values.LocalDate) bool {
	ok, err := interval.ContainsDate(date)
	return err == nil && ok
}

func expiredAt(interval values.EffectiveInterval, date values.LocalDate) bool {
	end, hasEnd := interval.EndDate()
	return hasEnd && end.Compare(date) <= 0
}

func better(a, b evidenceCandidate) bool {
	rank := func(s EvidenceStatus) int {
		switch s {
		case StatusVerified:
			return 5
		case StatusAsserted:
			return 4
		case StatusDisputed:
			return 3
		case StatusExpired:
			return 2
		default:
			return 1
		}
	}
	if rank(a.status) != rank(b.status) {
		return rank(a.status) > rank(b.status)
	}
	if a.level != b.level {
		return a.level > b.level
	}
	return a.evidenceCount > b.evidenceCount
}

// Resolve returns one result per requested skill (or per ontology skill when
// SkillRefs is empty). Direct evidence wins over equivalent evidence at the
// same status and level; expired evidence never becomes effective.
func (r Resolver) Resolve(ctx context.Context, req ResolveRequest) (Resolution, error) {
	if req.Ontology.Validate() != nil && r.Ontology.Validate() == nil {
		req.Ontology = r.Ontology
		if len(req.Equivalences) == 0 {
			req.Equivalences = append([]EquivalenceRule(nil), r.Equivalences...)
		}
	}
	if err := req.Validate(); err != nil {
		return Resolution{}, err
	}
	if r.Reader == nil {
		return Resolution{}, fmt.Errorf("%w: evidence reader is required", ErrInvalidResolution)
	}
	evidence, err := r.Reader.EvidenceAt(ctx, EvidenceQuery{Worker: req.Worker, AsOf: req.AsOf, SkillRefs: nil})
	if err != nil {
		return Resolution{}, fmt.Errorf("%w: %v", ErrReaderFailed, err)
	}
	for i, item := range evidence {
		if err := item.Validate(); err != nil {
			return Resolution{}, fmt.Errorf("%w: evidence[%d]: %v", ErrInvalidResolution, i, err)
		}
	}
	scales := make(map[string][]ProficiencyLevel, len(req.Ontology.Skills))
	for _, definition := range req.Ontology.Skills {
		scales[definition.ref().String()] = scaleOrDefault(definition.ProficiencyScale)
	}
	targets := append([]values.EntityRef(nil), req.SkillRefs...)
	if len(targets) == 0 {
		for _, definition := range req.Ontology.Skills {
			targets = append(targets, definition.ref())
		}
	}
	result := Resolution{Worker: req.Worker, AsOf: req.AsOf, OntologyDigest: req.Ontology.CanonicalDigest}
	for _, target := range targets {
		best, found := evidenceCandidate{}, false
		for _, item := range evidence {
			if item.SkillRef != target {
				continue
			}
			rank, levelErr := item.effectiveLevel(scales[item.SkillRef.String()])
			if levelErr != nil {
				return Resolution{}, levelErr
			}
			active := containsDate(item.Effective, req.AsOf)
			candidate := evidenceCandidate{level: rank, status: statusFor(item, active), evidenceCount: 1}
			if !found || better(candidate, best) {
				best, found = candidate, true
			}
		}
		// Traverse reviewed equivalences from every source skill. Validation has
		// already guaranteed acyclicity, but the visited set makes the evaluator
		// defensive if a caller changes the rules during composition.
		for _, rule := range req.Equivalences {
			if rule.TargetSkill != target || !containsDate(rule.Effective, req.AsOf) {
				continue
			}
			for _, item := range evidence {
				if item.SkillRef != rule.SourceSkill {
					continue
				}
				sourceLevel, levelErr := item.effectiveLevel(scales[item.SkillRef.String()])
				if levelErr != nil {
					return Resolution{}, levelErr
				}
				if sourceLevel < rule.SourceLevel {
					continue
				}
				active := containsDate(item.Effective, req.AsOf)
				candidate := evidenceCandidate{level: rule.TargetLevel, status: statusFor(item, active), evidenceCount: 1, viaEquivalence: true}
				if !found || better(candidate, best) {
					best, found = candidate, true
				}
			}
		}
		out := ProficiencyResult{SkillRef: target, Status: StatusUnknown}
		if found {
			out.Level, out.Proficiency, out.Status, out.EvidenceCount, out.ViaEquivalence = best.level, levelFromRank(scales[target.String()], best.level), best.status, best.evidenceCount, best.viaEquivalence
		}
		result.Proficiencies = append(result.Proficiencies, out)
	}
	result.CanonicalDigest = canonicalbytes.Digest(result.canonicalBody())
	return result, nil
}

// Resolve is also available as a package-level port function.
func Resolve(ctx context.Context, reader SkillEvidenceReader, req ResolveRequest) (Resolution, error) {
	return (Resolver{Reader: reader}).Resolve(ctx, req)
}
