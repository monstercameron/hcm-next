package fx

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrQuoteSuccessorRequired is returned when a correction attempts to alter
// an existing observation instead of appending a linked revision.
var ErrQuoteSuccessorRequired = fmt.Errorf("%w: quote correction must append a successor", ErrInvalidQuote)
var ErrInvalidImpactGraph = fmt.Errorf("%w: invalid impact dependency", ErrInvalidQuote)

// CorrectQuote validates and seals an append-only correction. The caller
// chooses the new identity; the old quote is never modified.
func CorrectQuote(previous, successor FXQuoteRevision) (FXQuoteRevision, error) {
	if err := previous.Validate(); err != nil {
		return FXQuoteRevision{}, err
	}
	if successor.Revision == 0 {
		successor.Revision = previous.Revision + 1
	}
	if successor.QuoteID == previous.QuoteID || successor.ParentQuoteID != previous.QuoteID || successor.ParentDigest != previous.CanonicalDigest || successor.Revision != previous.Revision+1 {
		return FXQuoteRevision{}, ErrQuoteSuccessorRequired
	}
	if successor.SourceID != previous.SourceID || successor.SourceRevision != previous.SourceRevision || successor.BaseCurrency != previous.BaseCurrency || successor.QuoteCurrency != previous.QuoteCurrency {
		return FXQuoteRevision{}, fmt.Errorf("%w: correction cannot change source or currency pair", ErrInvalidQuote)
	}
	if successor.AsOf.Compare(previous.AsOf) != 0 || !previous.KnownAt.Before(successor.KnownAt) {
		return FXQuoteRevision{}, fmt.Errorf("%w: correction must retain as-of and become known later", ErrInvalidQuote)
	}
	return NewFXQuoteRevision(successor)
}

// AppendQuoteSuccessor is the descriptive alias used by reconciliation code.
func AppendQuoteSuccessor(previous, successor FXQuoteRevision) (FXQuoteRevision, error) {
	return CorrectQuote(previous, successor)
}

// DerivedDependency pins a derived result or intent to the exact quote and
// currency scope used to produce it. ResultID and IntentID are opaque refs.
type DerivedDependency struct {
	ResultID      string
	IntentID      string
	QuoteID       string
	QuoteDigest   string
	BaseCurrency  string
	QuoteCurrency string
	AsOf          values.Instant
	KnownAt       values.Instant
	Approved      bool
}

// ImpactEntry is one affected consumer. Approved entries require explicit
// replanning; they are not silently recalculated or mutated.
type ImpactEntry struct {
	ResultID       string
	IntentID       string
	QuoteID        string
	BaseCurrency   string
	QuoteCurrency  string
	ReplanRequired bool
}

type ImpactGraph struct {
	PreviousQuoteDigest  string
	SuccessorQuoteDigest string
	Entries              []ImpactEntry
	CanonicalDigest      string
}

func (g ImpactGraph) Canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.fx.ImpactGraph", schemaVersion).
		String("previous_quote_digest", g.PreviousQuoteDigest).String("successor_quote_digest", g.SuccessorQuoteDigest).
		Count("entries", len(g.Entries))
	for _, e := range g.Entries {
		w.String("result_id", e.ResultID).String("intent_id", e.IntentID).String("quote_id", e.QuoteID).
			String("base_currency", e.BaseCurrency).String("quote_currency", e.QuoteCurrency).Bool("replan_required", e.ReplanRequired)
	}
	b, _ := w.Bytes()
	return b
}

// BuildImpactGraph returns only consumers pinned to the corrected quote,
// with exact pair and observation scope. Unknown/ambiguous scope is excluded.
func BuildImpactGraph(previous, successor FXQuoteRevision, deps []DerivedDependency) (ImpactGraph, error) {
	sealed, err := CorrectQuote(previous, successor)
	if err != nil {
		return ImpactGraph{}, err
	}
	entries := make([]ImpactEntry, 0)
	seen := make(map[string]struct{}, len(deps))
	for _, d := range deps {
		if d.QuoteID != previous.QuoteID || d.QuoteDigest != previous.CanonicalDigest {
			continue
		}
		if !d.AsOf.IsSet() || !d.KnownAt.IsSet() || d.AsOf.Compare(previous.AsOf) != 0 || d.KnownAt.Before(previous.KnownAt) || d.BaseCurrency != previous.BaseCurrency || d.QuoteCurrency != previous.QuoteCurrency {
			return ImpactGraph{}, ErrInvalidImpactGraph
		}
		if strings.TrimSpace(d.ResultID) == "" && strings.TrimSpace(d.IntentID) == "" {
			return ImpactGraph{}, ErrInvalidImpactGraph
		}
		key := d.ResultID + "\x00" + d.IntentID
		if _, ok := seen[key]; ok {
			return ImpactGraph{}, ErrInvalidImpactGraph
		}
		seen[key] = struct{}{}
		entries = append(entries, ImpactEntry{ResultID: d.ResultID, IntentID: d.IntentID, QuoteID: d.QuoteID, BaseCurrency: d.BaseCurrency, QuoteCurrency: d.QuoteCurrency, ReplanRequired: d.Approved})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.Join([]string{entries[i].ResultID, entries[i].IntentID}, "\x00") < strings.Join([]string{entries[j].ResultID, entries[j].IntentID}, "\x00")
	})
	g := ImpactGraph{PreviousQuoteDigest: previous.CanonicalDigest, SuccessorQuoteDigest: sealed.CanonicalDigest, Entries: entries}
	g.CanonicalDigest = canonicalbytes.Digest(g.Canonical())
	return g, nil
}
