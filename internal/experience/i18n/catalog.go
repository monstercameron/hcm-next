package i18n

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

type Translation struct {
	Key, Text, MeaningID, Source, Classification, Reviewer string
	EffectiveFrom, EffectiveUntil                          time.Time
	Legal                                                  bool
	LegalReviewStatus                                      string
}
type CatalogRevision struct {
	ID, Locale, PreviousRevision, Version    string
	Fallbacks                                map[string][]string
	Translations                             []Translation
	CreatedAt, EffectiveFrom, EffectiveUntil time.Time
	CanonicalDigest                          string
	Digest                                   string
}

type TranslationCatalog struct {
	mu        sync.RWMutex
	revisions map[string]CatalogRevision
	active    map[string]string
	meanings  map[string]string
}

func NewTranslationCatalog() *TranslationCatalog {
	return &TranslationCatalog{revisions: map[string]CatalogRevision{}, active: map[string]string{}, meanings: map[string]string{}}
}
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
	if _, ok := c.revisions[rev.ID]; ok {
		return ErrRevisionExists
	}
	tag, _ := parseLocale(rev.Locale)
	locale := tag.String()
	if prior, ok := c.active[locale]; ok && rev.PreviousRevision != prior {
		return fmt.Errorf("%w: previous revision must be %s", ErrRevisionConflict, prior)
	}
	for _, t := range rev.Translations {
		id := t.MeaningID
		if id == "" {
			id = t.Source
		}
		if old, ok := c.meanings[locale+"\x00"+t.Key]; ok && old != id {
			return fmt.Errorf("%w: %s", ErrMeaningChanged, t.Key)
		}
	}
	stored := cloneRevision(rev)
	stored.Locale = locale
	if stored.CanonicalDigest != "" && stored.CanonicalDigest != DigestRevision(stored) {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidRevision)
	}
	if stored.Digest != "" && stored.Digest != DigestRevision(stored) {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidRevision)
	}
	if stored.CanonicalDigest == "" {
		stored.CanonicalDigest = DigestRevision(stored)
	}
	stored.Digest = stored.CanonicalDigest
	c.revisions[stored.ID] = stored
	c.active[locale] = stored.ID
	for _, t := range stored.Translations {
		id := t.MeaningID
		if id == "" {
			id = t.Source
		}
		c.meanings[locale+"\x00"+t.Key] = id
	}
	return nil
}
func (c *TranslationCatalog) Publish(rev CatalogRevision) error { return c.PublishRevision(rev) }
func validateRevision(r CatalogRevision) error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidRevision)
	}
	if r.CreatedAt.IsZero() {
		return fmt.Errorf("%w: created_at is required", ErrInvalidRevision)
	}
	if _, e := parseLocale(r.Locale); e != nil {
		return e
	}
	if !r.EffectiveUntil.IsZero() && !r.EffectiveUntil.After(r.EffectiveFrom) {
		return fmt.Errorf("%w: invalid effective interval", ErrInvalidRevision)
	}
	edges := map[string][]string{}
	for from, tos := range r.Fallbacks {
		f, e := parseLocale(from)
		if e != nil {
			return e
		}
		for _, to := range tos {
			t, e := parseLocale(to)
			if e != nil {
				return e
			}
			edges[f.String()] = append(edges[f.String()], t.String())
		}
	}
	if hasCycle(edges) {
		return ErrFallbackCycle
	}
	seen := map[string]bool{}
	for _, t := range r.Translations {
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
		legal := t.Legal || strings.EqualFold(t.Classification, "LEGAL") || strings.EqualFold(t.Classification, "LEGAL_TEXT")
		if legal && (t.Reviewer == "" || !strings.EqualFold(t.LegalReviewStatus, "APPROVED")) {
			return fmt.Errorf("%w: %s", ErrLegalReviewRequired, t.Key)
		}
	}
	return nil
}

// Validate checks a revision without publishing it.
func (r CatalogRevision) Validate() error { return validateRevision(r) }

// Effective reports whether the revision applies at at.
func (r CatalogRevision) Effective(at time.Time) bool {
	return (r.EffectiveFrom.IsZero() || !at.Before(r.EffectiveFrom)) &&
		(r.EffectiveUntil.IsZero() || at.Before(r.EffectiveUntil))
}

// EffectiveTranslation returns a detached translation for key at at.
func (r CatalogRevision) EffectiveTranslation(key string, at time.Time) (Translation, bool) {
	for _, t := range r.Translations {
		if t.Key == key && (t.EffectiveFrom.IsZero() || !at.Before(t.EffectiveFrom)) &&
			(t.EffectiveUntil.IsZero() || at.Before(t.EffectiveUntil)) {
			return t, true
		}
	}
	return Translation{}, false
}

// DigestRevision returns a stable SHA-256 hex digest over the revision's
// semantic fields. The stored digest itself is excluded from the input.
func DigestRevision(r CatalogRevision) string {
	translations := append([]Translation(nil), r.Translations...)
	sort.SliceStable(translations, func(i, j int) bool { return translations[i].Key < translations[j].Key })
	v := digestView{ID: r.ID, Locale: r.Locale, PreviousRevision: r.PreviousRevision, Version: r.Version, Fallbacks: r.Fallbacks, Translations: translations, CreatedAt: r.CreatedAt, EffectiveFrom: r.EffectiveFrom, EffectiveUntil: r.EffectiveUntil}
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type digestView struct {
	ID, Locale, PreviousRevision, Version    string
	Fallbacks                                map[string][]string
	Translations                             []Translation
	CreatedAt, EffectiveFrom, EffectiveUntil time.Time
}

func cloneRevision(r CatalogRevision) CatalogRevision {
	r.Fallbacks = cloneEdges(r.Fallbacks)
	r.Translations = append([]Translation(nil), r.Translations...)
	return r
}
func (c *TranslationCatalog) Revision(id string) (CatalogRevision, bool) {
	if c == nil {
		return CatalogRevision{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.revisions[id]
	return cloneRevision(r), ok
}
func (c *TranslationCatalog) ActiveRevision(locale string) (CatalogRevision, bool) {
	t, e := parseLocale(locale)
	if e != nil || c == nil {
		return CatalogRevision{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	id, ok := c.active[t.String()]
	if !ok {
		return CatalogRevision{}, false
	}
	return cloneRevision(c.revisions[id]), true
}
func (r CatalogRevision) VerifyDigest() bool {
	d := r.CanonicalDigest
	if d == "" {
		d = r.Digest
	}
	return d != "" && d == DigestRevision(r)
}
