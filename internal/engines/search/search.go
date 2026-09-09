// Package search owns the pure authorization-before-ranking envelope. It
// accepts already materialized candidates, binds tenant/record/field/purpose
// and bitemporal policy evidence, prefilters them, and only then calls a
// ranking port. No ranking implementation can receive a denied candidate.
package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

const contractVersion = 1

// Version reports the search engine contract version (ARCH-GO-009).
func Version() int { return contractVersion }

// Explain describes the authorization and ranking boundary.
func Explain() string {
	return "search v1: tenant, record, relationship, field, purpose, classification, time, and policy prefilter precedes exact, fuzzy, or vector ranking"
}

var (
	ErrInvalidEnvelope  = errors.New("search: invalid authorized query envelope")
	ErrInvalidCandidate = errors.New("search: invalid candidate")
	ErrNoAuthorizer     = errors.New("search: authorization prefilter is required")
	ErrNoRanker         = errors.New("search: ranker is required")
	ErrRankerViolation  = errors.New("search: ranker returned an unauthorized or duplicate candidate")
)

// Mode is the ranking family. The family is metadata only; the injected
// ranker owns its algorithm, and receives authorized candidates only.
type Mode string

const (
	ModeExact  Mode = "EXACT"
	ModeFuzzy  Mode = "FUZZY"
	ModeVector Mode = "VECTOR"
)

// QueryEnvelope is the immutable authorization envelope for one search plan.
// Query text is represented by QueryDigest so the envelope can be retained as
// evidence without making the raw query part of the public result.
type QueryEnvelope struct {
	Tenant             values.TenantId
	Purpose            string
	Classification     dlp.DataClass
	Fields             []authz.FieldID
	EffectiveAt        values.Instant
	KnownAt            values.KnownAt
	PolicyDigest       string
	QueryDigest        string
	SemanticPlanDigest string
	IndexDigest        string
	MinimumWatermark   values.Instant
	Mode               Mode
	MaxResults         int
}

func (e QueryEnvelope) Validate() error {
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidEnvelope, err)
	}
	for name, value := range map[string]string{
		"purpose": e.Purpose, "policy digest": e.PolicyDigest,
		"query digest": e.QueryDigest, "semantic plan digest": e.SemanticPlanDigest,
		"index digest": e.IndexDigest,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidEnvelope, name)
		}
	}
	if !e.Classification.Valid() {
		return fmt.Errorf("%w: classification is invalid", ErrInvalidEnvelope)
	}
	if err := e.EffectiveAt.Validate(); err != nil {
		return fmt.Errorf("%w: effective time: %v", ErrInvalidEnvelope, err)
	}
	if err := e.KnownAt.Instant().Validate(); err != nil {
		return fmt.Errorf("%w: known time: %v", ErrInvalidEnvelope, err)
	}
	if err := e.MinimumWatermark.Validate(); err != nil {
		return fmt.Errorf("%w: watermark: %v", ErrInvalidEnvelope, err)
	}
	switch e.Mode {
	case ModeExact, ModeFuzzy, ModeVector:
	default:
		return fmt.Errorf("%w: unknown ranking mode %q", ErrInvalidEnvelope, e.Mode)
	}
	if e.MaxResults <= 0 || e.MaxResults > 1000 {
		return fmt.Errorf("%w: max results must be 1..1000", ErrInvalidEnvelope)
	}
	if len(e.Fields) == 0 {
		return fmt.Errorf("%w: no authorized fields are bound", ErrInvalidEnvelope)
	}
	return nil
}

// Digest returns a deterministic envelope identity. It includes all
// authorization and freshness inputs, not the raw query text.
func (e QueryEnvelope) Digest() string {
	fields := append([]authz.FieldID(nil), e.Fields...)
	sort.Slice(fields, func(i, j int) bool { return fields[i] < fields[j] })
	fieldNames := make([]string, len(fields))
	for i, field := range fields {
		fieldNames[i] = string(field)
	}
	stream := canonicalbytes.New("search.query-envelope", 1).
		String("tenant", string(e.Tenant)).
		String("purpose", e.Purpose).
		String("classification", string(e.Classification)).
		String("effective_at", e.EffectiveAt.String()).
		String("known_at", e.KnownAt.Instant().String()).
		String("policy_digest", e.PolicyDigest).
		String("query_digest", e.QueryDigest).
		String("semantic_plan_digest", e.SemanticPlanDigest).
		String("index_digest", e.IndexDigest).
		String("minimum_watermark", e.MinimumWatermark.String()).
		String("mode", string(e.Mode)).
		Int("max_results", int64(e.MaxResults)).
		SortedStrings("fields", fieldNames)
	digest, err := stream.Digest()
	if err != nil {
		return canonicalbytes.Digest([]byte("search.query-envelope.invalid:" + err.Error()))
	}
	return digest
}

// Candidate is a source record offered to the prefilter. SearchText is only
// handed to a ranker after authorization; it never appears in a Result.
type Candidate struct {
	Subject        values.EntityRef
	SearchText     string
	SourceDigest   string
	PolicyDigest   string
	Purpose        string
	Classification dlp.DataClass
	Fields         []authz.FieldID
	EffectiveAt    values.Instant
	KnownAt        values.KnownAt
	Watermark      values.Instant
}

func (c Candidate) Validate(envelope QueryEnvelope) error {
	if err := c.Subject.Validate(); err != nil {
		return fmt.Errorf("%w: subject: %v", ErrInvalidCandidate, err)
	}
	if c.Subject.Tenant != envelope.Tenant {
		return fmt.Errorf("%w: subject crosses tenant boundary", ErrInvalidCandidate)
	}
	for name, value := range map[string]string{"source digest": c.SourceDigest, "policy digest": c.PolicyDigest, "purpose": c.Purpose} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required", ErrInvalidCandidate, name)
		}
	}
	if c.Purpose != envelope.Purpose || c.PolicyDigest != envelope.PolicyDigest {
		return fmt.Errorf("%w: purpose or policy is not bound to the envelope", ErrInvalidCandidate)
	}
	if !c.Classification.Valid() || c.Classification != envelope.Classification {
		return fmt.Errorf("%w: classification is not bound to the envelope", ErrInvalidCandidate)
	}
	if c.EffectiveAt.Compare(envelope.EffectiveAt) != 0 || c.KnownAt.Instant().Compare(envelope.KnownAt.Instant()) != 0 {
		return fmt.Errorf("%w: candidate time is not the planned time", ErrInvalidCandidate)
	}
	if c.Watermark.Compare(envelope.MinimumWatermark) < 0 {
		return fmt.Errorf("%w: candidate watermark is stale", ErrInvalidCandidate)
	}
	fields := make(map[authz.FieldID]bool, len(c.Fields))
	for _, field := range c.Fields {
		fields[field] = true
	}
	for _, field := range envelope.Fields {
		if !fields[field] {
			return fmt.Errorf("%w: candidate is missing authorized field %q", ErrInvalidCandidate, field)
		}
	}
	return nil
}

// AuthorizationDecision is the redaction-safe prefilter answer. Evidence is
// required for both allow and deny so a later reviewer can distinguish a
// suppressed candidate from an unplanned ranking omission without seeing its
// content.
type AuthorizationDecision struct {
	Allowed      bool
	PolicyDigest string
	Evidence     string
}

func (d AuthorizationDecision) Validate(envelope QueryEnvelope) error {
	if d.PolicyDigest != envelope.PolicyDigest || strings.TrimSpace(d.Evidence) == "" {
		return ErrInvalidEnvelope
	}
	return nil
}

// Authorizer is the only port allowed to decide whether a candidate enters
// ranking. It may be backed by internal/trust/authz and a repository/RLS
// scope, but this engine never computes grants itself.
type Authorizer interface {
	Authorize(context.Context, QueryEnvelope, Candidate) (AuthorizationDecision, error)
}

// AuthorizerFunc adapts a function to Authorizer.
type AuthorizerFunc func(context.Context, QueryEnvelope, Candidate) (AuthorizationDecision, error)

func (f AuthorizerFunc) Authorize(ctx context.Context, e QueryEnvelope, c Candidate) (AuthorizationDecision, error) {
	return f(ctx, e, c)
}

// RankedCandidate is the ranker's public, bounded answer. Scores are not
// exposed for suppressed candidates because suppressed candidates never reach
// the ranker.
type RankedCandidate struct {
	Subject values.EntityRef
	Score   int64
}

// Ranker receives only the authorized candidate slice. A ranker may implement
// exact, fuzzy, or vector semantics without gaining an authorization bypass.
type Ranker interface {
	Rank(context.Context, QueryEnvelope, []Candidate) ([]RankedCandidate, error)
}

// RankerFunc adapts a function to Ranker.
type RankerFunc func(context.Context, QueryEnvelope, []Candidate) ([]RankedCandidate, error)

func (f RankerFunc) Rank(ctx context.Context, e QueryEnvelope, c []Candidate) ([]RankedCandidate, error) {
	return f(ctx, e, c)
}

// Result is one authorized hit. It carries source and authorization evidence,
// never snippets, field values, hidden counts, facets, embeddings, or denied
// similarity scores.
type Result struct {
	Subject               values.EntityRef
	SourceDigest          string
	PolicyDigest          string
	AuthorizationEvidence string
	Watermark             values.Instant
	Score                 int64
}

// Proof binds the visible results to the authorized query plan. SuppressedDigest
// is a one-way evidence identity, not a count or a list of suppressed subjects.
type Proof struct {
	EnvelopeDigest   string
	SuppressedDigest string
}

// Execute prefilters every candidate before invoking the ranker. A candidate
// with a tenant, time, classification, source-policy, or authorization
// mismatch is suppressed and can never influence rank ordering or any ranking
// side channel.
func Execute(ctx context.Context, envelope QueryEnvelope, candidates []Candidate, authorizer Authorizer, ranker Ranker) ([]Result, Proof, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, Proof{}, ErrInvalidEnvelope
	}
	if err := envelope.Validate(); err != nil {
		return nil, Proof{}, err
	}
	if authorizer == nil {
		return nil, Proof{}, ErrNoAuthorizer
	}
	if ranker == nil {
		return nil, Proof{}, ErrNoRanker
	}
	authorized := make([]Candidate, 0, len(candidates))
	decisions := make(map[string]AuthorizationDecision, len(candidates))
	suppressed := make([]string, 0)
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if err := candidate.Validate(envelope); err != nil {
			return nil, Proof{}, err
		}
		key := candidate.Subject.String()
		if seen[key] {
			return nil, Proof{}, fmt.Errorf("%w: duplicate source candidate", ErrInvalidCandidate)
		}
		seen[key] = true
		decision, err := authorizer.Authorize(ctx, envelope, candidate)
		if err != nil {
			suppressed = append(suppressed, key+"|AUTHZ_UNAVAILABLE")
			continue
		}
		if err := decision.Validate(envelope); err != nil {
			return nil, Proof{}, err
		}
		if !decision.Allowed {
			suppressed = append(suppressed, key+"|DENIED|"+decision.Evidence)
			continue
		}
		authorized = append(authorized, candidate)
		decisions[key] = decision
	}
	ranked, err := ranker.Rank(ctx, envelope, authorized)
	if err != nil {
		return nil, Proof{}, err
	}
	if len(ranked) > envelope.MaxResults {
		ranked = ranked[:envelope.MaxResults]
	}
	bySubject := make(map[string]Candidate, len(authorized))
	for _, candidate := range authorized {
		bySubject[candidate.Subject.String()] = candidate
	}
	results := make([]Result, 0, len(ranked))
	returned := make(map[string]bool, len(ranked))
	for _, rankedCandidate := range ranked {
		key := rankedCandidate.Subject.String()
		candidate, ok := bySubject[key]
		if !ok || returned[key] {
			return nil, Proof{}, ErrRankerViolation
		}
		returned[key] = true
		results = append(results, Result{Subject: candidate.Subject, SourceDigest: candidate.SourceDigest, PolicyDigest: candidate.PolicyDigest, AuthorizationEvidence: decisions[key].Evidence, Watermark: candidate.Watermark, Score: rankedCandidate.Score})
	}
	sort.Slice(suppressed, func(i, j int) bool { return suppressed[i] < suppressed[j] })
	sum := sha256.Sum256([]byte(strings.Join(suppressed, "\n")))
	return results, Proof{EnvelopeDigest: envelope.Digest(), SuppressedDigest: hex.EncodeToString(sum[:])}, nil
}
