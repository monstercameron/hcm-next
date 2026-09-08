package skill

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrConformance    = errors.New("skill: conformance failure")
	ErrCorrection     = errors.New("skill: invalid evidence correction")
	ErrConsumerParity = errors.New("skill: consumer parity mismatch")
	ErrPinnedEvidence = errors.New("skill: evidence revision is not pinned")
)

// Consumer identifies the four consumers that must agree on skill meaning.
type Consumer string

const (
	ConsumerQualification Consumer = "QUALIFICATION"
	ConsumerRecruiting    Consumer = "RECRUITING"
	ConsumerLearning      Consumer = "LEARNING"
	ConsumerPlanning      Consumer = "PLANNING"
)

func (c Consumer) valid() bool {
	return c == ConsumerQualification || c == ConsumerRecruiting || c == ConsumerLearning || c == ConsumerPlanning
}

// EvidenceRevision is a content-addressed, immutable evidence snapshot.
// Consumers must use the same digest; no consumer may silently re-read a
// different revision while producing a decision.
type EvidenceRevision struct {
	Sequence uint64
	Evidence []WorkerSkillEvidence
	Digest   string
}

func validateEvidenceLineage(evidence []WorkerSkillEvidence) error {
	byID := make(map[string]WorkerSkillEvidence, len(evidence))
	for _, item := range evidence {
		byID[item.EvidenceID.String()] = item
	}
	successor := make(map[string]string, len(evidence))
	for _, item := range evidence {
		if item.Supersedes.Id == "" {
			continue
		}
		prior, ok := byID[item.Supersedes.String()]
		if !ok {
			return fmt.Errorf("%w: dangling supersedes reference", ErrPinnedEvidence)
		}
		if prior.Worker != item.Worker || prior.SkillRef != item.SkillRef {
			return fmt.Errorf("%w: successor must retain worker and skill", ErrPinnedEvidence)
		}
		priorStart, _ := prior.Effective.StartDate()
		successorStart, _ := item.Effective.StartDate()
		if successorStart.Compare(priorStart) < 0 {
			return fmt.Errorf("%w: successor cannot predate predecessor", ErrPinnedEvidence)
		}
		if _, exists := successor[prior.EvidenceID.String()]; exists {
			return fmt.Errorf("%w: evidence has multiple successors", ErrPinnedEvidence)
		}
		successor[prior.EvidenceID.String()] = item.EvidenceID.String()
	}
	for start := range byID {
		seen := map[string]struct{}{}
		for current := start; current != ""; current = successor[current] {
			if _, ok := seen[current]; ok {
				return fmt.Errorf("%w: correction cycle", ErrPinnedEvidence)
			}
			seen[current] = struct{}{}
		}
	}
	return nil
}

func NewEvidenceRevision(sequence uint64, evidence []WorkerSkillEvidence) (EvidenceRevision, error) {
	if sequence == 0 || len(evidence) == 0 {
		return EvidenceRevision{}, fmt.Errorf("%w: positive sequence and evidence are required", ErrPinnedEvidence)
	}
	out := EvidenceRevision{Sequence: sequence, Evidence: append([]WorkerSkillEvidence(nil), evidence...)}
	seen := make(map[string]struct{}, len(evidence))
	for i := range out.Evidence {
		if err := out.Evidence[i].Validate(); err != nil {
			return EvidenceRevision{}, fmt.Errorf("%w: evidence[%d]: %v", ErrPinnedEvidence, i, err)
		}
		if _, ok := seen[out.Evidence[i].EvidenceID.String()]; ok {
			return EvidenceRevision{}, fmt.Errorf("%w: duplicate evidence", ErrPinnedEvidence)
		}
		seen[out.Evidence[i].EvidenceID.String()] = struct{}{}
	}
	if err := validateEvidenceLineage(out.Evidence); err != nil {
		return EvidenceRevision{}, err
	}
	out.Digest = evidenceRevisionDigest(out.Sequence, out.Evidence)
	return out, nil
}

func evidenceRevisionDigest(sequence uint64, evidence []WorkerSkillEvidence) string {
	items := append([]WorkerSkillEvidence(nil), evidence...)
	sort.Slice(items, func(i, j int) bool { return items[i].EvidenceID.String() < items[j].EvidenceID.String() })
	w := canonicalbytes.New("hcmnext.domains.skill.EvidenceRevision", schemaVersion).Int("sequence", int64(sequence)).Count("evidence", len(items))
	for _, item := range items {
		w.Field("evidence", item.Canonical())
	}
	b, _ := w.Bytes()
	return canonicalbytes.Digest(b)
}

// EvidenceCorrection records only lineage and scope. It never mutates or
// removes the predecessor.
type EvidenceCorrection struct {
	OriginalID      values.EntityRef
	SuccessorID     values.EntityRef
	OriginalDigest  string
	SuccessorDigest string
	Reason          string
	Consumers       []Consumer
}

func (c EvidenceCorrection) Validate() error {
	if err := c.OriginalID.Validate(); err != nil || c.OriginalID.Kind != values.Kind("skill_evidence") {
		return fmt.Errorf("%w: original evidence id", ErrCorrection)
	}
	if err := c.SuccessorID.Validate(); err != nil || c.SuccessorID.Kind != values.Kind("skill_evidence") || c.SuccessorID.Tenant != c.OriginalID.Tenant || c.SuccessorID == c.OriginalID {
		return fmt.Errorf("%w: successor evidence id", ErrCorrection)
	}
	if c.OriginalDigest == "" || c.SuccessorDigest == "" || strings.TrimSpace(c.Reason) == "" || len(c.Consumers) == 0 {
		return fmt.Errorf("%w: lineage, reason and scope are required", ErrCorrection)
	}
	seen := map[Consumer]struct{}{}
	for _, consumer := range c.Consumers {
		if !consumer.valid() {
			return fmt.Errorf("%w: unknown consumer %q", ErrCorrection, consumer)
		}
		if _, ok := seen[consumer]; ok {
			return fmt.Errorf("%w: duplicate consumer", ErrCorrection)
		}
		seen[consumer] = struct{}{}
	}
	return nil
}

// ConsumerResolution is the exact read result and pinned inputs used by one
// consumer. Explanation is intentionally purpose-safe and contains no raw
// evidence references.
type ConsumerResolution struct {
	Consumer       Consumer
	OntologyDigest string
	EvidenceDigest string
	Resolution     Resolution
	Explanation    string
}

type ConformanceReport struct {
	Results          []ConsumerResolution
	EvidenceRevision EvidenceRevision
}

// ConsumerResolver is a consumer-owned adapter. Each consumer independently
// evaluates the pinned request; conformance compares their resulting digests.
type ConsumerResolver interface {
	Consumer() Consumer
	Resolve(context.Context, Resolver, ResolveRequest) (Resolution, string, error)
}

// ConsumerResolverFunc is the lightweight adapter used by composition roots.
type ConsumerResolverFunc struct {
	Kind Consumer
	Fn   func(context.Context, Resolver, ResolveRequest) (Resolution, string, error)
}

func (a ConsumerResolverFunc) Consumer() Consumer { return a.Kind }
func (a ConsumerResolverFunc) Resolve(ctx context.Context, resolver Resolver, req ResolveRequest) (Resolution, string, error) {
	if !a.Kind.valid() || a.Fn == nil {
		return Resolution{}, "", ErrConformance
	}
	return a.Fn(ctx, resolver, req)
}

// ResolveConsumers evaluates all consumers against one pinned snapshot and
// rejects any divergence in ontology, evidence revision or resolution.
func ResolveConsumers(ctx context.Context, ontology SkillOntologyRevision, equivalences []EquivalenceRule, snapshot EvidenceRevision, req ResolveRequest, adapters ...ConsumerResolver) (ConformanceReport, error) {
	if err := ontology.Validate(); err != nil {
		return ConformanceReport{}, fmt.Errorf("%w: ontology: %v", ErrConformance, err)
	}
	if snapshot.Digest == "" || snapshot.Digest != evidenceRevisionDigest(snapshot.Sequence, snapshot.Evidence) {
		return ConformanceReport{}, ErrPinnedEvidence
	}
	if len(adapters) != 4 {
		return ConformanceReport{}, fmt.Errorf("%w: four consumer adapters are required", ErrConformance)
	}
	// Detach every aggregate with slice fields before it crosses a consumer
	// boundary. An adapter must not be able to change a later adapter's pinned
	// request, ontology, or evidence by retaining and mutating a shared slice.
	ontology = cloneOntology(ontology)
	equivalences = append([]EquivalenceRule(nil), equivalences...)
	snapshot = cloneEvidenceRevision(snapshot)
	req.SkillRefs = append([]values.EntityRef(nil), req.SkillRefs...)
	req.Ontology, req.Equivalences = ontology, append([]EquivalenceRule(nil), equivalences...)
	if err := req.Validate(); err != nil {
		return ConformanceReport{}, fmt.Errorf("%w: request: %v", ErrConformance, err)
	}
	baselineResolver := NewPinnedResolver(cloneOntology(ontology), append([]EquivalenceRule(nil), equivalences...), FakeSkillEvidenceReader{Evidence: append([]WorkerSkillEvidence(nil), snapshot.Evidence...)})
	baseline, err := baselineResolver.Resolve(ctx, cloneResolveRequest(req))
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("%w: pinned baseline: %v", ErrConformance, err)
	}
	results := make([]ConsumerResolution, 0, 4)
	seen := map[Consumer]struct{}{}
	for _, adapter := range adapters {
		consumer := adapter.Consumer()
		if !consumer.valid() {
			return ConformanceReport{}, fmt.Errorf("%w: invalid consumer", ErrConformance)
		}
		if _, ok := seen[consumer]; ok {
			return ConformanceReport{}, fmt.Errorf("%w: duplicate consumer %s", ErrConformance, consumer)
		}
		seen[consumer] = struct{}{}
		consumerOntology := cloneOntology(ontology)
		consumerEquivalences := append([]EquivalenceRule(nil), equivalences...)
		resolver := NewPinnedResolver(consumerOntology, consumerEquivalences, FakeSkillEvidenceReader{Evidence: append([]WorkerSkillEvidence(nil), snapshot.Evidence...)})
		resolution, explanation, err := adapter.Resolve(ctx, resolver, cloneResolveRequest(req))
		if err != nil {
			return ConformanceReport{}, fmt.Errorf("%w: %s: %v", ErrConformance, consumer, err)
		}
		if err := resolution.Validate(); err != nil || resolution.OntologyDigest != ontology.CanonicalDigest || resolution.CanonicalDigest == "" || resolution.CanonicalDigest != resolution.computedDigest() {
			return ConformanceReport{}, fmt.Errorf("%w: %s returned an unpinned resolution", ErrConsumerParity, consumer)
		}
		if strings.TrimSpace(explanation) == "" {
			return ConformanceReport{}, fmt.Errorf("%w: %s explanation is required", ErrConformance, consumer)
		}
		if resolution.CanonicalDigest != baseline.CanonicalDigest {
			return ConformanceReport{}, fmt.Errorf("%w: %s resolution differs from pinned baseline", ErrConsumerParity, consumer)
		}
		results = append(results, ConsumerResolution{Consumer: consumer, OntologyDigest: ontology.CanonicalDigest, EvidenceDigest: snapshot.Digest, Resolution: resolution, Explanation: explanation})
	}
	return ConformanceReport{Results: results, EvidenceRevision: snapshot}, nil
}

func cloneResolveRequest(req ResolveRequest) ResolveRequest {
	req.SkillRefs = append([]values.EntityRef(nil), req.SkillRefs...)
	req.Ontology = cloneOntology(req.Ontology)
	req.Equivalences = append([]EquivalenceRule(nil), req.Equivalences...)
	return req
}

func cloneEvidenceRevision(snapshot EvidenceRevision) EvidenceRevision {
	snapshot.Evidence = append([]WorkerSkillEvidence(nil), snapshot.Evidence...)
	return snapshot
}

// CorrectEvidence validates an append-only successor and returns its lineage.
func CorrectEvidence(original, successor WorkerSkillEvidence, reason string, consumers []Consumer) (EvidenceCorrection, error) {
	if err := original.Validate(); err != nil {
		return EvidenceCorrection{}, fmt.Errorf("%w: original: %v", ErrCorrection, err)
	}
	if err := successor.Validate(); err != nil {
		return EvidenceCorrection{}, fmt.Errorf("%w: successor: %v", ErrCorrection, err)
	}
	if successor.Supersedes != original.EvidenceID {
		return EvidenceCorrection{}, fmt.Errorf("%w: successor must supersede original", ErrCorrection)
	}
	if successor.Worker != original.Worker || successor.SkillRef != original.SkillRef {
		return EvidenceCorrection{}, fmt.Errorf("%w: successor must retain worker and skill", ErrCorrection)
	}
	originalStart, _ := original.Effective.StartDate()
	successorStart, _ := successor.Effective.StartDate()
	if successorStart.Compare(originalStart) < 0 {
		return EvidenceCorrection{}, fmt.Errorf("%w: successor cannot predate original", ErrCorrection)
	}
	correction := EvidenceCorrection{OriginalID: original.EvidenceID, SuccessorID: successor.EvidenceID, OriginalDigest: original.CanonicalDigest, SuccessorDigest: successor.CanonicalDigest, Reason: reason, Consumers: append([]Consumer(nil), consumers...)}
	if err := correction.Validate(); err != nil {
		return EvidenceCorrection{}, err
	}
	return correction, nil
}
