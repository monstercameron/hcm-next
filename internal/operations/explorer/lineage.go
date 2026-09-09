package explorer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/lineage"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// LineageResultView is one event's correction/supersession ancestry and
// descendants (internal/data/ledger/lineage.Ancestors, .Descendants).
// lineage.Node carries no payload - only identity, assertion class and
// timing - so nothing beyond subject-level disclosure is gated here.
type LineageResultView struct {
	Ref         ledger.EventRef
	Ancestors   []lineage.Node
	Descendants []lineage.Node
	// Withheld is true when the decision's subject was not disclosable:
	// Ancestors and Descendants are then both nil rather than an empty walk
	// result, so "no lineage" and "not authorized to see lineage" can never
	// be confused.
	Withheld bool
	Digest   string
}

// Lineage walks ref's correction/supersession ancestry and descendants. A
// decision whose subject is not disclosable refuses to walk at all
// (Withheld=true, both lists nil) rather than silently returning an empty
// result a caller could mistake for "this event has no lineage." A nil dec
// is unrestricted.
func Lineage(ctx context.Context, q lineage.Querier, tenant uuid.UUID, ref ledger.EventRef, dec *authz.Decision) (LineageResultView, error) {
	if dec != nil && !dec.SubjectDisclosable {
		v := LineageResultView{Ref: ref, Withheld: true}
		v.Digest = digestLineage(v)
		return v, nil
	}

	ancestors, err := lineage.Ancestors(ctx, q, tenant, ref)
	if err != nil {
		return LineageResultView{}, fmt.Errorf("explorer: lineage ancestors of %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}
	descendants, err := lineage.Descendants(ctx, q, tenant, ref)
	if err != nil {
		return LineageResultView{}, fmt.Errorf("explorer: lineage descendants of %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}

	view := LineageResultView{Ref: ref, Ancestors: ancestors, Descendants: descendants}
	view.Digest = digestLineage(view)
	return view, nil
}

func digestLineageNode(b *viewdigest.Builder, prefix string, n lineage.Node) *viewdigest.Builder {
	b.String(prefix+".stream_key", n.Ref.StreamKey).
		Int(prefix+".sequence", n.Ref.Sequence).
		String(prefix+".event_id", n.EventID.String()).
		String(prefix+".assertion_class", string(n.AssertionClass))
	if n.Corrects != nil {
		b.Bool(prefix+".corrects.present", true).
			String(prefix+".corrects.stream_key", n.Corrects.StreamKey).
			Int(prefix+".corrects.sequence", n.Corrects.Sequence)
	} else {
		b.Bool(prefix+".corrects.present", false)
	}
	return b
}

func digestLineage(v LineageResultView) string {
	b := viewdigest.New().
		String("ref.stream_key", v.Ref.StreamKey).
		Int("ref.sequence", v.Ref.Sequence).
		Bool("withheld", v.Withheld).
		Int("ancestors", int64(len(v.Ancestors))).
		Int("descendants", int64(len(v.Descendants)))
	for i, n := range v.Ancestors {
		digestLineageNode(b, fmt.Sprintf("ancestor[%d]", i), n)
	}
	for i, n := range v.Descendants {
		digestLineageNode(b, fmt.Sprintf("descendant[%d]", i), n)
	}
	return b.Digest()
}
