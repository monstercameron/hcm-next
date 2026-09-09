package explorer

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// StreamListingView is one stream's events, redacted per-event by the
// caller's decision, plus a content digest.
type StreamListingView struct {
	Tenant    uuid.UUID
	StreamKey string
	Events    []EventView
	Digest    string
}

// StreamListing lists every event recorded on one stream, ordered by
// sequence ascending (internal/data/ledger.Reader.ReadStream's own order),
// with each event redacted per dec. A nil dec is unrestricted.
func StreamListing(ctx context.Context, q ledger.Querier, tenant uuid.UUID, streamKey string, dec *authz.Decision) (StreamListingView, error) {
	records, err := ledger.NewReader().ReadStream(ctx, q, tenant, streamKey)
	if err != nil {
		return StreamListingView{}, fmt.Errorf("explorer: list stream %s: %w", streamKey, err)
	}

	view := StreamListingView{Tenant: tenant, StreamKey: streamKey}
	view.Events = make([]EventView, 0, len(records))
	for _, rec := range records {
		view.Events = append(view.Events, redactEvent(rec, dec))
	}
	view.Digest = digestStreamListing(view)
	return view, nil
}

// EventDetailView is one exact event plus its stream's hash-chain
// verification, redacted per dec.
type EventDetailView struct {
	Event  EventView
	Chain  ChainView
	Digest string
}

// EventDetail reads one exact event by (tenant, stream, sequence) and
// verifies its stream's hash chain, redacting the event per dec. Chain
// verification metadata (whether the chain verifies, and where it broke if
// not) is never gated: an operator diagnosing a reported tamper must be
// able to see the exact sequence a break was reported at even when the
// event's own payload is denied.
func EventDetail(ctx context.Context, q ledger.Querier, digester *hashchain.Digester, tenant uuid.UUID, ref ledger.EventRef, dec *authz.Decision) (EventDetailView, error) {
	rec, err := ledger.NewReader().ReadEvent(ctx, q, tenant, ref.StreamKey, ref.Sequence)
	if err != nil {
		return EventDetailView{}, fmt.Errorf("explorer: read event %s@%d: %w", ref.StreamKey, ref.Sequence, err)
	}

	chain, err := VerifyChain(ctx, q, digester, tenant, ref.StreamKey)
	if err != nil {
		return EventDetailView{}, err
	}

	view := EventDetailView{Event: redactEvent(rec, dec), Chain: chain}
	view.Digest = digestEventDetail(view)
	return view, nil
}

func digestEvent(b *viewdigest.Builder, prefix string, e EventView) *viewdigest.Builder {
	return b.
		String(prefix+".stream_key", e.StreamKey).
		Int(prefix+".sequence", e.Sequence).
		String(prefix+".event_id", e.EventID.String()).
		String(prefix+".assertion_class", e.AssertionClass).
		String(prefix+".schema_ref", e.SchemaRef).
		String(prefix+".digest", e.Digest).
		String(prefix+".digest_algorithm", e.DigestAlgorithm).
		String(prefix+".correlation_id", e.CorrelationID.String()).
		String(prefix+".idempotency_key", e.IdempotencyKey).
		Bool(prefix+".subject_withheld", e.SubjectWithheld).
		Bool(prefix+".payload_withheld", e.PayloadWithheld).
		String(prefix+".authority", e.Authority).
		String(prefix+".source_ref", e.SourceRef).
		String(prefix+".artifact_ref", e.ArtifactRef).
		String(prefix+".payload", string(e.Payload))
}

func digestStreamListing(v StreamListingView) string {
	b := viewdigest.New().
		String("tenant", v.Tenant.String()).
		String("stream_key", v.StreamKey).
		Int("events", int64(len(v.Events)))
	for i, e := range v.Events {
		digestEvent(b, fmt.Sprintf("event[%d]", i), e)
	}
	return b.Digest()
}

func digestChain(b *viewdigest.Builder, prefix string, c ChainView) *viewdigest.Builder {
	b.String(prefix+".stream_key", c.StreamKey).
		Bool(prefix+".verified", c.Verified).
		Bool(prefix+".empty", c.Empty).
		String(prefix+".head.chain_hash", c.Head.ChainHash).
		Int(prefix+".head.sequence", c.Head.Sequence)
	if c.Broken != nil {
		b.Bool(prefix+".broken.present", true).
			Int(prefix+".broken.sequence", c.Broken.Sequence).
			String(prefix+".broken.reason", c.Broken.Reason)
	} else {
		b.Bool(prefix+".broken.present", false)
	}
	return b
}

func digestEventDetail(v EventDetailView) string {
	b := viewdigest.New()
	digestEvent(b, "event", v.Event)
	digestChain(b, "chain", v.Chain)
	return b.Digest()
}

// SortEventsBySequence sorts a slice of EventView by Sequence ascending in
// place. StreamListing already returns events in this order; tests and
// callers that reassemble a listing from another source use this helper to
// restore the canonical order before comparing.
func SortEventsBySequence(events []EventView) {
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
}
