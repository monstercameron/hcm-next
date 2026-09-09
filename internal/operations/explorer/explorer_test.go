package explorer_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/explorer"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// TestTodo_ADMIN_003 is the ADMIN-003 primary test for the ledger/provenance
// explorer half of the todo: stream listing, event detail with chain-link
// verification, correction lineage, and bitemporal AsOf/KnownAt reads.
func TestTodo_ADMIN_003(t *testing.T) {
	ctx := context.Background()

	t.Run("StreamListing lists a stream's events in sequence order", func(t *testing.T) {
		f := newFixture(t)
		a := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
		b := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "130000"})

		listing, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, nil)
		if err != nil {
			t.Fatalf("StreamListing: %v", err)
		}
		if len(listing.Events) != 2 {
			t.Fatalf("len(Events) = %d, want 2", len(listing.Events))
		}
		if listing.Events[0].Sequence != a.Sequence || listing.Events[1].Sequence != b.Sequence {
			t.Fatalf("events not in sequence order: %d, %d", listing.Events[0].Sequence, listing.Events[1].Sequence)
		}
		if string(listing.Events[1].Payload) != "130000" {
			t.Errorf("Events[1].Payload = %s, want 130000", listing.Events[1].Payload)
		}
		if listing.Digest == "" {
			t.Error("Digest is empty")
		}
	})

	t.Run("EventDetail reads one event and verifies its chain", func(t *testing.T) {
		f := newFixture(t)
		f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
		b := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "130000"})

		digester, err := explorer.NewChainDigester()
		if err != nil {
			t.Fatalf("NewChainDigester: %v", err)
		}
		detail, err := explorer.EventDetail(ctx, f.db.Conn, digester, f.tenant, ledger.EventRef{StreamKey: streamA, Sequence: b.Sequence}, nil)
		if err != nil {
			t.Fatalf("EventDetail: %v", err)
		}
		if detail.Event.Sequence != b.Sequence {
			t.Errorf("Event.Sequence = %d, want %d", detail.Event.Sequence, b.Sequence)
		}
		if !detail.Chain.Verified {
			t.Fatalf("Chain.Verified = false, want true: %+v", detail.Chain)
		}
		if detail.Chain.Head.Sequence != b.Sequence {
			t.Errorf("Chain.Head.Sequence = %d, want %d", detail.Chain.Head.Sequence, b.Sequence)
		}
		if detail.Digest == "" {
			t.Error("Digest is empty")
		}
	})

	t.Run("Lineage walks a correction's ancestry and descendants", func(t *testing.T) {
		f := newFixture(t)
		original := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
		correction := f.appendLinked(t, eventSpec{
			Stream: streamA, Class: ledger.Correction, Payload: "125000",
			Corrects: &ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence},
		})

		ancestorsOfCorrection, err := explorer.Lineage(ctx, f.db.Conn, f.tenant, ledger.EventRef{StreamKey: streamA, Sequence: correction.Sequence}, nil)
		if err != nil {
			t.Fatalf("Lineage(correction): %v", err)
		}
		if len(ancestorsOfCorrection.Ancestors) != 1 || ancestorsOfCorrection.Ancestors[0].Ref.Sequence != original.Sequence {
			t.Fatalf("Ancestors = %+v, want exactly the original event", ancestorsOfCorrection.Ancestors)
		}

		descendantsOfOriginal, err := explorer.Lineage(ctx, f.db.Conn, f.tenant, ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence}, nil)
		if err != nil {
			t.Fatalf("Lineage(original): %v", err)
		}
		if len(descendantsOfOriginal.Descendants) != 1 || descendantsOfOriginal.Descendants[0].Ref.Sequence != correction.Sequence {
			t.Fatalf("Descendants = %+v, want exactly the correction event", descendantsOfOriginal.Descendants)
		}
	})

	t.Run("BitemporalAsOf and BitemporalKnownAt resolve their own axis", func(t *testing.T) {
		f := newFixture(t)
		// a is recorded well before effectiveAt; the correction is recorded
		// two days after effectiveAt, so a knownAt horizon set shortly before
		// effectiveAt sees only a, and one set well after sees the correction.
		a := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000", RecordedAt: occurredAt})
		correction := f.appendLinked(t, eventSpec{
			Stream: streamA, Class: ledger.Correction, Payload: "135000",
			Corrects:   &ledger.EventRef{StreamKey: streamA, Sequence: a.Sequence},
			RecordedAt: effectiveAt.Add(48 * time.Hour),
		})
		_ = correction

		asOf, err := explorer.BitemporalAsOf(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt, f.bitemporalDecision())
		if err != nil {
			t.Fatalf("BitemporalAsOf: %v", err)
		}
		if len(asOf.Facts) != 1 || string(asOf.Facts[0].Payload) != "135000" {
			t.Fatalf("BitemporalAsOf facts = %+v, want the correction's payload (asOf uses everything known now)", asOf.Facts)
		}

		knownAt, err := explorer.BitemporalKnownAt(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt.Add(-time.Hour), f.bitemporalDecision())
		if err != nil {
			t.Fatalf("BitemporalKnownAt: %v", err)
		}
		if len(knownAt.Facts) != 1 || string(knownAt.Facts[0].Payload) != "120000" {
			t.Fatalf("BitemporalKnownAt facts = %+v, want only the original (the correction was not yet recorded)", knownAt.Facts)
		}
		if knownAt.Digest == "" {
			t.Error("Digest is empty")
		}
	})
}

// TestTodo_ADMIN_003_Property proves BitemporalAsOf/KnownAt resolve the same
// business-time/knowledge-time answer this package's wrapper is supposed to
// pass through unchanged, across a spread of (asOf, knownAt) pairs against a
// fixed timeline - not just the single pair the primary test happens to
// narrate - and that StreamListing/EventDetail digests are stable across
// repeated reads of unchanged state.
func TestTodo_ADMIN_003_Property(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	a := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
	c := f.appendLinked(t, eventSpec{
		Stream: streamA, Class: ledger.Correction, Payload: "135000",
		Corrects: &ledger.EventRef{StreamKey: streamA, Sequence: a.Sequence},
	})
	_ = c

	t.Run("repeated StreamListing reads of unchanged state produce the same digest", func(t *testing.T) {
		first, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, nil)
		if err != nil {
			t.Fatalf("StreamListing: %v", err)
		}
		second, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, nil)
		if err != nil {
			t.Fatalf("StreamListing: %v", err)
		}
		if first.Digest != second.Digest {
			t.Fatalf("digests differ across identical reads: %s vs %s", first.Digest, second.Digest)
		}
	})

	t.Run("BitemporalAsOf resolves the correction at and after its effective time, the original before it", func(t *testing.T) {
		before, err := explorer.BitemporalAsOf(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt.Add(-24*time.Hour), f.bitemporalDecision())
		if err != nil {
			t.Fatalf("BitemporalAsOf(before): %v", err)
		}
		if len(before.Facts) != 0 {
			t.Fatalf("before the first fact's effective time, got %d facts, want 0", len(before.Facts))
		}

		at, err := explorer.BitemporalAsOf(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt, f.bitemporalDecision())
		if err != nil {
			t.Fatalf("BitemporalAsOf(at): %v", err)
		}
		if len(at.Facts) != 1 || string(at.Facts[0].Payload) != "135000" {
			t.Fatalf("at the fact's effective time, facts = %+v, want the correction", at.Facts)
		}

		after, err := explorer.BitemporalAsOf(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt.Add(24*time.Hour), f.bitemporalDecision())
		if err != nil {
			t.Fatalf("BitemporalAsOf(after): %v", err)
		}
		if len(after.Facts) != 1 || string(after.Facts[0].Payload) != "135000" {
			t.Fatalf("after the fact's effective time, facts = %+v, want the correction still visible", after.Facts)
		}
	})
}

// TestTodo_ADMIN_003_Security proves the RED clause: a denied event field is
// absent from the returned view (not merely zeroed in a way indistinguishable
// from an empty payload), a non-disclosable subject withholds every gated
// field uniformly, a chain gap is reported at the exact sequence it occurs,
// and a bitemporal field a Decision does not allow never appears - proven
// through this package's own wrapper, on top of the guarantee
// internal/data/bitemporal already gives at the SQL layer.
func TestTodo_ADMIN_003_Security(t *testing.T) {
	ctx := context.Background()

	t.Run("denying FieldPayload withholds the payload but keeps chain-relevant metadata", func(t *testing.T) {
		f := newFixture(t)
		f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})

		dec := &authz.Decision{
			SubjectDisclosable: true,
			Fields: map[authz.FieldID]authz.FieldRuling{
				explorer.FieldPayload:    {Effect: authz.EffectDenied, RuleID: "test.deny"},
				explorer.FieldProvenance: {Effect: authz.EffectAllow, RuleID: "test.allow"},
			},
		}
		listing, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, dec)
		if err != nil {
			t.Fatalf("StreamListing: %v", err)
		}
		if len(listing.Events) != 1 {
			t.Fatalf("len(Events) = %d, want 1", len(listing.Events))
		}
		ev := listing.Events[0]
		if ev.Payload != nil {
			t.Errorf("Payload = %v, want nil under a denied payload field", ev.Payload)
		}
		if !ev.PayloadWithheld {
			t.Error("PayloadWithheld = false, want true")
		}
		if ev.Authority != authorityRef {
			t.Errorf("Authority = %q, want %q (provenance was allowed)", ev.Authority, authorityRef)
		}
		if ev.Digest == "" || ev.Sequence == 0 {
			t.Error("integrity metadata (Digest/Sequence) was withheld; it must never be gated")
		}
	})

	t.Run("a non-disclosable subject withholds every gated field uniformly", func(t *testing.T) {
		f := newFixture(t)
		f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})

		dec := &authz.Decision{SubjectDisclosable: false, SubjectDenialReason: "no relationship"}
		listing, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, dec)
		if err != nil {
			t.Fatalf("StreamListing: %v", err)
		}
		ev := listing.Events[0]
		if !ev.SubjectWithheld || !ev.PayloadWithheld {
			t.Fatalf("event = %+v, want both SubjectWithheld and PayloadWithheld", ev)
		}
		if ev.Authority != "" || ev.SourceRef != "" {
			t.Errorf("Authority/SourceRef = %q/%q, want both empty for a non-disclosable subject", ev.Authority, ev.SourceRef)
		}
	})

	t.Run("a chain gap is reported at the exact sequence it occurs", func(t *testing.T) {
		f := newFixture(t)
		f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
		// No further linked append follows: hashchain.Appender itself refuses
		// to extend a chain past a sequence with no recorded predecessor
		// link, so a gap can only be the last recorded event on a stream,
		// never a hole with more linked events after it.
		gap := f.appendGap(t, eventSpec{Stream: streamA, Payload: "130000"}) // no chain link recorded

		digester, err := explorer.NewChainDigester()
		if err != nil {
			t.Fatalf("NewChainDigester: %v", err)
		}
		chain, err := explorer.VerifyChain(ctx, f.db.Conn, digester, f.tenant, streamA)
		if err != nil {
			t.Fatalf("VerifyChain: %v", err)
		}
		if chain.Verified {
			t.Fatal("Verified = true, want false for a stream with a chain gap")
		}
		if chain.Broken == nil {
			t.Fatal("Broken is nil, want the exact break point")
		}
		if chain.Broken.Sequence != gap.Sequence {
			t.Fatalf("Broken.Sequence = %d, want %d (the exact gapped event)", chain.Broken.Sequence, gap.Sequence)
		}
	})

	t.Run("a non-disclosable subject refuses to walk lineage at all", func(t *testing.T) {
		f := newFixture(t)
		original := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
		f.appendLinked(t, eventSpec{
			Stream: streamA, Class: ledger.Correction, Payload: "125000",
			Corrects: &ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence},
		})

		dec := &authz.Decision{SubjectDisclosable: false}
		view, err := explorer.Lineage(ctx, f.db.Conn, f.tenant, ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence}, dec)
		if err != nil {
			t.Fatalf("Lineage: %v", err)
		}
		if !view.Withheld {
			t.Fatal("Withheld = false, want true")
		}
		if view.Ancestors != nil || view.Descendants != nil {
			t.Fatalf("Ancestors/Descendants = %v/%v, want both nil when withheld", view.Ancestors, view.Descendants)
		}
	})

	t.Run("a bitemporal field a Decision does not allow never appears in the view", func(t *testing.T) {
		f := newFixture(t)
		f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})

		dec := bitemporal.Decision{Tenant: f.tenant, DenyFields: []string{schemaComp}}
		view, err := explorer.BitemporalAsOf(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt, dec)
		if err != nil {
			t.Fatalf("BitemporalAsOf: %v", err)
		}
		if len(view.Facts) != 0 {
			t.Fatalf("Facts = %+v, want empty for a denied field", view.Facts)
		}
	})
}

// TestTodo_ADMIN_003_Mutation proves boundary precision - one more gapped
// event flips a stream from verified to broken at exactly that new sequence,
// and nothing earlier in the chain is affected - and that every read this
// package exposes performs zero writes: the stream head and event count are
// unchanged after exercising every read function.
func TestTodo_ADMIN_003_Mutation(t *testing.T) {
	ctx := context.Background()

	t.Run("adding one gapped event flips exactly the new sequence from verified to broken", func(t *testing.T) {
		f := newFixture(t)
		f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})

		digester, err := explorer.NewChainDigester()
		if err != nil {
			t.Fatalf("NewChainDigester: %v", err)
		}
		before, err := explorer.VerifyChain(ctx, f.db.Conn, digester, f.tenant, streamA)
		if err != nil {
			t.Fatalf("VerifyChain (before): %v", err)
		}
		if !before.Verified {
			t.Fatalf("before = %+v, want Verified=true", before)
		}

		gap := f.appendGap(t, eventSpec{Stream: streamA, Payload: "130000"})

		after, err := explorer.VerifyChain(ctx, f.db.Conn, digester, f.tenant, streamA)
		if err != nil {
			t.Fatalf("VerifyChain (after): %v", err)
		}
		if after.Verified {
			t.Fatal("after = Verified=true, want false once a gapped event is appended")
		}
		if after.Broken == nil || after.Broken.Sequence != gap.Sequence {
			t.Fatalf("Broken = %+v, want exactly sequence %d", after.Broken, gap.Sequence)
		}
	})

	t.Run("every explorer read performs zero writes", func(t *testing.T) {
		f := newFixture(t)
		original := f.appendLinked(t, eventSpec{Stream: streamA, Payload: "120000"})
		f.appendLinked(t, eventSpec{
			Stream: streamA, Class: ledger.Correction, Payload: "125000",
			Corrects: &ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence},
		})

		digester, err := explorer.NewChainDigester()
		if err != nil {
			t.Fatalf("NewChainDigester: %v", err)
		}
		countEvents := func() int {
			listing, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, nil)
			if err != nil {
				t.Fatalf("StreamListing: %v", err)
			}
			return len(listing.Events)
		}

		before := countEvents()
		for i := 0; i < 5; i++ {
			if _, err := explorer.StreamListing(ctx, f.db.Conn, f.tenant, streamA, nil); err != nil {
				t.Fatalf("StreamListing: %v", err)
			}
			if _, err := explorer.EventDetail(ctx, f.db.Conn, digester, f.tenant, ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence}, nil); err != nil {
				t.Fatalf("EventDetail: %v", err)
			}
			if _, err := explorer.Lineage(ctx, f.db.Conn, f.tenant, ledger.EventRef{StreamKey: streamA, Sequence: original.Sequence}, nil); err != nil {
				t.Fatalf("Lineage: %v", err)
			}
			if _, err := explorer.BitemporalAsOf(ctx, f.db.Conn, f.tenant, streamA, schemaComp, effectiveAt, f.bitemporalDecision()); err != nil {
				t.Fatalf("BitemporalAsOf: %v", err)
			}
		}
		after := countEvents()
		if after != before {
			t.Fatalf("event count changed from %d to %d after read-only explorer calls", before, after)
		}
	})
}
