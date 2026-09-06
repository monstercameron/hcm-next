package fx

import (
	"context"
	"errors"
	"testing"
)

func TestTodo_PERSIST_FX_001(t *testing.T) {
	store := NewMemoryStore()
	source := validFXSource(t, "memory-source")
	profile := validFXProfile(t, source.SourceID)
	quote := validFXQuote(t, "memory-quote", source.SourceID, "2026-06-01T10:00:00Z")
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversionProfile(context.Background(), "tenant-a", profile); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuote(context.Background(), "tenant-a", quote); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadQuote(context.Background(), "tenant-a", quote.QuoteID)
	if err != nil || got.CanonicalDigest != quote.CanonicalDigest {
		t.Fatalf("quote round trip = %+v, err %v", got, err)
	}
}

func TestTodo_PERSIST_FX_001_Fault(t *testing.T) {
	store := NewMemoryStore()
	source := validFXSource(t, "fault-source")
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); !errors.Is(err, ErrStoreDuplicateRevision) {
		t.Fatalf("duplicate source = %v, want duplicate revision", err)
	}
	next, err := NewRateSourceRevision(RateSourceRevision{
		SourceID: "fault-source", Revision: 2, ParentRevision: 1, ParentDigest: "sha256:" + "0" + source.CanonicalDigest[len("sha256:")+1:],
		ProviderRef: "provider:fault-source", Pairs: source.Pairs, QuoteCadence: CadenceHourly,
		AuthorityClass: AuthorityPrimary, Effective: source.Effective,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRateSource(context.Background(), "tenant-a", next); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("stale source = %v, want stale CAS", err)
	}
}

func TestTodo_PERSIST_FX_001_Integration(t *testing.T) {
	store := NewMemoryStore()
	source := validFXSource(t, "integration-source")
	quote := validFXQuote(t, "integration-quote", source.SourceID, "2026-06-01T10:00:00Z")
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveQuote(context.Background(), "tenant-a", quote); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadRateSource(context.Background(), "tenant-a", source.SourceID, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != source.CanonicalDigest || len(got.Pairs) != len(source.Pairs) {
		t.Fatalf("source round trip = %+v", got)
	}
	gotQuote, err := store.LoadQuote(context.Background(), "tenant-a", quote.QuoteID)
	if err != nil || gotQuote.Rate.String() != quote.Rate.String() {
		t.Fatalf("quote round trip = %+v, err %v", gotQuote, err)
	}
}

func TestTodo_PERSIST_FX_001_Security(t *testing.T) {
	store := NewMemoryStore()
	source := validFXSource(t, "security-source")
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadRateSource(context.Background(), "tenant-b", source.SourceID, source.Revision); !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("cross-tenant load = %v, want not found", err)
	}
}

func TestTodo_PERSIST_FX_001_Recovery(t *testing.T) {
	store := NewMemoryStore()
	source := validFXSource(t, "recovery-source")
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadRateSource(context.Background(), "tenant-a", source.SourceID, source.Revision)
	if err != nil || got.CanonicalDigest != source.CanonicalDigest {
		t.Fatalf("memory recovery = %+v, err %v", got, err)
	}
}

func TestTodo_PERSIST_FX_001_Mutation(t *testing.T) {
	store := NewMemoryStore()
	source := validFXSource(t, "mutation-source")
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); err != nil {
		t.Fatal(err)
	}
	source.Pairs[0].Base = "GBP"
	if err := store.SaveRateSource(context.Background(), "tenant-a", source); !errors.Is(err, ErrStoreInvalid) {
		t.Fatalf("stale source digest = %v, want invalid refusal", err)
	}
	got, err := store.LoadRateSource(context.Background(), "tenant-a", "mutation-source", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pairs[0].Base != "USD" {
		t.Fatalf("stored source changed after caller mutation: %+v", got.Pairs)
	}
}
