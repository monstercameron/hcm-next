// Package fx owns the governed exchange-rate vocabulary used by payroll,
// rewards, and analytics. It is deliberately pure: sources and observations
// are supplied by callers, and no ambient clock or provider is consulted.
package fx

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidRateSource        = errors.New("fx: invalid rate source")
	ErrInvalidQuote             = errors.New("fx: invalid quote")
	ErrInvalidConversionProfile = errors.New("fx: invalid conversion profile")
	ErrInvalidQuoteResolution   = errors.New("fx: invalid quote resolution request")
	ErrQuoteUnknown             = errors.New("fx: quote pair is unknown")
	ErrQuoteStale               = errors.New("fx: quote is stale")
	ErrQuoteConflict            = errors.New("fx: quote candidates conflict")
	ErrInvalidConversion        = errors.New("fx: invalid conversion")
)

// QuoteCadence is a closed vocabulary describing how often a source observes
// rates. It is descriptive metadata, not permission to synthesize a rate.
type QuoteCadence string

const (
	CadenceRealTime QuoteCadence = "REAL_TIME"
	CadenceHourly   QuoteCadence = "HOURLY"
	CadenceDaily    QuoteCadence = "DAILY"
	CadenceMonthly  QuoteCadence = "MONTHLY"
	CadenceOnDemand QuoteCadence = "ON_DEMAND"
)

func (c QuoteCadence) Valid() bool {
	switch c {
	case CadenceRealTime, CadenceHourly, CadenceDaily, CadenceMonthly, CadenceOnDemand:
		return true
	default:
		return false
	}
}

// AuthorityClass records the authority level assigned to a source by policy.
type AuthorityClass string

const (
	AuthorityPrimary   AuthorityClass = "PRIMARY"
	AuthoritySecondary AuthorityClass = "SECONDARY"
	AuthorityAdvisory  AuthorityClass = "ADVISORY"
)

func (a AuthorityClass) Valid() bool {
	switch a {
	case AuthorityPrimary, AuthoritySecondary, AuthorityAdvisory:
		return true
	default:
		return false
	}
}

// MarketConvention is the convention under which an observation was made.
type MarketConvention string

const (
	ConventionSpot     MarketConvention = "SPOT"
	ConventionAverage  MarketConvention = "AVERAGE"
	ConventionClosing  MarketConvention = "CLOSING"
	ConventionOfficial MarketConvention = "OFFICIAL"
)

func (c MarketConvention) Valid() bool {
	switch c {
	case ConventionSpot, ConventionAverage, ConventionClosing, ConventionOfficial:
		return true
	default:
		return false
	}
}

// Confidence is the source's declared confidence in an observation.
type Confidence string

const (
	ConfidenceHigh   Confidence = "HIGH"
	ConfidenceMedium Confidence = "MEDIUM"
	ConfidenceLow    Confidence = "LOW"
)

func (c Confidence) Valid() bool {
	switch c {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return true
	default:
		return false
	}
}

// CurrencyPair is a canonical base/quote currency pair.
type CurrencyPair struct {
	Base  string
	Quote string
}

func NewCurrencyPair(base, quote string) (CurrencyPair, error) {
	p := CurrencyPair{Base: base, Quote: quote}
	if err := p.Validate(); err != nil {
		return CurrencyPair{}, err
	}
	return p, nil
}

func (p CurrencyPair) Validate() error {
	if !currencyCode(p.Base) || !currencyCode(p.Quote) {
		return fmt.Errorf("%w: base and quote currencies must be three uppercase letters", ErrInvalidQuote)
	}
	if p.Base == p.Quote {
		return fmt.Errorf("%w: base and quote currencies must differ", ErrInvalidQuote)
	}
	return nil
}

func (p CurrencyPair) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.fx.CurrencyPair", schemaVersion).
		String("base", p.Base).String("quote", p.Quote).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func currencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// RateSourceRevision is an immutable, versioned declaration of an exchange
// rate provider and the pairs it is permitted to publish.
type RateSourceRevision struct {
	SourceID        string
	Revision        uint64
	ParentRevision  uint64
	ParentDigest    string
	ProviderRef     string
	Pairs           []CurrencyPair
	QuoteCadence    QuoteCadence
	AuthorityClass  AuthorityClass
	Priority        int
	Effective       values.EffectiveInterval
	CanonicalDigest string
}

type FXRateSource = RateSourceRevision
type RateSource = RateSourceRevision
type FXRateSourceRevision = RateSourceRevision
type ExchangeRateSourceRevision = RateSourceRevision

func (s RateSourceRevision) Validate() error {
	if strings.TrimSpace(s.SourceID) == "" || s.Revision == 0 {
		return fmt.Errorf("%w: source_id and revision are required", ErrInvalidRateSource)
	}
	if s.Revision == 1 && (s.ParentRevision != 0 || s.ParentDigest != "") {
		return fmt.Errorf("%w: parent_revision: first revision cannot have a parent", ErrInvalidRateSource)
	}
	if s.Revision > 1 && (s.ParentRevision == 0 || s.ParentRevision >= s.Revision || strings.TrimSpace(s.ParentDigest) == "") {
		return fmt.Errorf("%w: parent_digest: successor requires an earlier parent digest", ErrInvalidRateSource)
	}
	if strings.TrimSpace(s.ProviderRef) == "" {
		return fmt.Errorf("%w: provider_ref is required", ErrInvalidRateSource)
	}
	if len(s.Pairs) == 0 {
		return fmt.Errorf("%w: pairs are required", ErrInvalidRateSource)
	}
	seen := make(map[string]struct{}, len(s.Pairs))
	for _, pair := range s.Pairs {
		if err := pair.Validate(); err != nil {
			return fmt.Errorf("%w: pair: %v", ErrInvalidRateSource, err)
		}
		key := pair.Base + "/" + pair.Quote
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: pairs: duplicate %s", ErrInvalidRateSource, key)
		}
		seen[key] = struct{}{}
	}
	if !s.QuoteCadence.Valid() {
		return fmt.Errorf("%w: quote_cadence %q is not declared", ErrInvalidRateSource, s.QuoteCadence)
	}
	if !s.AuthorityClass.Valid() {
		return fmt.Errorf("%w: authority_class %q is not declared", ErrInvalidRateSource, s.AuthorityClass)
	}
	if s.Priority < 0 {
		return fmt.Errorf("%w: priority cannot be negative", ErrInvalidRateSource)
	}
	if err := s.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidRateSource, err)
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRateSource)
	}
	return nil
}

func (s RateSourceRevision) body() []byte {
	pairs := append([]CurrencyPair(nil), s.Pairs...)
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Base+"/"+pairs[i].Quote < pairs[j].Base+"/"+pairs[j].Quote })
	w := canonicalbytes.New("hcmnext.domains.fx.RateSourceRevision", schemaVersion).
		String("source_id", s.SourceID).Int("revision", int64(s.Revision)).Int("parent_revision", int64(s.ParentRevision)).
		String("parent_digest", s.ParentDigest).String("provider_ref", s.ProviderRef).
		String("quote_cadence", string(s.QuoteCadence)).String("authority_class", string(s.AuthorityClass)).
		Int("priority", int64(s.Priority)).Value("effective", s.Effective).Count("pairs", len(pairs))
	for _, pair := range pairs {
		w.Value("pair", pair)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (s RateSourceRevision) computedDigest() string { return canonicalbytes.Digest(s.body()) }
func (s RateSourceRevision) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	return s.body()
}

// NewRateSourceRevision copies and digests a source revision.
func NewRateSourceRevision(s RateSourceRevision) (RateSourceRevision, error) {
	s.Pairs = append([]CurrencyPair(nil), s.Pairs...)
	s.CanonicalDigest = ""
	if err := s.Validate(); err != nil {
		return RateSourceRevision{}, err
	}
	s.CanonicalDigest = s.computedDigest()
	return s, nil
}

func NewFXRateSourceRevision(s RateSourceRevision) (RateSourceRevision, error) {
	return NewRateSourceRevision(s)
}

// FXQuoteRevision is one immutable, digested observation. AsOf is the market
// observation time; EffectiveAt and ObservedAt are accepted compatibility
// spellings and must agree when both are supplied.
type FXQuoteRevision struct {
	QuoteID          string
	SourceID         string
	SourceRevision   uint64
	BaseCurrency     string
	QuoteCurrency    string
	Rate             values.Decimal
	AsOf             values.Instant
	EffectiveAt      values.Instant
	ObservedAt       values.Instant
	KnownAt          values.Instant
	MarketConvention MarketConvention
	Confidence       Confidence
	CanonicalDigest  string
}

type QuoteRevision = FXQuoteRevision
type FXQuote = FXQuoteRevision

func (q FXQuoteRevision) observationTime() (values.Instant, error) {
	chosen := q.AsOf
	for _, candidate := range []values.Instant{q.EffectiveAt, q.ObservedAt} {
		if !candidate.IsSet() {
			continue
		}
		if !chosen.IsSet() {
			chosen = candidate
			continue
		}
		if chosen.Compare(candidate) != 0 {
			return values.Instant{}, fmt.Errorf("%w: as_of and effective_at/observed_at disagree", ErrInvalidQuote)
		}
	}
	return chosen, nil
}

func (q FXQuoteRevision) Validate() error {
	if strings.TrimSpace(q.QuoteID) == "" {
		return fmt.Errorf("%w: quote_id is required", ErrInvalidQuote)
	}
	if strings.TrimSpace(q.SourceID) == "" || q.SourceRevision == 0 {
		return fmt.Errorf("%w: source_id and source_revision are required", ErrInvalidQuote)
	}
	pair := CurrencyPair{Base: q.BaseCurrency, Quote: q.QuoteCurrency}
	if err := pair.Validate(); err != nil {
		return fmt.Errorf("%w: base_currency/quote_currency: %v", ErrInvalidQuote, err)
	}
	if err := q.Rate.Validate(); err != nil {
		return fmt.Errorf("%w: rate: %v", ErrInvalidQuote, err)
	}
	if q.Rate.Sign() <= 0 {
		return fmt.Errorf("%w: rate must be positive", ErrInvalidQuote)
	}
	when, err := q.observationTime()
	if err != nil {
		return err
	}
	if err := when.Validate(); err != nil {
		return fmt.Errorf("%w: as_of: %v", ErrInvalidQuote, err)
	}
	if err := q.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidQuote, err)
	}
	if q.KnownAt.Before(when) {
		return fmt.Errorf("%w: known_at must not precede as_of", ErrInvalidQuote)
	}
	if !q.MarketConvention.Valid() {
		return fmt.Errorf("%w: market_convention %q is not declared", ErrInvalidQuote, q.MarketConvention)
	}
	if !q.Confidence.Valid() {
		return fmt.Errorf("%w: confidence %q is not declared", ErrInvalidQuote, q.Confidence)
	}
	if q.CanonicalDigest != "" && q.CanonicalDigest != q.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidQuote)
	}
	return nil
}

func (q FXQuoteRevision) normalized() FXQuoteRevision {
	when, _ := q.observationTime()
	q.AsOf, q.EffectiveAt, q.ObservedAt = when, when, when
	return q
}

func (q FXQuoteRevision) body() []byte {
	q = q.normalized()
	w := canonicalbytes.New("hcmnext.domains.fx.FXQuoteRevision", schemaVersion).
		String("quote_id", q.QuoteID).String("source_id", q.SourceID).Int("source_revision", int64(q.SourceRevision)).
		String("base_currency", q.BaseCurrency).String("quote_currency", q.QuoteCurrency).Value("rate", q.Rate).
		Value("as_of", q.AsOf).Value("known_at", q.KnownAt).
		String("market_convention", string(q.MarketConvention)).String("confidence", string(q.Confidence))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (q FXQuoteRevision) computedDigest() string { return canonicalbytes.Digest(q.body()) }
func (q FXQuoteRevision) Canonical() []byte {
	if q.Validate() != nil {
		return nil
	}
	return q.body()
}

// NewFXQuoteRevision copies and digests an observation.
func NewFXQuoteRevision(q FXQuoteRevision) (FXQuoteRevision, error) {
	q = q.normalized()
	q.CanonicalDigest = ""
	if err := q.Validate(); err != nil {
		return FXQuoteRevision{}, err
	}
	q.CanonicalDigest = q.computedDigest()
	return q, nil
}

func NewQuoteRevision(q FXQuoteRevision) (FXQuoteRevision, error) {
	return NewFXQuoteRevision(q)
}

// RoundingRule makes the output scale and rounding behavior explicit.
type RoundingRule struct {
	Scale int32
	Mode  values.RoundingMode
}

func (r RoundingRule) Validate() error {
	if r.Scale < 0 || r.Scale > values.MaxScale {
		return fmt.Errorf("%w: rounding_rule.scale is out of range", ErrInvalidConversionProfile)
	}
	if !r.Mode.Valid() {
		return fmt.Errorf("%w: rounding_rule.mode is not declared", ErrInvalidConversionProfile)
	}
	return nil
}

func (r RoundingRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.fx.RoundingRule", schemaVersion).
		Int("scale", int64(r.Scale)).String("mode", r.Mode.String()).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ConversionProfileRevision is a versioned, effective-dated selection and
// rounding policy. Its source order is explicit; there is no current-rate
// fallback.
type ConversionProfileRevision struct {
	ProfileID           string
	Revision            uint64
	ParentRevision      uint64
	ParentDigest        string
	RoundingRule        RoundingRule
	Tolerance           time.Duration
	FallbackSourceOrder []string
	Effective           values.EffectiveInterval
	CanonicalDigest     string
}

type ConversionProfile = ConversionProfileRevision

func (p ConversionProfileRevision) Validate() error {
	if strings.TrimSpace(p.ProfileID) == "" || p.Revision == 0 {
		return fmt.Errorf("%w: profile_id and revision are required", ErrInvalidConversionProfile)
	}
	if p.Revision == 1 && (p.ParentRevision != 0 || p.ParentDigest != "") {
		return fmt.Errorf("%w: parent_revision: first revision cannot have a parent", ErrInvalidConversionProfile)
	}
	if p.Revision > 1 && (p.ParentRevision == 0 || p.ParentRevision >= p.Revision || strings.TrimSpace(p.ParentDigest) == "") {
		return fmt.Errorf("%w: parent_digest: successor requires an earlier parent digest", ErrInvalidConversionProfile)
	}
	if err := p.RoundingRule.Validate(); err != nil {
		return err
	}
	if p.Tolerance <= 0 {
		return fmt.Errorf("%w: tolerance must be positive", ErrInvalidConversionProfile)
	}
	if len(p.FallbackSourceOrder) == 0 {
		return fmt.Errorf("%w: fallback_source_order is required", ErrInvalidConversionProfile)
	}
	seen := make(map[string]struct{}, len(p.FallbackSourceOrder))
	for _, id := range p.FallbackSourceOrder {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: fallback_source_order contains an empty source", ErrInvalidConversionProfile)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%w: fallback_source_order contains duplicate %q", ErrInvalidConversionProfile, id)
		}
		seen[id] = struct{}{}
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidConversionProfile, err)
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidConversionProfile)
	}
	return nil
}

func (p ConversionProfileRevision) body() []byte {
	order := append([]string(nil), p.FallbackSourceOrder...)
	w := canonicalbytes.New("hcmnext.domains.fx.ConversionProfileRevision", schemaVersion).
		String("profile_id", p.ProfileID).Int("revision", int64(p.Revision)).Int("parent_revision", int64(p.ParentRevision)).
		String("parent_digest", p.ParentDigest).Value("rounding_rule", p.RoundingRule).
		Int("tolerance_nanoseconds", p.Tolerance.Nanoseconds()).Value("effective", p.Effective).
		Count("fallback_source_order", len(order))
	for _, id := range order {
		w.String("fallback_source", id)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p ConversionProfileRevision) computedDigest() string { return canonicalbytes.Digest(p.body()) }
func (p ConversionProfileRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

func NewConversionProfileRevision(p ConversionProfileRevision) (ConversionProfileRevision, error) {
	p.FallbackSourceOrder = append([]string(nil), p.FallbackSourceOrder...)
	p.CanonicalDigest = ""
	if err := p.Validate(); err != nil {
		return ConversionProfileRevision{}, err
	}
	p.CanonicalDigest = p.computedDigest()
	return p, nil
}

func NewConversionProfile(p ConversionProfileRevision) (ConversionProfileRevision, error) {
	return NewConversionProfileRevision(p)
}

// QuoteResolutionStatus describes exactly one selected observation or why
// selection was not safe.
type QuoteResolutionStatus string

const (
	ResolutionQuote    QuoteResolutionStatus = "FXQuoteRevision"
	ResolutionUnknown  QuoteResolutionStatus = "UNKNOWN"
	ResolutionStale    QuoteResolutionStatus = "STALE"
	ResolutionConflict QuoteResolutionStatus = "CONFLICT"
	Resolved                                 = ResolutionQuote
	Unknown                                  = ResolutionUnknown
	Stale                                    = ResolutionStale
	Conflict                                 = ResolutionConflict
)

// QuoteResolutionRequest contains all inputs needed for deterministic quote
// selection. Sources and quotes are snapshots, never an implicit live feed.
type QuoteResolutionRequest struct {
	BaseCurrency  string
	QuoteCurrency string
	AsOf          values.Instant
	KnownAt       values.Instant
	Profile       ConversionProfileRevision
	Sources       []RateSourceRevision
	Quotes        []FXQuoteRevision
}

// QuoteResolution is a single result. Quote is zero when Status is not
// ResolutionQuote; the status is not inferred from a zero rate.
type QuoteResolution struct {
	Status          QuoteResolutionStatus
	Quote           FXQuoteRevision
	SourcePriority  int
	SourceID        string
	SourceRevision  uint64
	ObservedAt      values.Instant
	KnownAt         values.Instant
	Confidence      Confidence
	CanonicalDigest string
}

type FXQuoteResolution = QuoteResolution

func (r QuoteResolution) Validate() error {
	switch r.Status {
	case ResolutionUnknown, ResolutionStale, ResolutionConflict:
		return nil
	case ResolutionQuote:
		if err := r.Quote.Validate(); err != nil {
			return err
		}
		if r.SourcePriority <= 0 || r.SourceID != r.Quote.SourceID || r.SourceRevision != r.Quote.SourceRevision {
			return fmt.Errorf("%w: selected source priority and identity are inconsistent", ErrInvalidQuoteResolution)
		}
		if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
			return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidQuoteResolution)
		}
		return nil
	default:
		return fmt.Errorf("%w: status %q is not declared", ErrInvalidQuoteResolution, r.Status)
	}
}

func (r QuoteResolution) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.fx.QuoteResolution", schemaVersion).
		String("status", string(r.Status)).Int("source_priority", int64(r.SourcePriority)).
		String("source_id", r.SourceID).Int("source_revision", int64(r.SourceRevision)).
		Optional("quote", r.Status == ResolutionQuote, r.Quote)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func (r QuoteResolution) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}
func (r QuoteResolution) canonicalBody() []byte {
	w := canonicalbytes.New("hcmnext.domains.fx.QuoteResolution", schemaVersion).
		String("status", string(r.Status)).Int("source_priority", int64(r.SourcePriority)).
		String("source_id", r.SourceID).Int("source_revision", int64(r.SourceRevision)).
		Optional("quote", r.Status == ResolutionQuote, r.Quote)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func sourcePairAllowed(s RateSourceRevision, base, quote string) bool {
	for _, pair := range s.Pairs {
		if pair.Base == base && pair.Quote == quote {
			return true
		}
	}
	return false
}

// ResolveQuote selects a quote from the supplied snapshots. A missing pair,
// stale pair, and ambiguous pair are returned as statuses rather than being
// replaced by a current or synthesized rate.
func ResolveQuote(req QuoteResolutionRequest) (QuoteResolution, error) {
	if err := req.Profile.Validate(); err != nil {
		return QuoteResolution{}, err
	}
	if err := req.AsOf.Validate(); err != nil {
		return QuoteResolution{}, fmt.Errorf("%w: as_of: %v", ErrInvalidQuoteResolution, err)
	}
	if err := req.KnownAt.Validate(); err != nil {
		return QuoteResolution{}, fmt.Errorf("%w: known_at: %v", ErrInvalidQuoteResolution, err)
	}
	if contains, err := req.Profile.Effective.ContainsInstant(req.AsOf); err != nil {
		return QuoteResolution{}, fmt.Errorf("%w: profile effective: %v", ErrInvalidQuoteResolution, err)
	} else if !contains {
		return QuoteResolution{Status: ResolutionUnknown}, nil
	}
	pair := CurrencyPair{Base: req.BaseCurrency, Quote: req.QuoteCurrency}
	if err := pair.Validate(); err != nil {
		return QuoteResolution{}, fmt.Errorf("%w: pair: %v", ErrInvalidQuoteResolution, err)
	}
	sources := make(map[string]RateSourceRevision, len(req.Sources))
	providers := make(map[string]string, len(req.Sources))
	for _, source := range req.Sources {
		if err := source.Validate(); err != nil {
			return QuoteResolution{}, err
		}
		if _, exists := sources[source.SourceID]; exists {
			return QuoteResolution{Status: ResolutionConflict}, nil
		}
		if prior, exists := providers[source.ProviderRef]; exists && prior != source.SourceID {
			return QuoteResolution{Status: ResolutionConflict}, nil
		}
		sources[source.SourceID] = source
		providers[source.ProviderRef] = source.SourceID
	}
	for _, quote := range req.Quotes {
		if err := quote.Validate(); err != nil {
			return QuoteResolution{}, err
		}
	}
	order := make(map[string]int, len(req.Profile.FallbackSourceOrder))
	for i, id := range req.Profile.FallbackSourceOrder {
		order[id] = i + 1
	}
	var candidates []FXQuoteRevision
	for _, quote := range req.Quotes {
		source, ok := sources[quote.SourceID]
		if !ok || source.Revision != quote.SourceRevision || !sourcePairAllowed(source, req.BaseCurrency, req.QuoteCurrency) {
			continue
		}
		when, _ := quote.observationTime()
		if when.After(req.AsOf) || quote.KnownAt.After(req.KnownAt) {
			continue
		}
		if contains, err := source.Effective.ContainsInstant(when); err != nil || !contains {
			continue
		}
		if priority, ok := order[quote.SourceID]; ok && priority > 0 {
			candidates = append(candidates, quote)
		} else if len(order) == 0 {
			candidates = append(candidates, quote)
		}
	}
	if len(candidates) == 0 {
		for _, quote := range req.Quotes {
			source, ok := sources[quote.SourceID]
			if !ok || source.Revision != quote.SourceRevision || !sourcePairAllowed(source, req.BaseCurrency, req.QuoteCurrency) {
				continue
			}
			when, _ := quote.observationTime()
			if when.Before(req.AsOf) || when.Compare(req.AsOf) == 0 {
				return QuoteResolution{Status: ResolutionStale}, nil
			}
		}
		return QuoteResolution{Status: ResolutionUnknown}, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		pi, pj := order[candidates[i].SourceID], order[candidates[j].SourceID]
		if pi == 0 {
			pi = sources[candidates[i].SourceID].Priority
		}
		if pj == 0 {
			pj = sources[candidates[j].SourceID].Priority
		}
		if pi != pj {
			return pi < pj
		}
		return candidates[i].QuoteID < candidates[j].QuoteID
	})
	bestPriority := order[candidates[0].SourceID]
	if bestPriority == 0 {
		bestPriority = sources[candidates[0].SourceID].Priority
	}
	if bestPriority == 0 {
		bestPriority = 1
	}
	best := candidates[:0]
	for _, quote := range candidates {
		p := order[quote.SourceID]
		if p == 0 {
			p = sources[quote.SourceID].Priority
		}
		if p == 0 {
			p = 1
		}
		if p == bestPriority {
			best = append(best, quote)
		}
	}
	if len(best) != 1 {
		return QuoteResolution{Status: ResolutionConflict}, nil
	}
	chosen := best[0]
	age := req.AsOf.Time().Sub(chosen.observationTimeMust().Time())
	if age < 0 || age > req.Profile.Tolerance {
		return QuoteResolution{Status: ResolutionStale}, nil
	}
	result := QuoteResolution{Status: ResolutionQuote, Quote: chosen, SourcePriority: bestPriority, SourceID: chosen.SourceID, SourceRevision: chosen.SourceRevision, ObservedAt: chosen.observationTimeMust(), KnownAt: chosen.KnownAt, Confidence: chosen.Confidence}
	result.CanonicalDigest = result.computedDigest()
	return result, nil
}

func Resolve(req QuoteResolutionRequest) (QuoteResolution, error) {
	return ResolveQuote(req)
}

func (q FXQuoteRevision) observationTimeMust() values.Instant {
	when, _ := q.observationTime()
	return when
}

// ConversionResult records the exact decimal conversion and the quote that
// supplied it. No float or ambient current rate participates in the result.
type ConversionResult struct {
	SourceAmount    values.Decimal
	ConvertedAmount values.Decimal
	QuoteID         string
	QuoteDigest     string
	ProfileID       string
	ProfileRevision uint64
	RoundingRule    RoundingRule
	CanonicalDigest string
}

func (r ConversionResult) Validate() error {
	if err := r.SourceAmount.Validate(); err != nil {
		return fmt.Errorf("%w: source_amount: %v", ErrInvalidConversion, err)
	}
	if err := r.ConvertedAmount.Validate(); err != nil {
		return fmt.Errorf("%w: converted_amount: %v", ErrInvalidConversion, err)
	}
	if strings.TrimSpace(r.QuoteID) == "" || strings.TrimSpace(r.QuoteDigest) == "" {
		return fmt.Errorf("%w: quote_id and quote_digest are required", ErrInvalidConversion)
	}
	if strings.TrimSpace(r.ProfileID) == "" || r.ProfileRevision == 0 {
		return fmt.Errorf("%w: profile_id and profile_revision are required", ErrInvalidConversion)
	}
	if err := r.RoundingRule.Validate(); err != nil {
		return err
	}
	if r.CanonicalDigest == "" || r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidConversion)
	}
	return nil
}

func (r ConversionResult) computedDigest() string {
	w := canonicalbytes.New("hcmnext.domains.fx.ConversionResult", schemaVersion).
		Value("source_amount", r.SourceAmount).Value("converted_amount", r.ConvertedAmount).
		String("quote_id", r.QuoteID).String("quote_digest", r.QuoteDigest).String("profile_id", r.ProfileID).
		Int("profile_revision", int64(r.ProfileRevision)).Value("rounding_rule", r.RoundingRule)
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// Convert applies one resolved quote using the profile's explicit decimal
// rounding rule.
func (p ConversionProfileRevision) Convert(amount values.Decimal, resolution QuoteResolution) (ConversionResult, error) {
	if err := p.Validate(); err != nil {
		return ConversionResult{}, err
	}
	if err := amount.Validate(); err != nil {
		return ConversionResult{}, fmt.Errorf("%w: source_amount: %v", ErrInvalidConversion, err)
	}
	if resolution.Status != ResolutionQuote {
		return ConversionResult{}, fmt.Errorf("%w: quote resolution status is %s", ErrInvalidConversion, resolution.Status)
	}
	if err := resolution.Validate(); err != nil {
		return ConversionResult{}, err
	}
	converted, err := amount.Mul(resolution.Quote.Rate, p.RoundingRule.Scale, p.RoundingRule.Mode)
	if err != nil {
		return ConversionResult{}, fmt.Errorf("%w: decimal multiplication: %v", ErrInvalidConversion, err)
	}
	result := ConversionResult{SourceAmount: amount, ConvertedAmount: converted, QuoteID: resolution.Quote.QuoteID, QuoteDigest: resolution.Quote.CanonicalDigest, ProfileID: p.ProfileID, ProfileRevision: p.Revision, RoundingRule: p.RoundingRule}
	result.CanonicalDigest = result.computedDigest()
	return result, nil
}

func Convert(amount values.Decimal, resolution QuoteResolution, profile ConversionProfileRevision) (ConversionResult, error) {
	return profile.Convert(amount, resolution)
}

// ConvertQuote is a convenience for callers that already selected a quote.
// It still requires an explicit as-of instant so freshness cannot be inferred
// from the wall clock.
func ConvertQuote(amount values.Decimal, quote FXQuoteRevision, asOf values.Instant, profile ConversionProfileRevision) (ConversionResult, error) {
	if err := quote.Validate(); err != nil {
		return ConversionResult{}, err
	}
	if err := asOf.Validate(); err != nil {
		return ConversionResult{}, fmt.Errorf("%w: as_of: %v", ErrInvalidConversion, err)
	}
	when := quote.observationTimeMust()
	if when.After(asOf) || asOf.Time().Sub(when.Time()) > profile.Tolerance {
		return ConversionResult{}, ErrQuoteStale
	}
	resolution := QuoteResolution{Status: ResolutionQuote, Quote: quote, SourcePriority: 1, SourceID: quote.SourceID, SourceRevision: quote.SourceRevision, ObservedAt: when, KnownAt: quote.KnownAt, Confidence: quote.Confidence}
	resolution.CanonicalDigest = resolution.computedDigest()
	return profile.Convert(amount, resolution)
}

// Explain returns an audit-safe summary of a quote resolution.
func (r QuoteResolution) Explain() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	if r.Status != ResolutionQuote {
		return fmt.Sprintf("FX quote resolution: %s", r.Status), nil
	}
	return fmt.Sprintf("FX quote resolution: %s source %s priority %d quote %s digest %s confidence %s", r.Status, r.SourceID, r.SourcePriority, r.Quote.QuoteID, r.Quote.CanonicalDigest, r.Confidence), nil
}

func Explain(r QuoteResolution) (string, error) { return r.Explain() }
