package i18n

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidRevision     = errors.New("i18n: invalid catalog revision")
	ErrRevisionExists      = errors.New("i18n: catalog revision exists")
	ErrRevisionConflict    = errors.New("i18n: revision conflict")
	ErrLegalReviewRequired = errors.New("i18n: legal review required")
	ErrMeaningChanged      = errors.New("i18n: translation key meaning changed")
)

// Translation is one governed message in a catalog revision.
type Translation struct {
	Key, Text, MeaningID, Source, Classification, Reviewer string
	EffectiveFrom, EffectiveUntil                          time.Time
	Legal                                                  bool
	LegalReviewStatus                                      string
}

type CatalogRevision struct {
	ID, Locale, PreviousRevision string
	Fallbacks                    map[string][]string
	Translations                 []Translation
	CreatedAt                    time.Time
}

// TranslationCatalog stores append-only revisions and publishes only valid
// revisions. Reads return copies, so callers cannot mutate stored history.
type TranslationCatalog struct {
	mu        sync.RWMutex
	revisions map[string]CatalogRevision
	active    map[string]string
	meanings  map[string]string
}

func NewTranslationCatalog() *TranslationCatalog {
	return &TranslationCatalog{revisions: map[string]CatalogRevision{}, active: map[string]string{}, meanings: map[string]string{}}
}

// NewCatalog is a concise constructor alias.
func NewCatalog() *TranslationCatalog { return NewTranslationCatalog() }

func (c *TranslationCatalog) PublishRevision(rev CatalogRevision) error {
	if c == nil {
		return fmt.Errorf("%w: nil catalog", ErrInvalidRevision)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := validateRevision(rev); err != nil {
		return err
	}
	if _, exists := c.revisions[rev.ID]; exists {
		return ErrRevisionExists
	}
	locale, err := parseLocale(rev.Locale)
	if err != nil {
		return err
	}
	if prior, ok := c.active[locale.String()]; ok && rev.PreviousRevision != prior {
		return fmt.Errorf("%w: previous revision must be %s", ErrRevisionConflict, prior)
	}
	for _, t := range rev.Translations {
		identity := t.MeaningID
		if identity == "" {
			identity = t.Source
		}
		key := locale.String() + "\x00" + t.Key
		if old, ok := c.meanings[key]; ok && old != identity {
			return fmt.Errorf("%w: %s", ErrMeaningChanged, t.Key)
		}
	}
	stored := cloneRevision(rev)
	stored.Locale = locale.String()
	c.revisions[rev.ID] = stored
	c.active[stored.Locale] = rev.ID
	for _, t := range rev.Translations {
		identity := t.MeaningID
		if identity == "" {
			identity = t.Source
		}
		c.meanings[stored.Locale+"\x00"+t.Key] = identity
	}
	return nil
}

// Publish appends and activates a validated revision.
func (c *TranslationCatalog) Publish(rev CatalogRevision) error { return c.PublishRevision(rev) }

func validateRevision(rev CatalogRevision) error {
	if rev.ID == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidRevision)
	}
	if rev.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidRevision)
	}
	if rev.Locale == "" {
		return fmt.Errorf("%w: locale is required", ErrInvalidRevision)
	}
	edges := map[string][]string{}
	for raw, candidates := range rev.Fallbacks {
		from, err := parseLocale(raw)
		if err != nil {
			return err
		}
		fromName := from.String()
		for _, rawCandidate := range candidates {
			to, err := parseLocale(rawCandidate)
			if err != nil {
				return err
			}
			edges[fromName] = append(edges[fromName], to.String())
		}
	}
	if hasCycle(edges) {
		return ErrFallbackCycle
	}
	seen := map[string]bool{}
	for _, t := range rev.Translations {
		if t.Key == "" || t.Text == "" || t.Source == "" || t.Classification == "" {
			return fmt.Errorf("%w: key, text, source and classification are required", ErrInvalidRevision)
		}
		if seen[t.Key] {
			return fmt.Errorf("%w: duplicate key %s", ErrInvalidRevision, t.Key)
		}
		seen[t.Key] = true
		if t.EffectiveFrom.IsZero() || (!t.EffectiveUntil.IsZero() && !t.EffectiveUntil.After(t.EffectiveFrom)) {
			return fmt.Errorf("%w: invalid effective interval for %s", ErrInvalidRevision, t.Key)
		}
		isLegal := t.Legal || strings.EqualFold(t.Classification, "LEGAL") || strings.EqualFold(t.Classification, "LEGAL_TEXT")
		if isLegal && (t.Reviewer == "" || t.LegalReviewStatus != "APPROVED") {
			return fmt.Errorf("%w: %s", ErrLegalReviewRequired, t.Key)
		}
	}
	return nil
}

func cloneRevision(in CatalogRevision) CatalogRevision {
	out := in
	out.Fallbacks = map[string][]string{}
	for k, v := range in.Fallbacks {
		out.Fallbacks[k] = append([]string(nil), v...)
	}
	out.Translations = append([]Translation(nil), in.Translations...)
	return out
}

func (c *TranslationCatalog) Revision(id string) (CatalogRevision, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rev, ok := c.revisions[id]
	if !ok {
		return CatalogRevision{}, false
	}
	return cloneRevision(rev), true
}
func (c *TranslationCatalog) ActiveRevision(locale string) (CatalogRevision, bool) {
	tag, err := parseLocale(locale)
	if err != nil {
		return CatalogRevision{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.active[tag.String()]
	if !ok {
		return CatalogRevision{}, false
	}
	return cloneRevision(c.revisions[id]), true
}
