// Package fxstore persists the tenant-scoped FX revision projections declared
// by migrations/00092_fx.sql. Rich FX validation remains in internal/domains/fx;
// this adapter owns transaction scope, RLS context, lineage CAS and digest
// representation at the PostgreSQL boundary.
package fxstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/fx"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction-opening capability used by Store. Callers should
// provide a connection already operating as hcmnext_app when testing RLS.
type DB interface{ dbport.Beginner }

// Store implements [fx.Store] over migration 00092.
type Store struct{ db DB }

var _ fx.Store = (*Store)(nil)

// New returns a PostgreSQL-backed FX store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenant string, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return &fx.StoreError{Code: fx.StoreCodeInvalid, Detail: "database capability is required"}
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("fxstore: begin transaction: %w", err)
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
		return fmt.Errorf("fxstore: commit transaction: %w", err)
	}
	return nil
}

func parseTenant(tenant string) (uuid.UUID, error) {
	id, err := uuid.Parse(tenant)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, &fx.StoreError{Code: fx.StoreCodeInvalid, Detail: "tenant id must be a non-nil UUID"}
	}
	return id, nil
}

func invalid(detail string) error {
	return &fx.StoreError{Code: fx.StoreCodeInvalid, Detail: detail}
}

func notFound(detail string) error {
	return &fx.StoreError{Code: fx.StoreCodeNotFound, Detail: detail}
}

func duplicate(detail string) error {
	return &fx.StoreError{Code: fx.StoreCodeDuplicateRevision, Detail: detail}
}

func referenceNotFound(detail string) error {
	return &fx.StoreError{Code: fx.StoreCodeReferenceNotFound, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &fx.StoreError{Code: fx.StoreCodeStaleCAS, Expected: expected, Actual: actual, Detail: detail}
}

func storedDigest(digest string) string { return strings.TrimPrefix(digest, "sha256:") }

func domainDigest(digest string) string {
	if digest == "" || strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return "sha256:" + digest
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// SaveRateSource appends one immutable source revision. For successors, the
// parent must be the current head in the same tenant; the transaction-scoped
// advisory lock serializes competing successors of one source.
func (s *Store) SaveRateSource(ctx context.Context, tenant string, source fx.RateSourceRevision) error {
	sealed := source
	var err error
	if source.CanonicalDigest == "" {
		sealed, err = fx.NewRateSourceRevision(source)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := sealed.Validate(); err != nil {
		return invalid(err.Error())
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	from, to, err := intervalBounds(sealed.Effective)
	if err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		if err := requireSourceHead(ctx, tx, tenantID, sealed); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO fx_rate_source_revision (
				row_id, tenant_id, source_id, revision, parent_revision,
				parent_digest, effective_from, effective_to, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, sealed.SourceID, int64(sealed.Revision), nullableRevision(sealed.ParentRevision),
			nullableDigest(sealed.ParentDigest), from, to, storedDigest(sealed.CanonicalDigest))
		if err != nil {
			return fmt.Errorf("fxstore: insert rate source %s/%d: %w", sealed.SourceID, sealed.Revision, err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("rate source revision %s/%d already exists", sealed.SourceID, sealed.Revision))
		}
		return nil
	})
}

func requireSourceHead(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, source fx.RateSourceRevision) error {
	lockKey := tenantID.String() + ":fx-rate-source:" + source.SourceID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("fxstore: lock rate source head: %w", err)
	}
	var revisionExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM fx_rate_source_revision
			WHERE tenant_id=$1 AND source_id=$2 AND revision=$3)`,
		tenantID, source.SourceID, int64(source.Revision)).Scan(&revisionExists); err != nil {
		return fmt.Errorf("fxstore: check rate source revision: %w", err)
	}
	if revisionExists {
		return duplicate(fmt.Sprintf("rate source revision %s/%d already exists", source.SourceID, source.Revision))
	}
	var (
		latestRevision int64
		latestDigest   string
	)
	err := tx.QueryRow(ctx, `
		SELECT revision, canonical_digest
		FROM fx_rate_source_revision
		WHERE tenant_id=$1 AND source_id=$2
		ORDER BY revision DESC LIMIT 1`, tenantID, source.SourceID).Scan(&latestRevision, &latestDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		if source.Revision == 1 {
			return nil
		}
		return stale(source.ParentRevision, 0, "rate source successor has no current parent")
	}
	if err != nil {
		return fmt.Errorf("fxstore: read rate source head: %w", err)
	}
	if source.Revision == 1 {
		return stale(0, uint64(latestRevision), "rate source already has a current revision")
	}
	if uint64(latestRevision) != source.ParentRevision || domainDigest(latestDigest) != source.ParentDigest {
		return stale(source.ParentRevision, uint64(latestRevision), "rate source predecessor is stale")
	}
	return nil
}

// LoadRateSource loads the durable source revision projection.
func (s *Store) LoadRateSource(ctx context.Context, tenant, sourceID string, revision uint64) (fx.RateSourceRevision, error) {
	if sourceID == "" || revision == 0 {
		return fx.RateSourceRevision{}, invalid("source id and positive revision are required")
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return fx.RateSourceRevision{}, err
	}
	var out fx.RateSourceRevision
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var (
			parentRevision *int64
			parentDigest   *string
			from, to       *time.Time
			storedRevision int64
			digest         string
		)
		err := tx.QueryRow(ctx, `
			SELECT revision, parent_revision, parent_digest, effective_from,
				effective_to, canonical_digest
			FROM fx_rate_source_revision
			WHERE tenant_id=$1 AND source_id=$2 AND revision=$3`, tenantID, sourceID, int64(revision)).Scan(
			&storedRevision, &parentRevision, &parentDigest, &from, &to, &digest)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("rate source revision %s/%d", sourceID, revision))
			}
			return fmt.Errorf("fxstore: load rate source: %w", err)
		}
		interval, err := intervalFromBounds(from, to)
		if err != nil {
			return invalid("stored rate source effective interval is invalid")
		}
		out = fx.RateSourceRevision{SourceID: sourceID, Revision: uint64(storedRevision), Effective: interval, CanonicalDigest: domainDigest(digest)}
		if parentRevision != nil {
			out.ParentRevision = uint64(*parentRevision)
		}
		if parentDigest != nil {
			out.ParentDigest = domainDigest(*parentDigest)
		}
		return nil
	})
	return out, err
}

// SaveQuote appends one immutable quote against an existing source revision.
func (s *Store) SaveQuote(ctx context.Context, tenant string, quote fx.FXQuoteRevision) error {
	sealed := quote
	var err error
	if quote.CanonicalDigest == "" {
		sealed, err = fx.NewFXQuoteRevision(quote)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := sealed.Validate(); err != nil {
		return invalid(err.Error())
	}
	if sealed.Revision != 1 || sealed.ParentQuoteID != "" || sealed.ParentDigest != "" {
		return stale(sealed.Revision, 0, "quote successors must use SaveQuoteSuccessor")
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	when := sealed.AsOf
	if !when.IsSet() {
		when = sealed.EffectiveAt
	}
	if !when.IsSet() {
		when = sealed.ObservedAt
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM fx_rate_source_revision
				WHERE tenant_id=$1 AND source_id=$2 AND revision=$3)`,
			tenantID, sealed.SourceID, int64(sealed.SourceRevision)).Scan(&exists); err != nil {
			return fmt.Errorf("fxstore: check quote source: %w", err)
		}
		if !exists {
			return referenceNotFound(fmt.Sprintf("quote source revision %s/%d", sealed.SourceID, sealed.SourceRevision))
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO fx_quote_revision (
				row_id, tenant_id, quote_id, source_id, source_revision,
				as_of, effective_at, observed_at, known_at, canonical_digest,
				quote_revision, parent_quote_id, parent_digest, base_currency, quote_currency,
				rate, rate_scale, market_convention, confidence, as_of_submicrosecond, known_at_submicrosecond)
			VALUES ($1,$2,$3,$4,$5,$6,$6,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, sealed.QuoteID, sealed.SourceID, int64(sealed.SourceRevision),
			when.Time(), sealed.KnownAt.Time(), storedDigest(sealed.CanonicalDigest), int64(sealed.Revision), nullableDigest(sealed.ParentQuoteID), nullableDigest(sealed.ParentDigest),
			sealed.BaseCurrency, sealed.QuoteCurrency, sealed.Rate.String(), sealed.Rate.Scale(), string(sealed.MarketConvention), string(sealed.Confidence), int16(when.Time().Nanosecond()%1000), int16(sealed.KnownAt.Time().Nanosecond()%1000))
		if err != nil {
			return fmt.Errorf("fxstore: insert quote %s: %w", sealed.QuoteID, err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("quote revision %s already exists", sealed.QuoteID))
		}
		return nil
	})
}

// SaveQuoteSuccessor appends a correction fenced by the predecessor digest and
// revision. The partial unique parent index permits one winner per head.
func (s *Store) SaveQuoteSuccessor(ctx context.Context, tenant string, successor fx.FXQuoteRevision) error {
	if successor.ParentQuoteID == "" {
		return invalid("quote successor parent is required")
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		lockKey := tenantID.String() + ":fx-quote:" + successor.ParentQuoteID
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
			return fmt.Errorf("fxstore: lock quote predecessor: %w", err)
		}
		previous, err := loadQuoteTx(ctx, tx, tenantID, successor.ParentQuoteID)
		if errors.Is(err, fx.ErrStoreNotFound) {
			return stale(successor.Revision, 0, "quote predecessor does not exist")
		}
		if err != nil {
			return err
		}
		sealed, err := fx.CorrectQuote(previous, successor)
		if err != nil {
			return stale(successor.Revision, previous.Revision, err.Error())
		}
		when := sealed.AsOf
		if !when.IsSet() {
			when = sealed.EffectiveAt
		}
		if !when.IsSet() {
			when = sealed.ObservedAt
		}
		_, err = tx.Exec(ctx, `INSERT INTO fx_quote_revision (row_id,tenant_id,quote_id,source_id,source_revision,as_of,effective_at,observed_at,known_at,canonical_digest,quote_revision,parent_quote_id,parent_digest,base_currency,quote_currency,rate,rate_scale,market_convention,confidence,as_of_submicrosecond,known_at_submicrosecond) VALUES ($1,$2,$3,$4,$5,$6,$6,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, uuid.New(), tenantID, sealed.QuoteID, sealed.SourceID, int64(sealed.SourceRevision), when.Time(), sealed.KnownAt.Time(), storedDigest(sealed.CanonicalDigest), int64(sealed.Revision), sealed.ParentQuoteID, storedDigest(sealed.ParentDigest), sealed.BaseCurrency, sealed.QuoteCurrency, sealed.Rate.String(), sealed.Rate.Scale(), string(sealed.MarketConvention), string(sealed.Confidence), int16(when.Time().Nanosecond()%1000), int16(sealed.KnownAt.Time().Nanosecond()%1000))
		if err != nil {
			return stale(previous.Revision, previous.Revision, "quote successor lost compare-and-set")
		}
		return nil
	})
}

// LoadQuote loads the durable quote projection.
func (s *Store) LoadQuote(ctx context.Context, tenant, quoteID string) (fx.FXQuoteRevision, error) {
	if quoteID == "" {
		return fx.FXQuoteRevision{}, invalid("quote id is required")
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return fx.FXQuoteRevision{}, err
	}
	var out fx.FXQuoteRevision
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var loadErr error
		out, loadErr = loadQuoteTx(ctx, tx, tenantID, quoteID)
		return loadErr
	})
	return out, err
}

func loadQuoteTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, quoteID string) (fx.FXQuoteRevision, error) {
	var sourceID, digest string
	var sourceRevision, quoteRevision int64
	var parentID, parentDigest, base, quote, rate, convention, confidence *string
	var rateScale *int16
	var asOf, effective, observed, known *time.Time
	var asOfSub, knownSub *int16
	err := tx.QueryRow(ctx, `SELECT source_id,source_revision,as_of,effective_at,observed_at,known_at,canonical_digest,quote_revision,parent_quote_id,parent_digest,base_currency,quote_currency,rate,rate_scale,market_convention,confidence,as_of_submicrosecond,known_at_submicrosecond FROM fx_quote_revision WHERE tenant_id=$1 AND quote_id=$2`, tenantID, quoteID).Scan(&sourceID, &sourceRevision, &asOf, &effective, &observed, &known, &digest, &quoteRevision, &parentID, &parentDigest, &base, &quote, &rate, &rateScale, &convention, &confidence, &asOfSub, &knownSub)
	if errors.Is(err, dbport.ErrNoRows) {
		return fx.FXQuoteRevision{}, notFound(fmt.Sprintf("quote revision %s", quoteID))
	}
	if err != nil {
		return fx.FXQuoteRevision{}, fmt.Errorf("fxstore: load quote: %w", err)
	}
	if asOf == nil || known == nil || base == nil || quote == nil || rate == nil || rateScale == nil || convention == nil || confidence == nil || asOfSub == nil || knownSub == nil {
		return fx.FXQuoteRevision{}, invalid("stored quote policy is incomplete")
	}
	rateValue, err := values.NewDecimal(*rate, int32(*rateScale), values.RoundingExactRequired)
	if err != nil {
		return fx.FXQuoteRevision{}, invalid("stored quote rate is invalid")
	}
	asOfValue := values.NewInstant(asOf.UTC().Add(time.Duration(*asOfSub)))
	knownValue := values.NewInstant(known.UTC().Add(time.Duration(*knownSub)))
	out := fx.FXQuoteRevision{QuoteID: quoteID, Revision: uint64(quoteRevision), ParentQuoteID: deref(parentID), ParentDigest: domainDigest(deref(parentDigest)), SourceID: sourceID, SourceRevision: uint64(sourceRevision), BaseCurrency: *base, QuoteCurrency: *quote, Rate: rateValue, AsOf: asOfValue, EffectiveAt: asOfValue, ObservedAt: asOfValue, KnownAt: knownValue, MarketConvention: fx.MarketConvention(*convention), Confidence: fx.Confidence(*confidence), CanonicalDigest: domainDigest(digest)}
	if err := out.Validate(); err != nil {
		return fx.FXQuoteRevision{}, invalid("stored quote does not match its canonical digest or policy")
	}
	return out, nil
}

// SaveConversionProfile appends one immutable conversion-profile revision.
func (s *Store) SaveConversionProfile(ctx context.Context, tenant string, profile fx.ConversionProfileRevision) error {
	sealed := profile
	var err error
	if profile.CanonicalDigest == "" {
		sealed, err = fx.NewConversionProfileRevision(profile)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := sealed.Validate(); err != nil {
		return invalid(err.Error())
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return err
	}
	from, to, err := intervalBounds(sealed.Effective)
	if err != nil {
		return invalid(err.Error())
	}
	fromSubmicrosecond := int16(from.Nanosecond() % 1000)
	var toSubmicrosecond *int16
	if to != nil {
		part := int16(to.Nanosecond() % 1000)
		toSubmicrosecond = &part
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		if err := requireProfileHead(ctx, tx, tenantID, sealed); err != nil {
			return err
		}
		affected, err := tx.Exec(ctx, `
			INSERT INTO fx_conversion_profile_revision (
				row_id, tenant_id, profile_id, revision, parent_revision,
				parent_digest, canonical_digest, rounding_scale, rounding_mode, tolerance_nanoseconds,
				fallback_source_order, triangulation_currencies, effective_from, effective_to,
				effective_from_submicrosecond, effective_to_submicrosecond)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT DO NOTHING`,
			uuid.New(), tenantID, sealed.ProfileID, int64(sealed.Revision), nullableRevision(sealed.ParentRevision), nullableDigest(sealed.ParentDigest), storedDigest(sealed.CanonicalDigest), sealed.RoundingRule.Scale, sealed.RoundingRule.Mode.String(), sealed.Tolerance.Nanoseconds(), sealed.FallbackSourceOrder, sealed.TriangulationCurrencies, from, to, fromSubmicrosecond, toSubmicrosecond)
		if err != nil {
			return fmt.Errorf("fxstore: insert conversion profile %s/%d: %w", sealed.ProfileID, sealed.Revision, err)
		}
		if affected == 0 {
			return duplicate(fmt.Sprintf("conversion profile revision %s/%d already exists", profile.ProfileID, profile.Revision))
		}
		return nil
	})
}

func requireProfileHead(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, profile fx.ConversionProfileRevision) error {
	lockKey := tenantID.String() + ":fx-conversion-profile:" + profile.ProfileID
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("fxstore: lock conversion profile head: %w", err)
	}
	var revisionExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM fx_conversion_profile_revision
			WHERE tenant_id=$1 AND profile_id=$2 AND revision=$3)`,
		tenantID, profile.ProfileID, int64(profile.Revision)).Scan(&revisionExists); err != nil {
		return fmt.Errorf("fxstore: check conversion profile revision: %w", err)
	}
	if revisionExists {
		return duplicate(fmt.Sprintf("conversion profile revision %s/%d already exists", profile.ProfileID, profile.Revision))
	}
	var (
		latestRevision int64
		latestDigest   string
	)
	err := tx.QueryRow(ctx, `
		SELECT revision, canonical_digest
		FROM fx_conversion_profile_revision
		WHERE tenant_id=$1 AND profile_id=$2
		ORDER BY revision DESC LIMIT 1`, tenantID, profile.ProfileID).Scan(&latestRevision, &latestDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		if profile.Revision == 1 {
			return nil
		}
		return stale(profile.ParentRevision, 0, "conversion profile successor has no current parent")
	}
	if err != nil {
		return fmt.Errorf("fxstore: read conversion profile head: %w", err)
	}
	if profile.Revision == 1 {
		return stale(0, uint64(latestRevision), "conversion profile already has a current revision")
	}
	if uint64(latestRevision) != profile.ParentRevision || domainDigest(latestDigest) != profile.ParentDigest {
		return stale(profile.ParentRevision, uint64(latestRevision), "conversion profile predecessor is stale")
	}
	return nil
}

// LoadConversionProfile loads the durable conversion-profile projection.
func (s *Store) LoadConversionProfile(ctx context.Context, tenant, profileID string, revision uint64) (fx.ConversionProfileRevision, error) {
	if profileID == "" || revision == 0 {
		return fx.ConversionProfileRevision{}, invalid("profile id and positive revision are required")
	}
	tenantID, err := parseTenant(tenant)
	if err != nil {
		return fx.ConversionProfileRevision{}, err
	}
	var out fx.ConversionProfileRevision
	err = s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var (
			storedRevision               int64
			parentRevision               *int64
			parentDigest                 *string
			digest                       string
			roundingScale                *int32
			roundingMode                 *string
			toleranceNanos               *int64
			fallbackOrder, triangulation []string
			effectiveFrom, effectiveTo   *time.Time
			fromSubmicrosecond           *int16
			toSubmicrosecond             *int16
		)
		err := tx.QueryRow(ctx, `
			SELECT revision, parent_revision, parent_digest, canonical_digest, rounding_scale, rounding_mode,
				tolerance_nanoseconds, fallback_source_order, triangulation_currencies, effective_from, effective_to,
				effective_from_submicrosecond, effective_to_submicrosecond
			FROM fx_conversion_profile_revision
			WHERE tenant_id=$1 AND profile_id=$2 AND revision=$3`, tenantID, profileID, int64(revision)).Scan(
			&storedRevision, &parentRevision, &parentDigest, &digest, &roundingScale, &roundingMode, &toleranceNanos, &fallbackOrder, &triangulation, &effectiveFrom, &effectiveTo, &fromSubmicrosecond, &toSubmicrosecond)
		if err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return notFound(fmt.Sprintf("conversion profile revision %s/%d", profileID, revision))
			}
			return fmt.Errorf("fxstore: load conversion profile: %w", err)
		}
		if roundingScale == nil || roundingMode == nil || toleranceNanos == nil || fallbackOrder == nil || effectiveFrom == nil || fromSubmicrosecond == nil || (effectiveTo != nil && toSubmicrosecond == nil) {
			return invalid("conversion profile policy is incomplete")
		}
		mode, parseErr := values.ParseRoundingMode(*roundingMode)
		if parseErr != nil {
			return invalid("conversion profile rounding mode is invalid")
		}
		fromWithNanos := effectiveFrom.Add(time.Duration(*fromSubmicrosecond))
		var toWithNanos *time.Time
		if effectiveTo != nil {
			adjusted := effectiveTo.Add(time.Duration(*toSubmicrosecond))
			toWithNanos = &adjusted
		}
		interval, intervalErr := intervalFromBounds(&fromWithNanos, toWithNanos)
		if intervalErr != nil {
			return invalid("conversion profile effective interval is invalid")
		}
		out = fx.ConversionProfileRevision{ProfileID: profileID, Revision: uint64(storedRevision), CanonicalDigest: domainDigest(digest), RoundingRule: fx.RoundingRule{Scale: *roundingScale, Mode: mode}, Tolerance: time.Duration(*toleranceNanos), FallbackSourceOrder: append([]string(nil), fallbackOrder...), TriangulationCurrencies: append([]string(nil), triangulation...), Effective: interval}
		if parentRevision != nil {
			out.ParentRevision = uint64(*parentRevision)
		}
		if parentDigest != nil {
			out.ParentDigest = domainDigest(*parentDigest)
		}
		if validateErr := out.Validate(); validateErr != nil {
			return invalid("stored conversion profile does not match its canonical digest or policy")
		}
		return nil
	})
	return out, err
}

func nullableRevision(revision uint64) any {
	if revision == 0 {
		return nil
	}
	return int64(revision)
}

func nullableDigest(digest string) any {
	if digest == "" {
		return nil
	}
	return storedDigest(digest)
}

func intervalStart(interval values.EffectiveInterval) time.Time {
	if start, ok := interval.StartInstant(); ok {
		return start.Time()
	}
	if start, ok := interval.StartDate(); ok {
		return time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
	}
	return time.Time{}
}

func intervalEnd(interval values.EffectiveInterval) any {
	if end, ok := interval.EndInstant(); ok {
		return end.Time()
	}
	if end, ok := interval.EndDate(); ok {
		return time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
	}
	return nil
}

func intervalBounds(interval values.EffectiveInterval) (time.Time, *time.Time, error) {
	if start, ok := interval.StartInstant(); ok {
		if end, hasEnd := interval.EndInstant(); hasEnd {
			value := end.Time()
			return start.Time(), &value, nil
		}
		return start.Time(), nil, nil
	}
	if start, ok := interval.StartDate(); ok {
		from := time.Date(int(start.Year()), start.Month(), int(start.Day()), 0, 0, 0, 0, time.UTC)
		if end, hasEnd := interval.EndDate(); hasEnd {
			value := time.Date(int(end.Year()), end.Month(), int(end.Day()), 0, 0, 0, 0, time.UTC)
			return from, &value, nil
		}
		return from, nil, nil
	}
	return time.Time{}, nil, errors.New("effective interval must have an instant or date start")
}

func intervalFromBounds(from, to *time.Time) (values.EffectiveInterval, error) {
	if from == nil {
		return values.EffectiveInterval{}, errors.New("effective_from is null")
	}
	start := values.NewInstant(from.UTC())
	if to == nil {
		return values.NewOpenInstantInterval(start)
	}
	return values.NewInstantInterval(start, values.NewInstant(to.UTC()))
}
