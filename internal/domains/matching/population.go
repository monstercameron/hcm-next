package matching

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Candidate population resolution is deliberately separate from Match. A
// population is a frozen input boundary: ranking may consume it, but it may
// not re-read the facts port or silently widen the authorized set.
const populationSchemaVersion = 1

var (
	ErrInvalidCandidatePopulation = errors.New("matching: invalid candidate population")
	ErrPopulationUnfrozen         = errors.New("matching: candidate population is not frozen")
	ErrPopulationSuperseded       = errors.New("matching: candidate population is superseded")
	ErrDisclosureDecision         = errors.New("matching: invalid candidate disclosure decision")
	ErrPopulationBinding          = errors.New("matching: candidate population binding mismatch")
)

// CandidateCompleteness records whether the facts boundary gave the resolver
// a complete candidate view. It is retained in the frozen digest rather than
// being inferred later by a matcher.
type CandidateCompleteness string

const (
	CandidateCompletenessComplete CandidateCompleteness = "COMPLETE"
	CandidateCompletenessPartial  CandidateCompleteness = "PARTIAL"
)

func (c CandidateCompleteness) Valid() bool {
	return c == CandidateCompletenessComplete || c == CandidateCompletenessPartial
}

// CandidateExclusionReason is a stable reason token for an omitted candidate.
// The disclosure policy may provide a more specific policy reason; that
// reason is preserved as a separate map key and never accompanied by a ref.
type CandidateExclusionReason string

const (
	ExclusionSourceMismatch   CandidateExclusionReason = "SOURCE_MISMATCH"
	ExclusionDisclosureDenied CandidateExclusionReason = "DISCLOSURE_DENIED"
	ExclusionIncompleteFacts  CandidateExclusionReason = "INCOMPLETE_FACTS"
)

// DisclosureDecision is the authorization result for one candidate. A denied
// decision must name a reason, because counts are exposed without identities.
type DisclosureDecision struct {
	Allowed       bool
	Reason        string
	PolicyVersion string
	Completeness  CandidateCompleteness
}

// DisclosureScope is supplied by the authorization owner. The matcher does
// not construct a broader policy from the requester's entity reference.
type DisclosureScope func(values.EntityRef, CandidateFacts) DisclosureDecision

// CandidateDisclosureScope is a descriptive alias for callers that want the
// authorization role explicit in their declarations.
type CandidateDisclosureScope = DisclosureScope

// AllowAllDisclosureScope is useful only for a caller that has already
// established that the requester may see every candidate in this source.
func AllowAllDisclosureScope(values.EntityRef, CandidateFacts) DisclosureDecision {
	return DisclosureDecision{Allowed: true, PolicyVersion: "matching.scope/allow-all", Completeness: CandidateCompletenessComplete}
}

// CandidatePopulation is the immutable, digested candidate set authorized for
// one MatchRequest at that request's as-of instant. Candidates that were
// excluded are represented only by reason counts; their references and facts
// are intentionally absent.
type CandidatePopulation struct {
	RequestID          string
	RequestDigest      string
	RequesterScope     values.EntityRef
	CandidateSourceRef values.EntityRef
	AsOf               values.Instant
	Candidates         []CandidateFacts
	ExclusionCounts    map[string]int
	Completeness       CandidateCompleteness
	Superseded         bool
	CanonicalDigest    string

	frozen bool
}

// CandidateFactsList returns a deep defensive copy of the authorized facts.
func (p CandidatePopulation) CandidateFactsList() []CandidateFacts {
	return cloneCandidateFacts(p.Candidates)
}

// Exclusions returns a defensive copy of the omission counts.
func (p CandidatePopulation) Exclusions() map[string]int {
	out := make(map[string]int, len(p.ExclusionCounts))
	for reason, count := range p.ExclusionCounts {
		out[reason] = count
	}
	return out
}

func cloneCandidateFacts(in []CandidateFacts) []CandidateFacts {
	out := make([]CandidateFacts, len(in))
	for i, fact := range in {
		out[i] = fact
		out[i].Availability = append([]values.EffectiveInterval(nil), fact.Availability...)
		out[i].QualificationRefs = append([]values.EntityRef(nil), fact.QualificationRefs...)
	}
	return out
}

// Supersede returns a copy marked unusable for future matching. It is the
// only package operation that marks a frozen value superseded; the original
// value remains unchanged and can still be retained as historical evidence.
func (p CandidatePopulation) Supersede() CandidatePopulation {
	p.Superseded = true
	return p
}

// Validate verifies the frozen binding and digest. A zero value or a value
// whose public fields were changed after freezing cannot pass this boundary.
func (p CandidatePopulation) Validate() error {
	if !p.frozen || p.CanonicalDigest == "" {
		return ErrPopulationUnfrozen
	}
	if p.Superseded {
		return ErrPopulationSuperseded
	}
	if strings.TrimSpace(p.RequestID) == "" || strings.TrimSpace(p.RequestDigest) == "" {
		return fmt.Errorf("%w: request binding is required", ErrInvalidCandidatePopulation)
	}
	if err := p.RequesterScope.Validate(); err != nil {
		return fmt.Errorf("%w: requester scope: %v", ErrInvalidCandidatePopulation, err)
	}
	if err := p.CandidateSourceRef.Validate(); err != nil {
		return fmt.Errorf("%w: candidate source: %v", ErrInvalidCandidatePopulation, err)
	}
	if err := p.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %v", ErrInvalidCandidatePopulation, err)
	}
	if !p.Completeness.Valid() {
		return fmt.Errorf("%w: completeness is required", ErrInvalidCandidatePopulation)
	}
	if err := validatePopulationFacts(p.Candidates, p.RequesterScope.Tenant); err != nil {
		return err
	}
	if err := validateExclusionCounts(p.ExclusionCounts); err != nil {
		return err
	}
	if want := p.computedDigest(); want != p.CanonicalDigest {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidCandidatePopulation)
	}
	return nil
}

func validatePopulationFacts(facts []CandidateFacts, tenant values.TenantId) error {
	seen := make(map[string]struct{}, len(facts))
	for i, fact := range facts {
		if err := fact.Validate(); err != nil {
			return fmt.Errorf("%w: candidate %d: %v", ErrInvalidCandidatePopulation, i, err)
		}
		if fact.CandidateRef.Tenant != tenant {
			return fmt.Errorf("%w: candidate %d crosses tenant", ErrInvalidCandidatePopulation, i)
		}
		key := fact.CandidateRef.String()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate candidate %s", ErrInvalidCandidatePopulation, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateExclusionCounts(counts map[string]int) error {
	for reason, count := range counts {
		if strings.TrimSpace(reason) == "" || count < 0 {
			return fmt.Errorf("%w: exclusion counts require non-empty reasons and non-negative counts", ErrInvalidCandidatePopulation)
		}
	}
	return nil
}

func freezeCandidatePopulation(request MatchRequest, facts []CandidateFacts, counts map[string]int, completeness CandidateCompleteness) (CandidatePopulation, error) {
	if err := request.Validate(); err != nil {
		return CandidatePopulation{}, err
	}
	if !completeness.Valid() {
		return CandidatePopulation{}, fmt.Errorf("%w: completeness is required", ErrInvalidCandidatePopulation)
	}
	ordered := cloneCandidateFacts(facts)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CandidateRef.String() < ordered[j].CandidateRef.String() })
	if err := validatePopulationFacts(ordered, request.RequesterScope.Tenant); err != nil {
		return CandidatePopulation{}, err
	}
	for i, fact := range ordered {
		if fact.SourceRef != (values.EntityRef{}) && fact.SourceRef != request.CandidateSourceRef {
			return CandidatePopulation{}, fmt.Errorf("%w: candidate %d is outside the requested source", ErrInvalidCandidatePopulation, i)
		}
	}
	copiedCounts := make(map[string]int, len(counts))
	for reason, count := range counts {
		copiedCounts[reason] = count
	}
	if err := validateExclusionCounts(copiedCounts); err != nil {
		return CandidatePopulation{}, err
	}
	p := CandidatePopulation{
		RequestID:          request.RequestID,
		RequestDigest:      request.computedDigest(),
		RequesterScope:     request.RequesterScope,
		CandidateSourceRef: request.CandidateSourceRef,
		AsOf:               request.AsOf,
		Candidates:         ordered,
		ExclusionCounts:    copiedCounts,
		Completeness:       completeness,
		frozen:             true,
	}
	p.CanonicalDigest = p.computedDigest()
	return p, nil
}

// FreezeCandidatePopulation freezes facts that have already been resolved by
// a caller. ResolveCandidatePopulation is preferred when authorization must
// be applied in this package.
func FreezeCandidatePopulation(request MatchRequest, facts []CandidateFacts, exclusionCounts map[string]int, completeness CandidateCompleteness) (CandidatePopulation, error) {
	return freezeCandidatePopulation(request, facts, exclusionCounts, completeness)
}

// CandidatePopulationResolver resolves and freezes one authorized population
// from a read-only candidate facts port.
type CandidatePopulationResolver struct {
	Facts CandidateFactsPort
	Scope DisclosureScope
}

// Resolve obtains facts at request.AsOf, filters by the requested source,
// applies the requester's disclosure scope, and freezes only allowed facts.
func (r CandidatePopulationResolver) Resolve(ctx context.Context, request MatchRequest) (CandidatePopulation, error) {
	if r.Facts == nil {
		return CandidatePopulation{}, fmt.Errorf("%w: nil facts port", ErrFactsReader)
	}
	if r.Scope == nil {
		return CandidatePopulation{}, fmt.Errorf("%w: disclosure scope is required", ErrDisclosureDecision)
	}
	if err := request.Validate(); err != nil {
		return CandidatePopulation{}, err
	}
	facts, err := r.Facts.CandidateFacts(ctx, request)
	if err != nil {
		return CandidatePopulation{}, fmt.Errorf("%w: %v", ErrFactsReader, err)
	}
	counts := make(map[string]int)
	allowed := make([]CandidateFacts, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	completeness := CandidateCompletenessComplete
	for i, fact := range facts {
		if err := fact.Validate(); err != nil {
			return CandidatePopulation{}, fmt.Errorf("%w: candidate %d: %v", ErrInvalidCandidatePopulation, i, err)
		}
		key := fact.CandidateRef.String()
		if _, ok := seen[key]; ok {
			return CandidatePopulation{}, fmt.Errorf("%w: %s", ErrDuplicateCandidate, key)
		}
		seen[key] = struct{}{}
		if fact.SourceRef != (values.EntityRef{}) && fact.SourceRef != request.CandidateSourceRef {
			counts[string(ExclusionSourceMismatch)]++
			continue
		}
		decision := r.Scope(request.RequesterScope, fact)
		if decision.Completeness != "" && !decision.Completeness.Valid() {
			return CandidatePopulation{}, fmt.Errorf("%w: completeness %q", ErrDisclosureDecision, decision.Completeness)
		}
		if decision.Completeness == CandidateCompletenessPartial {
			completeness = CandidateCompletenessPartial
		}
		if !decision.Allowed {
			if strings.TrimSpace(decision.Reason) == "" {
				return CandidatePopulation{}, fmt.Errorf("%w: denied candidate %s has no reason", ErrDisclosureDecision, key)
			}
			counts[decision.Reason]++
			continue
		}
		allowed = append(allowed, fact)
	}
	return freezeCandidatePopulation(request, allowed, counts, completeness)
}

// ResolveCandidatePopulation is the direct function form of the resolver.
func ResolveCandidatePopulation(ctx context.Context, facts CandidateFactsPort, request MatchRequest, scope DisclosureScope) (CandidatePopulation, error) {
	return (CandidatePopulationResolver{Facts: facts, Scope: scope}).Resolve(ctx, request)
}

func candidatePopulationBody(p CandidatePopulation) []byte {
	ordered := cloneCandidateFacts(p.Candidates)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CandidateRef.String() < ordered[j].CandidateRef.String() })
	reasons := make([]string, 0, len(p.ExclusionCounts))
	for reason := range p.ExclusionCounts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	w := canonicalbytes.New("hcmnext.domains.matching.CandidatePopulation", populationSchemaVersion).
		String("request_id", p.RequestID).String("request_digest", p.RequestDigest).
		Value("requester_scope", p.RequesterScope).Value("candidate_source_ref", p.CandidateSourceRef).
		Value("as_of", p.AsOf).String("completeness", string(p.Completeness)).Count("candidates", len(ordered))
	for _, fact := range ordered {
		quals := make([]string, 0, len(fact.QualificationRefs))
		for _, ref := range fact.QualificationRefs {
			quals = append(quals, ref.String())
		}
		sort.Strings(quals)
		windows := append([]values.EffectiveInterval(nil), fact.Availability...)
		sort.Slice(windows, func(i, j int) bool { return windows[i].String() < windows[j].String() })
		w.Value("candidate_ref", fact.CandidateRef)
		if fact.SourceRef == (values.EntityRef{}) {
			w.String("source_ref", "")
		} else {
			w.Value("source_ref", fact.SourceRef)
		}
		w.String("location", fact.Location).Value("cost", fact.Cost).Count("availability", len(windows))
		for _, window := range windows {
			w.Value("availability_window", window)
		}
		w.SortedStrings("qualification_ref", quals)
	}
	w.Count("exclusion_reasons", len(reasons))
	for _, reason := range reasons {
		w.String("exclusion_reason", reason).Int("exclusion_count", int64(p.ExclusionCounts[reason]))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (p CandidatePopulation) computedDigest() string {
	raw := candidatePopulationBody(p)
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the frozen population's canonical bytes.
func (p CandidatePopulation) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return candidatePopulationBody(p)
}

// Digest returns the frozen population digest.
func (p CandidatePopulation) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.CanonicalDigest, nil
}

// MatchAgainstPopulation ranks exactly the facts in a frozen population.
// It never calls a facts port, and refuses a population bound to another
// request or one that was superseded after its freeze.
func MatchAgainstPopulation(ctx context.Context, request MatchRequest, population CandidatePopulation) (MatchResult, error) {
	if err := population.Validate(); err != nil {
		return MatchResult{}, err
	}
	if err := request.Validate(); err != nil {
		return MatchResult{}, err
	}
	if population.RequestID != request.RequestID || population.RequestDigest != request.computedDigest() || population.AsOf != request.AsOf || population.CandidateSourceRef != request.CandidateSourceRef {
		return MatchResult{}, fmt.Errorf("%w: request, as-of, or source differs", ErrPopulationBinding)
	}
	reader, err := NewInMemoryCandidateFacts(population.CandidateFactsList())
	if err != nil {
		return MatchResult{}, err
	}
	return Match(ctx, reader, request)
}

// MatchFrozen is a concise alias for MatchAgainstPopulation.
func MatchFrozen(ctx context.Context, request MatchRequest, population CandidatePopulation) (MatchResult, error) {
	return MatchAgainstPopulation(ctx, request, population)
}

// CandidatePopulationExplanation is the bounded explanation of an
// authorization result. It contains exclusion counts but no excluded refs.
type CandidatePopulationExplanation struct {
	RequestID        string
	PopulationDigest string
	CandidateCount   int
	ExcludedCounts   map[string]int
	Completeness     CandidateCompleteness
	AsOf             values.Instant
	Authority        string
}

func (p CandidatePopulation) Explain() (CandidatePopulationExplanation, error) {
	if err := p.Validate(); err != nil {
		return CandidatePopulationExplanation{}, err
	}
	return CandidatePopulationExplanation{
		RequestID:        p.RequestID,
		PopulationDigest: p.CanonicalDigest,
		CandidateCount:   len(p.Candidates),
		ExcludedCounts:   p.Exclusions(),
		Completeness:     p.Completeness,
		AsOf:             p.AsOf,
		Authority:        "authorized frozen candidate population; no assignment authority",
	}, nil
}

// Explain returns the stable population explanation used by audit and UI
// layers.
func ExplainCandidatePopulation(p CandidatePopulation) (CandidatePopulationExplanation, error) {
	return p.Explain()
}
