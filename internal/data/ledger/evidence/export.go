package evidence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/checkpoint"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
)

// Request is what a caller states about an export. Everything else - which
// streams are covered, where their chains stood, which epochs apply - is
// derived, because a caller that could supply them could hand an auditor a
// package describing a ledger that never existed.
type Request struct {
	Tenant uuid.UUID
	// From and To are the half-open recorded-time window to export.
	From time.Time
	To   time.Time
	// Schema binds the package to the physical schema the ledger was read
	// under, exactly as a checkpoint is bound (LEDGER-010).
	Schema SchemaRelease
}

// Exporter assembles evidence packages from a live ledger. It holds no
// state; the value exists so the method set can be swapped at a composition
// root and so [internal/ledger] has something to satisfy its port with.
type Exporter struct{}

// NewExporter returns an Exporter.
func NewExporter() *Exporter { return &Exporter{} }

// Export reads the covered slice of the ledger and builds the package.
//
// It is read-only: no statement it runs writes anything, so an export never
// competes with an appender for a lock and can be taken against a replica.
// It refuses rather than degrades - an unchained stream head, a window no
// signed epoch covers, or a row naming another tenant produces an error and
// no package at all.
func (e *Exporter) Export(ctx context.Context, q Querier, req Request) (Package, error) {
	content, err := e.Read(ctx, q, req)
	if err != nil {
		return Package{}, err
	}
	return Build(content)
}

// Read assembles the [Content] an export covers without encoding it. A
// caller that wants to inspect what would be exported - how many streams,
// how many events, which epochs - calls this and never builds the bytes.
func (e *Exporter) Read(ctx context.Context, q Querier, req Request) (Content, error) {
	if req.Tenant == uuid.Nil {
		return Content{}, ErrContentInvalid{Missing: []string{"tenant is required"}}
	}
	from, to := Truncate(req.From), Truncate(req.To)
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return Content{}, ErrContentInvalid{
			Tenant:  req.Tenant,
			Missing: []string{"the exported recorded-time window must be a non-empty half-open interval"},
		}
	}

	streamKeys, err := coveredStreams(ctx, q, req.Tenant, from, to)
	if err != nil {
		return Content{}, err
	}

	content := Content{
		Tenant:     req.Tenant,
		CoversFrom: from,
		CoversTo:   to,
		Schema:     req.Schema,
		Streams:    make([]Stream, 0, len(streamKeys)),
	}
	for _, streamKey := range streamKeys {
		stream, err := readStream(ctx, q, req.Tenant, streamKey, from, to)
		if err != nil {
			return Content{}, err
		}
		content.Streams = append(content.Streams, stream)
	}

	epochs, err := coveringEpochs(ctx, q, req.Tenant, from, to)
	if err != nil {
		return Content{}, err
	}
	content.Epochs = epochs
	return content, nil
}

// coveredStreams lists every stream holding an event recorded inside the
// window, in stream-key order.
func coveredStreams(ctx context.Context, q Querier, tenant uuid.UUID, from, to time.Time) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT stream_key
		FROM ledger_event
		WHERE tenant_id = $1 AND recorded_at >= $2 AND recorded_at < $3
		ORDER BY stream_key ASC`, tenant, from, to)
	if err != nil {
		return nil, fmt.Errorf("evidence: list covered streams: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var streamKey string
		if err := rows.Scan(&streamKey); err != nil {
			return nil, fmt.Errorf("evidence: scan covered stream: %w", err)
		}
		out = append(out, streamKey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence: list covered streams: %w", err)
	}
	return out, nil
}

// readStream assembles one stream: its head as of the window's end, the
// whole chain up to that head, and the envelopes recorded inside the window.
//
// The head is taken at the window's end rather than "now" so that a package
// says the same thing when it is exported again later, after the stream has
// moved on.
func readStream(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string, from, to time.Time) (Stream, error) {
	head, err := headAsOf(ctx, q, tenant, streamKey, to)
	if err != nil {
		return Stream{}, err
	}

	digests, err := hashchain.ReadEventDigests(ctx, q, tenant, streamKey)
	if err != nil {
		return Stream{}, err
	}
	links, err := hashchain.ReadLinks(ctx, q, tenant, streamKey)
	if err != nil {
		return Stream{}, err
	}
	// Both reads return the whole stream as it stands now; the package
	// carries only what the window's head commits to.
	digests = truncateDigests(digests, head)
	links = truncateLinks(links, head)

	stream := Stream{StreamKey: streamKey, Links: links, Digests: digests}
	if n := len(links); n > 0 && links[n-1].Sequence == head {
		stream.Head = StreamHead{
			StreamKey:      streamKey,
			Sequence:       head,
			ChainHash:      links[n-1].ChainHash,
			ChainAlgorithm: links[n-1].Algorithm,
		}
	}

	events, err := readEvents(ctx, q, tenant, streamKey, from, to)
	if err != nil {
		return Stream{}, err
	}
	stream.Events = events
	return stream, nil
}

func headAsOf(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string, at time.Time) (int64, error) {
	var head *int64
	if err := q.QueryRow(ctx, `
		SELECT MAX(sequence) FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND recorded_at < $3`,
		tenant, streamKey, at).Scan(&head); err != nil {
		return 0, fmt.Errorf("evidence: read head of stream %s: %w", streamKey, err)
	}
	if head == nil {
		return 0, nil
	}
	return *head, nil
}

func truncateDigests(in []EventDigest, head int64) []EventDigest {
	out := make([]EventDigest, 0, len(in))
	for _, d := range in {
		if d.Sequence <= head {
			out = append(out, d)
		}
	}
	return out
}

func truncateLinks(in []ChainLink, head int64) []ChainLink {
	out := make([]ChainLink, 0, len(in))
	for _, link := range in {
		if link.Sequence <= head {
			// recorded_at on a chain link is when the link was written, not
			// what it proves. Dropping it keeps the package a function of the
			// evidence rather than of the writer's clock.
			link.RecordedAt = time.Time{}
			out = append(out, link)
		}
	}
	return out
}

const selectEventColumns = `
	tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
	source_ref, schema_ref, payload, artifact_ref, canonical_length, digest,
	digest_algorithm, occurred_at, effective_at, recorded_at, correlation_id,
	causation_id, idempotency_key, corrects_stream_key, corrects_sequence`

// readEvents returns the full envelopes recorded inside the window, ordered
// by sequence. Every row's own tenant_id is selected and compared: row level
// security (migration 00008) already makes another tenant's row unreadable,
// and this is what makes a row that somehow arrived anyway refuse to become
// evidence instead of being filed under the wrong tenant.
func readEvents(ctx context.Context, q Querier, tenant uuid.UUID, streamKey string, from, to time.Time) ([]Event, error) {
	rows, err := q.Query(ctx, `
		SELECT `+selectEventColumns+`
		FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND recorded_at >= $3 AND recorded_at < $4
		ORDER BY sequence ASC`, tenant, streamKey, from, to)
	if err != nil {
		return nil, fmt.Errorf("evidence: read covered events on stream %s: %w", streamKey, err)
	}
	defer rows.Close()

	var out []Event
	for rows.Next() {
		var (
			e              Event
			assertionClass string
			authority      *string
			artifact       *string
			causation      *uuid.UUID
			correctsStream *string
			correctsSeq    *int64
		)
		if err := rows.Scan(
			&e.Tenant, &e.StreamKey, &e.Sequence, &e.EventID, &assertionClass, &authority,
			&e.SourceRef, &e.SchemaRef, &e.Payload, &artifact, &e.CanonicalLength, &e.Digest,
			&e.DigestAlgorithm, &e.OccurredAt, &e.EffectiveAt, &e.RecordedAt, &e.CorrelationID,
			&causation, &e.IdempotencyKey, &correctsStream, &correctsSeq,
		); err != nil {
			return nil, fmt.Errorf("evidence: scan covered event on stream %s: %w", streamKey, err)
		}
		if e.Tenant != tenant {
			return nil, ErrTenantLeak{
				Expected: tenant, Found: e.Tenant,
				Where: fmt.Sprintf("event %s@%d", e.StreamKey, e.Sequence),
			}
		}
		e.AssertionClass = datalogger.AssertionClass(assertionClass)
		if authority != nil {
			e.Authority = *authority
		}
		if artifact != nil {
			e.ArtifactRef = *artifact
		}
		if causation != nil {
			e.CausationID = *causation
		}
		if correctsStream != nil && correctsSeq != nil {
			e.Corrects = &EventRef{StreamKey: *correctsStream, Sequence: *correctsSeq}
		}
		e.OccurredAt = Truncate(e.OccurredAt)
		e.EffectiveAt = Truncate(e.EffectiveAt)
		e.RecordedAt = Truncate(e.RecordedAt)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evidence: read covered events on stream %s: %w", streamKey, err)
	}
	return out, nil
}

// coveringEpochs returns every signed checkpoint epoch whose own window
// intersects the exported window, in epoch-number order. An epoch that
// merely touches the boundary is not an intersection: the windows are
// half-open on both sides.
func coveringEpochs(ctx context.Context, q Querier, tenant uuid.UUID, from, to time.Time) ([]Epoch, error) {
	all, err := checkpoint.NewStore().List(ctx, q, tenant)
	if err != nil {
		return nil, err
	}
	var out []Epoch
	for _, epoch := range all {
		if epoch.Tenant != tenant {
			return nil, ErrTenantLeak{
				Expected: tenant, Found: epoch.Tenant,
				Where: fmt.Sprintf("checkpoint epoch %d", epoch.EpochNumber),
			}
		}
		if epoch.CoversFrom.Before(to) && epoch.CoversTo.After(from) {
			out = append(out, epoch)
		}
	}
	return out, nil
}
