package fx

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Store is the tenant-aware persistence port for the governed FX revision
// families. Implementations retain immutable revisions and never update a
// previously accepted identity. Tenant is a string here so the domain stays
// independent of the database's UUID type.
type Store interface {
	SaveRateSource(context.Context, string, RateSourceRevision) error
	LoadRateSource(context.Context, string, string, uint64) (RateSourceRevision, error)
	SaveQuote(context.Context, string, FXQuoteRevision) error
	LoadQuote(context.Context, string, string) (FXQuoteRevision, error)
	SaveConversionProfile(context.Context, string, ConversionProfileRevision) error
	LoadConversionProfile(context.Context, string, string, uint64) (ConversionProfileRevision, error)
}

// StoreCode is the stable machine-readable classification of a persistence
// result. Data adapters return these same codes rather than exposing driver
// constraint text to callers.
type StoreCode string

const (
	StoreCodeInvalid           StoreCode = "INVALID"
	StoreCodeNotFound          StoreCode = "NOT_FOUND"
	StoreCodeDuplicateRevision StoreCode = "DUPLICATE_REVISION"
	StoreCodeStaleCAS          StoreCode = "STALE_CAS"
	StoreCodeReferenceNotFound StoreCode = "REFERENCE_NOT_FOUND"
)

// StoreError is a typed refusal from an FX store. Expected and Actual are
// populated for stale compare-and-set failures.
type StoreError struct {
	Code             StoreCode
	Detail           string
	Expected, Actual uint64
}

func (e *StoreError) Error() string {
	if e == nil {
		return "fx: <nil store error>"
	}
	if e.Detail == "" {
		return fmt.Sprintf("fx: %s", e.Code)
	}
	return fmt.Sprintf("fx: %s: %s", e.Code, e.Detail)
}

func (e *StoreError) Is(target error) bool {
	t, ok := target.(*StoreError)
	return ok && e != nil && t != nil && e.Code == t.Code
}

var (
	ErrStoreInvalid           = &StoreError{Code: StoreCodeInvalid}
	ErrStoreNotFound          = &StoreError{Code: StoreCodeNotFound}
	ErrStoreDuplicateRevision = &StoreError{Code: StoreCodeDuplicateRevision}
	ErrStoreStaleCAS          = &StoreError{Code: StoreCodeStaleCAS}
	ErrStoreReferenceNotFound = &StoreError{Code: StoreCodeReferenceNotFound}
)

// CodeOf returns the stable store code carried by err, if present.
func CodeOf(err error) StoreCode {
	var typed *StoreError
	if errors.As(err, &typed) && typed != nil {
		return typed.Code
	}
	return ""
}

func storeError(code StoreCode, detail string) *StoreError {
	return &StoreError{Code: code, Detail: detail}
}

// MemoryStore is the kernel-pure reference implementation of Store. It is
// useful for composition that does not need PostgreSQL and mirrors the
// durable adapter's duplicate, lineage, reference and tenant rules.
type MemoryStore struct {
	mu       sync.RWMutex
	sources  map[string]RateSourceRevision
	quotes   map[string]FXQuoteRevision
	profiles map[string]ConversionProfileRevision
}

// NewMemoryStore returns an empty FX store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sources:  make(map[string]RateSourceRevision),
		quotes:   make(map[string]FXQuoteRevision),
		profiles: make(map[string]ConversionProfileRevision),
	}
}

var _ Store = (*MemoryStore)(nil)

func validateStoreContext(ctx context.Context, tenant string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(tenant) == "" {
		return storeError(StoreCodeInvalid, "tenant id is required")
	}
	return nil
}

func sourceKey(tenant, id string, revision uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", tenant, id, revision)
}

func quoteKey(tenant, id string) string { return tenant + "\x00" + id }

func profileKey(tenant, id string, revision uint64) string {
	return fmt.Sprintf("%s\x00%s\x00%d", tenant, id, revision)
}

func cloneSource(in RateSourceRevision) RateSourceRevision {
	in.Pairs = append([]CurrencyPair(nil), in.Pairs...)
	return in
}

func cloneProfile(in ConversionProfileRevision) ConversionProfileRevision {
	in.FallbackSourceOrder = append([]string(nil), in.FallbackSourceOrder...)
	return in
}

func (s *MemoryStore) SaveRateSource(ctx context.Context, tenant string, source RateSourceRevision) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if s == nil {
		return storeError(StoreCodeInvalid, "nil memory store")
	}
	sealed := cloneSource(source)
	if source.CanonicalDigest == "" {
		var err error
		sealed, err = NewRateSourceRevision(source)
		if err != nil {
			return storeError(StoreCodeInvalid, err.Error())
		}
	} else if err := sealed.Validate(); err != nil {
		return storeError(StoreCodeInvalid, err.Error())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	key := sourceKey(tenant, sealed.SourceID, sealed.Revision)
	if _, exists := s.sources[key]; exists {
		return storeError(StoreCodeDuplicateRevision, "rate source revision already exists")
	}
	if err := requireSourceHead(s.sources, tenant, sealed); err != nil {
		return err
	}
	s.sources[key] = cloneSource(sealed)
	return nil
}

func requireSourceHead(rows map[string]RateSourceRevision, tenant string, source RateSourceRevision) error {
	var latest RateSourceRevision
	var found bool
	for key, candidate := range rows {
		if !strings.HasPrefix(key, tenant+"\x00"+source.SourceID+"\x00") {
			continue
		}
		if !found || candidate.Revision > latest.Revision {
			latest, found = candidate, true
		}
	}
	if source.Revision == 1 {
		if found {
			return storeError(StoreCodeStaleCAS, "rate source already has a current revision")
		}
		return nil
	}
	if !found || latest.Revision != source.ParentRevision || latest.CanonicalDigest != source.ParentDigest {
		actual := uint64(0)
		if found {
			actual = latest.Revision
		}
		err := storeError(StoreCodeStaleCAS, "rate source predecessor is not the current revision")
		err.Expected, err.Actual = source.ParentRevision, actual
		return err
	}
	return nil
}

func (s *MemoryStore) LoadRateSource(ctx context.Context, tenant, sourceID string, revision uint64) (RateSourceRevision, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return RateSourceRevision{}, err
	}
	if sourceID == "" || revision == 0 {
		return RateSourceRevision{}, storeError(StoreCodeInvalid, "source id and positive revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	source, ok := s.sources[sourceKey(tenant, sourceID, revision)]
	if !ok {
		return RateSourceRevision{}, storeError(StoreCodeNotFound, "rate source revision does not exist")
	}
	return cloneSource(source), nil
}

func (s *MemoryStore) SaveQuote(ctx context.Context, tenant string, quote FXQuoteRevision) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if s == nil {
		return storeError(StoreCodeInvalid, "nil memory store")
	}
	sealed := quote
	if quote.CanonicalDigest == "" {
		var err error
		sealed, err = NewFXQuoteRevision(quote)
		if err != nil {
			return storeError(StoreCodeInvalid, err.Error())
		}
	} else if err := sealed.Validate(); err != nil {
		return storeError(StoreCodeInvalid, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sources[sourceKey(tenant, sealed.SourceID, sealed.SourceRevision)]; !exists {
		return storeError(StoreCodeReferenceNotFound, "quote source revision does not exist")
	}
	key := quoteKey(tenant, sealed.QuoteID)
	if _, exists := s.quotes[key]; exists {
		return storeError(StoreCodeDuplicateRevision, "quote revision already exists")
	}
	s.quotes[key] = sealed
	return nil
}

func (s *MemoryStore) LoadQuote(ctx context.Context, tenant, quoteID string) (FXQuoteRevision, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return FXQuoteRevision{}, err
	}
	if quoteID == "" {
		return FXQuoteRevision{}, storeError(StoreCodeInvalid, "quote id is required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	quote, ok := s.quotes[quoteKey(tenant, quoteID)]
	if !ok {
		return FXQuoteRevision{}, storeError(StoreCodeNotFound, "quote revision does not exist")
	}
	return quote, nil
}

func (s *MemoryStore) SaveConversionProfile(ctx context.Context, tenant string, profile ConversionProfileRevision) error {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return err
	}
	if s == nil {
		return storeError(StoreCodeInvalid, "nil memory store")
	}
	sealed := cloneProfile(profile)
	if profile.CanonicalDigest == "" {
		var err error
		sealed, err = NewConversionProfileRevision(profile)
		if err != nil {
			return storeError(StoreCodeInvalid, err.Error())
		}
	} else if err := sealed.Validate(); err != nil {
		return storeError(StoreCodeInvalid, err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := profileKey(tenant, sealed.ProfileID, sealed.Revision)
	if _, exists := s.profiles[key]; exists {
		return storeError(StoreCodeDuplicateRevision, "conversion profile revision already exists")
	}
	var latest ConversionProfileRevision
	var found bool
	for rowKey, candidate := range s.profiles {
		if !strings.HasPrefix(rowKey, tenant+"\x00"+sealed.ProfileID+"\x00") {
			continue
		}
		if !found || candidate.Revision > latest.Revision {
			latest, found = candidate, true
		}
	}
	if sealed.Revision == 1 {
		if found {
			return storeError(StoreCodeStaleCAS, "conversion profile already has a current revision")
		}
	} else if !found || latest.Revision != sealed.ParentRevision || latest.CanonicalDigest != sealed.ParentDigest {
		err := storeError(StoreCodeStaleCAS, "conversion profile predecessor is not the current revision")
		err.Expected = sealed.ParentRevision
		if found {
			err.Actual = latest.Revision
		}
		return err
	}
	s.profiles[key] = cloneProfile(sealed)
	return nil
}

func (s *MemoryStore) LoadConversionProfile(ctx context.Context, tenant, profileID string, revision uint64) (ConversionProfileRevision, error) {
	if err := validateStoreContext(ctx, tenant); err != nil {
		return ConversionProfileRevision{}, err
	}
	if profileID == "" || revision == 0 {
		return ConversionProfileRevision{}, storeError(StoreCodeInvalid, "profile id and positive revision are required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, ok := s.profiles[profileKey(tenant, profileID, revision)]
	if !ok {
		return ConversionProfileRevision{}, storeError(StoreCodeNotFound, "conversion profile revision does not exist")
	}
	return cloneProfile(profile), nil
}
