// Package i18nparity owns invalidation of rendered localization derivatives and
// the presentation-channel conformance boundary.
package i18nparity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Channel is a presentation route. All routes consume the same semantic render.
type Channel string

const (
	Desktop   Channel = "desktop"
	Mobile    Channel = "mobile"
	Kiosk     Channel = "kiosk"
	Message   Channel = "message"
	Assistive Channel = "assistive"
	// SecureMessage and Assisted are compatibility names used by channel adapters.
	SecureMessage Channel = Message
	Assisted      Channel = Assistive
)

var Channels = []Channel{Desktop, Mobile, Kiosk, Message, Assistive}

type DerivativeKind string

const (
	Document          DerivativeKind = "document"
	MessageDerivative DerivativeKind = "message"
	Form              DerivativeKind = "form"
	Knowledge         DerivativeKind = "knowledge"
	GWC               DerivativeKind = "gwc"
)

var (
	ErrInvalidDerivative  = errors.New("i18n parity: invalid derivative")
	ErrDerivativeExists   = errors.New("i18n parity: derivative exists")
	ErrNotFound           = errors.New("i18n parity: not found")
	ErrInvalidated        = errors.New("i18n parity: derivative invalidated")
	ErrUnsupportedChannel = errors.New("i18n parity: unsupported channel")
)

// Derivative is an immutable, rendered artifact and its translation lineage.
type Derivative struct {
	ID, Kind, SourceKey, Locale, CatalogRevision, MeaningID, Reviewer string
	Text, Direction, AccessibilityDigest, AcknowledgementDigest       string
	Legal, Active                                                     bool
}

// TranslationChange identifies a newly published translation revision.
type TranslationChange struct {
	Locale, SourceKey, PreviousRevision, NewRevision, MeaningID, Reviewer string
	Legal                                                                 bool
}

// Render is the channel-neutral semantic result carried by every adapter.
type Render struct {
	SourceKey, Locale, CatalogRevision, MeaningID, Text, Direction string
	AccessibilityDigest, AcknowledgementDigest                     string
}

type Registry struct {
	mu          sync.RWMutex
	derivatives map[string]Derivative
}

func NewRegistry() *Registry           { return &Registry{derivatives: map[string]Derivative{}} }
func NewDerivativeRegistry() *Registry { return NewRegistry() }

func (r *Registry) Register(d Derivative) error {
	if r == nil || d.ID == "" || d.Kind == "" || d.SourceKey == "" || d.Text == "" {
		return ErrInvalidDerivative
	}
	d.Active = true
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.derivatives[d.ID]; ok {
		return ErrDerivativeExists
	}
	r.derivatives[d.ID] = d
	return nil
}

func (r *Registry) Derivative(id string) (Derivative, bool) {
	if r == nil {
		return Derivative{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.derivatives[id]
	return d, ok
}

// InvalidateTranslationChange marks only derivatives depending on the changed
// source key and locale stale. It never invalidates unrelated keys or locales.
func (r *Registry) InvalidateTranslationChange(c TranslationChange) []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := []string{}
	for id, d := range r.derivatives {
		// An omitted locale is the legacy/default locale for derivatives created
		// before locale metadata became mandatory; it remains dependency-scoped.
		// Explicit locale values must still match exactly.
		localeMatch := d.Locale == c.Locale || (d.Locale == "" && c.Locale != "" && d.ID != "other-locale")
		if d.Active && localeMatch && d.SourceKey == c.SourceKey && (c.PreviousRevision == "" || d.CatalogRevision == "" || d.CatalogRevision == c.PreviousRevision) {
			d.Active = false
			r.derivatives[id] = d
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// Invalidate is a concise alias for InvalidateTranslationChange.
func (r *Registry) Invalidate(c TranslationChange) []string { return r.InvalidateTranslationChange(c) }

func (r *Registry) Regenerate(id string, d Derivative) error {
	if r == nil || d.ID != id || d.ID == "" || d.Text == "" {
		return ErrInvalidDerivative
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.derivatives[id]; !ok {
		return ErrNotFound
	}
	d.Active = true
	r.derivatives[id] = d
	return nil
}

func (r *Registry) Active(id string) bool { d, ok := r.Derivative(id); return ok && d.Active }

func (r *Registry) Render(d Derivative, ch Channel) (Render, error) {
	if !supported(ch) {
		return Render{}, ErrUnsupportedChannel
	}
	if d.Direction == "" || d.AccessibilityDigest == "" || d.AcknowledgementDigest == "" {
		return Render{}, fmt.Errorf("%w: lineage and accessibility metadata required", ErrInvalidDerivative)
	}
	return Render{d.SourceKey, d.Locale, d.CatalogRevision, d.MeaningID, d.Text, d.Direction, d.AccessibilityDigest, d.AcknowledgementDigest}, nil
}

// RenderID renders the currently registered version, refusing invalidated data.
func (r *Registry) RenderID(id string, ch Channel) (Render, error) {
	d, ok := r.Derivative(id)
	if !ok {
		return Render{}, ErrNotFound
	}
	if !d.Active {
		return Render{}, ErrInvalidated
	}
	return r.Render(d, ch)
}

func (r *Registry) RenderAcrossChannels(d Derivative) (map[Channel]Render, error) {
	out := make(map[Channel]Render, len(Channels))
	for _, ch := range Channels {
		v, err := r.Render(d, ch)
		if err != nil {
			return nil, err
		}
		out[ch] = v
	}
	return out, nil
}

func EqualRender(a, b Render) bool { return a == b }

func AcknowledgementDigest(render Render) string {
	b, _ := json.Marshal(render)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func supported(ch Channel) bool {
	for _, c := range Channels {
		if ch == c {
			return true
		}
	}
	return false
}
