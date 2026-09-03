// Package rewards owns compensation semantics for P1A: the versioned pay-band
// catalog port, the governed pay-band evaluation
// (hcmnext.rewards.evaluate_pay_band_position/v1) and the pure, version-pinned
// compensation simulation (hcmnext.rewards.simulate_compensation/v1) that
// COMP-006 specifies.
//
// Semantic owner: Compensation domain. Phase: P1A.
//
// Everything here is a calculation. Nothing in this package writes, reserves,
// enqueues or calls out; every result carries an EffectCounters that says so
// and a receipt that refuses to exist if it did not. The arithmetic is exact
// fixed-point decimal from internal/kernel/values with declared scales and
// rounding modes at every point, because a compensation number that rounds
// differently on a different machine is not a simulation of anything.
package rewards

import (
	"context"
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Intent identity for the pay-band evaluation contract.
const (
	// EvaluatePayBandIntentType is the catalog identifier.
	EvaluatePayBandIntentType = "hcmnext.rewards.evaluate_pay_band_position"
	// EvaluatePayBandIntentVersion is the contract version.
	EvaluatePayBandIntentVersion = "v1"
	// BandRulePackVersion versions the advisory/blocking classification below.
	BandRulePackVersion = "rewards.payband.rules/1.0.0"
)

const rewardsSchemaVer = 1

// Catalog and evaluation errors. All are matchable with errors.Is.
var (
	// ErrBandNotFound is returned by a catalog when no band governs the query.
	// It is a typed miss, not a zero band: an unmatched worker must surface as
	// NEEDS_DATA, never as a band of zero width.
	ErrBandNotFound = errors.New("rewards: no pay band governs the requested scope")
	// ErrCatalogFailed wraps a transport or storage failure from the port.
	ErrCatalogFailed = errors.New("rewards: pay band catalog failed")
	// ErrBandQueryInvalid is returned for a malformed band query.
	ErrBandQueryInvalid = errors.New("rewards: pay band query is invalid")
	// ErrCatalogUnpinned is returned when a catalog answers without naming its
	// own version. An unpinned band cannot be cited in a proposal digest.
	ErrCatalogUnpinned = errors.New("rewards: pay band catalog answered without a catalog version")
	// ErrBandScopeMismatch is returned when the catalog answers with a band
	// scoped to a different job, grade or pay zone than was asked about.
	ErrBandScopeMismatch = errors.New("rewards: catalog answered with a band outside the requested scope")
)

// BandQuery addresses one band in the catalog. Currency is part of the
// question, not an inference from the worker: a query that does not say which
// currency it means cannot be answered without guessing an FX decision.
type BandQuery struct {
	Tenant   values.TenantId
	JobCode  string
	Grade    string
	PayZone  string
	Currency string
	// AsOf is the business date the band must be effective on.
	AsOf values.LocalDate
}

// Validate reports whether the query is fully specified.
func (q BandQuery) Validate() error {
	if err := q.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %w", ErrBandQueryInvalid, err)
	}
	switch {
	case q.JobCode == "":
		return fmt.Errorf("%w: job code is required", ErrBandQueryInvalid)
	case q.Grade == "":
		return fmt.Errorf("%w: grade is required", ErrBandQueryInvalid)
	case q.PayZone == "":
		return fmt.Errorf("%w: pay zone is required", ErrBandQueryInvalid)
	case q.Currency == "":
		return fmt.Errorf("%w: currency is required", ErrBandQueryInvalid)
	}
	if err := q.AsOf.Validate(); err != nil {
		return fmt.Errorf("%w: as-of: %w", ErrBandQueryInvalid, err)
	}
	return nil
}

// Scope returns the engine-level scope this query addresses.
func (q BandQuery) Scope() payband.Scope {
	return payband.Scope{JobCode: q.JobCode, Grade: q.Grade, PayZone: q.PayZone}
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (q BandQuery) Canonical() []byte {
	if q.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.BandQuery", rewardsSchemaVer).
		String("tenant", string(q.Tenant)).
		String("job_code", q.JobCode).
		String("grade", q.Grade).
		String("pay_zone", q.PayZone).
		String("currency", q.Currency).
		Value("as_of", q.AsOf).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// BandRecord is one catalog answer: the band itself plus the governance
// metadata that decides how a violation is treated and how the answer is
// cited later.
type BandRecord struct {
	Band payband.Band
	// CatalogVersion pins the published catalog the band was read from.
	CatalogVersion string
	// Blocking says whether an out-of-band amount blocks or merely advises.
	// The compensation contract requires this to be recorded per band rather
	// than assumed, because the same band is blocking in one jurisdiction and
	// advisory in another.
	Blocking bool
	// Authority is the source-authority decision for the band data.
	Authority evidence.SourceAuthority
	// Provenance is where the band came from.
	Provenance evidence.Provenance
}

// Validate reports whether the record is complete and citable.
func (r BandRecord) Validate() error {
	if err := r.Band.Validate(); err != nil {
		return err
	}
	if r.CatalogVersion == "" {
		return fmt.Errorf("%w: band %s", ErrCatalogUnpinned, r.Band.ID)
	}
	if err := r.Authority.Validate(); err != nil {
		return fmt.Errorf("rewards: band %s: %w", r.Band.ID, err)
	}
	return r.Provenance.Validate()
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (r BandRecord) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.BandRecord", rewardsSchemaVer).
		Value("band", r.Band).
		String("catalog_version", r.CatalogVersion).
		Bool("blocking", r.Blocking).
		Value("authority", r.Authority).
		Value("provenance", r.Provenance).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// PayBandCatalog is the governed-reference read port the intent kernel wires
// to a real catalog. It is the only way this package obtains a band.
type PayBandCatalog interface {
	// LookupBand returns the band governing the query, or ErrBandNotFound.
	LookupBand(ctx context.Context, q BandQuery) (BandRecord, error)
}

// BandOutcome classifies a position for governance, distinct from the
// engine's arithmetic placement. The engine says where the amount is; this
// says what that means for the proposal.
type BandOutcome uint8

// Band outcomes.
const (
	// BandOutcomeUnspecified is the zero value and is never legal.
	BandOutcomeUnspecified BandOutcome = iota
	// BandOutcomeWithin means the amount sits inside the band.
	BandOutcomeWithin
	// BandOutcomeExceptionAdvisory means the amount is out of band on a band
	// whose check is advisory.
	BandOutcomeExceptionAdvisory
	// BandOutcomeExceptionBlocking means the amount is out of band on a band
	// whose check blocks.
	BandOutcomeExceptionBlocking
)

var bandOutcomeWire = map[BandOutcome]string{
	BandOutcomeWithin:            "WITHIN_BAND",
	BandOutcomeExceptionAdvisory: "BAND_EXCEPTION_ADVISORY",
	BandOutcomeExceptionBlocking: "BAND_EXCEPTION_BLOCKING",
}

// String returns the stable wire token, or "BAND_OUTCOME_UNSPECIFIED".
func (o BandOutcome) String() string {
	if s, ok := bandOutcomeWire[o]; ok {
		return s
	}
	return "BAND_OUTCOME_UNSPECIFIED"
}

// PayBandEvaluation is the governed result of one band evaluation: the pure
// position, the governance outcome, and every version needed to replay it.
type PayBandEvaluation struct {
	IntentType    string
	IntentVersion string

	Query    BandQuery
	Position payband.Position
	Outcome  BandOutcome

	CatalogVersion  string
	RulePackVersion string
	Authority       evidence.SourceAuthority
	Provenance      evidence.Provenance

	InputsDigest string
	ResultDigest string
	Effects      evidence.EffectCounters
}

// canonicalBody encodes everything the result digest covers.
func (e PayBandEvaluation) canonicalBody() ([]byte, error) {
	return canonicalbytes.New("hcmnext.domains.rewards.PayBandEvaluation", rewardsSchemaVer).
		String("intent_type", e.IntentType).
		String("intent_version", e.IntentVersion).
		Value("query", e.Query).
		Value("position", e.Position).
		String("outcome", e.Outcome.String()).
		String("catalog_version", e.CatalogVersion).
		String("rule_pack_version", e.RulePackVersion).
		Value("authority", e.Authority).
		Value("provenance", e.Provenance).
		Value("effects", e.Effects).
		Bytes()
}

// Canonical returns the canonical byte encoding, or nil when incoherent.
func (e PayBandEvaluation) Canonical() []byte {
	body, err := e.canonicalBody()
	if err != nil {
		return nil
	}
	raw, err := canonicalbytes.New("hcmnext.domains.rewards.PayBandEvaluationEnvelope", rewardsSchemaVer).
		Field("body", body).
		String("inputs_digest", e.InputsDigest).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// EvaluatePayBandPosition resolves the governing band and returns the position
// of amount within it.
//
// The catalog answer is checked against the question. A catalog that returns a
// band for a different job, grade or pay zone is a fault rather than a
// finding: silently accepting it would make the compa-ratio a number about
// some other population.
func EvaluatePayBandPosition(ctx context.Context, catalog PayBandCatalog, q BandQuery, amount values.Money) (PayBandEvaluation, error) {
	if catalog == nil {
		return PayBandEvaluation{}, fmt.Errorf("%w: no pay band catalog", ErrBandQueryInvalid)
	}
	if err := q.Validate(); err != nil {
		return PayBandEvaluation{}, err
	}
	if err := amount.Validate(); err != nil {
		return PayBandEvaluation{}, fmt.Errorf("rewards: amount: %w", err)
	}
	if amount.Currency() != q.Currency {
		return PayBandEvaluation{}, fmt.Errorf("%w: amount %s, query %s",
			payband.ErrAmountCurrency, amount.Currency(), q.Currency)
	}

	inputsDigest, err := canonicalbytes.New("hcmnext.domains.rewards.EvaluatePayBandRequest", rewardsSchemaVer).
		String("intent_type", EvaluatePayBandIntentType).
		String("intent_version", EvaluatePayBandIntentVersion).
		Value("query", q).
		Value("amount", amount).
		Digest()
	if err != nil {
		return PayBandEvaluation{}, err
	}

	record, err := catalog.LookupBand(ctx, q)
	if err != nil {
		if errors.Is(err, ErrBandNotFound) {
			return PayBandEvaluation{}, err
		}
		return PayBandEvaluation{}, fmt.Errorf("%w: %w", ErrCatalogFailed, err)
	}
	if err := record.Validate(); err != nil {
		return PayBandEvaluation{}, err
	}
	if record.Band.Scope != q.Scope() {
		return PayBandEvaluation{}, fmt.Errorf("%w: asked %+v, answered %+v",
			ErrBandScopeMismatch, q.Scope(), record.Band.Scope)
	}
	if record.Band.Currency() != q.Currency {
		return PayBandEvaluation{}, fmt.Errorf("%w: band %s, query %s",
			ErrBandScopeMismatch, record.Band.Currency(), q.Currency)
	}

	position, err := payband.Evaluate(record.Band, amount)
	if err != nil {
		return PayBandEvaluation{}, err
	}

	outcome := BandOutcomeWithin
	if position.Placement != payband.PlacementInBand {
		outcome = BandOutcomeExceptionAdvisory
		if record.Blocking {
			outcome = BandOutcomeExceptionBlocking
		}
	}

	result := PayBandEvaluation{
		IntentType:      EvaluatePayBandIntentType,
		IntentVersion:   EvaluatePayBandIntentVersion,
		Query:           q,
		Position:        position,
		Outcome:         outcome,
		CatalogVersion:  record.CatalogVersion,
		RulePackVersion: BandRulePackVersion,
		Authority:       record.Authority,
		Provenance:      record.Provenance,
		InputsDigest:    inputsDigest,
		Effects:         evidence.ZeroEffects(),
	}
	body, err := result.canonicalBody()
	if err != nil {
		return PayBandEvaluation{}, err
	}
	result.ResultDigest = canonicalbytes.Digest(body)
	return result, nil
}
