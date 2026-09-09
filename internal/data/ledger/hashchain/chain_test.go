package hashchain_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
)

// TestTodo_DATA_004 proves the per-stream hash chain: valid chains verify and
// reproduce the recorded head digest, and a missing, reordered, altered, or
// duplicated event, or a wrong prior hash, is refused with the exact broken
// stream and sequence named rather than verifying successfully.
func TestTodo_DATA_004(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)

	t.Run("a valid chain verifies and reproduces the recorded head", func(t *testing.T) {
		var head int64
		var links []hashchain.ChainedLink
		for i := 0; i < 5; i++ {
			link := f.appendLinked(t, f.request(head))
			head = link.Sequence
			links = append(links, link)
		}

		got, err := digester.Verify(context.Background(), f.db.Conn, f.tenant, streamKey)
		if err != nil {
			t.Fatalf("verify an intact chain: %v", err)
		}
		want := links[len(links)-1]
		if got.Sequence != want.Sequence || got.ChainHash != want.ChainHash || got.Algorithm != want.Algorithm {
			t.Fatalf("verify reproduced %+v, want the recorded head %+v", got, want)
		}

		checkpoint, ok, err := hashchain.CurrentHead(context.Background(), f.db.Conn, f.tenant, streamKey)
		if err != nil || !ok {
			t.Fatalf("read current head: ok=%v err=%v", ok, err)
		}
		if checkpoint != got {
			t.Fatalf("stored checkpoint %+v does not match the reproduced head %+v", checkpoint, got)
		}
	})

	t.Run("genesis: the first link's prior hash is the genesis sentinel", func(t *testing.T) {
		g := newFixture(t)
		link := g.appendLinked(t, g.request(0))
		if link.PrevHash != hashchain.GenesisHash {
			t.Fatalf("first link prior hash is %q, want the genesis sentinel %q", link.PrevHash, hashchain.GenesisHash)
		}
		if link.ChainHash == "" || link.ChainHash == link.PrevHash {
			t.Fatalf("first link chain hash %q must differ from and be no weaker than the prior hash", link.ChainHash)
		}
	})

	t.Run("a gap in the event stream is refused with the exact sequence", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a")},
			{Sequence: 3, EventID: uuid.New(), Digest: fixedDigest("c")}, // sequence 2 is missing
		}
		links, err := digester.Fold(streamKey, events)
		var broken hashchain.ErrChainBroken
		if err == nil {
			t.Fatalf("folding a gapped stream produced %v, want a failure", links)
		}
		if !errors.As(err, &broken) {
			t.Fatalf("gap returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 2 {
			t.Fatalf("gap located at sequence %d, want 2", broken.Sequence)
		}
	})

	t.Run("a reordered event stream is refused with the exact sequence", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a")},
			{Sequence: 2, EventID: uuid.New(), Digest: fixedDigest("b")},
		}
		// Swap the order the events are presented in: sequence 2 first.
		reordered := []hashchain.EventDigest{events[1], events[0]}
		_, err := digester.Fold(streamKey, reordered)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("reordering returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 1 {
			t.Fatalf("reordering located at sequence %d, want 1", broken.Sequence)
		}
	})

	t.Run("a duplicated sequence is refused with the exact sequence", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a")},
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a-again")}, // duplicate sequence 1
		}
		_, err := digester.Fold(streamKey, events)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("duplicated sequence returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 2 {
			t.Fatalf("duplicate located at sequence %d, want 2", broken.Sequence)
		}
	})

	t.Run("an altered event digest breaks verification at that exact sequence", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a")},
			{Sequence: 2, EventID: uuid.New(), Digest: fixedDigest("b")},
			{Sequence: 3, EventID: uuid.New(), Digest: fixedDigest("c")},
		}
		links, err := digester.Fold(streamKey, events)
		if err != nil {
			t.Fatalf("fold a clean stream: %v", err)
		}

		// The digest recorded for sequence 2 is altered after the fact; the
		// chain link at sequence 2 (minted from the true digest) is untouched.
		tampered := append([]hashchain.EventDigest(nil), events...)
		tampered[1].Digest = fixedDigest("b-altered")

		_, err = digester.VerifyLinks(streamKey, tampered, links)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("an altered digest returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 2 {
			t.Fatalf("alteration located at sequence %d, want 2 (the exact altered event)", broken.Sequence)
		}
	})

	t.Run("a wrong prior hash on one link breaks verification at that exact sequence", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a")},
			{Sequence: 2, EventID: uuid.New(), Digest: fixedDigest("b")},
			{Sequence: 3, EventID: uuid.New(), Digest: fixedDigest("c")},
		}
		links, err := digester.Fold(streamKey, events)
		if err != nil {
			t.Fatalf("fold a clean stream: %v", err)
		}

		tampered := append([]hashchain.ChainedLink(nil), links...)
		tampered[1].PrevHash = "not-the-real-prior-hash"

		_, err = digester.VerifyLinks(streamKey, events, tampered)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("a wrong prior hash returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 2 {
			t.Fatalf("wrong prior hash located at sequence %d, want 2", broken.Sequence)
		}
	})

	t.Run("a changed recorded algorithm breaks verification", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("algorithm")},
		}
		links, err := digester.Fold(streamKey, events)
		if err != nil {
			t.Fatalf("fold a clean stream: %v", err)
		}
		links[0].Algorithm = "sha512"

		_, err = digester.VerifyLinks(streamKey, events, links)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) || broken.Sequence != 1 {
			t.Fatalf("algorithm substitution returned %v, want ErrChainBroken at sequence 1", err)
		}
	})

	t.Run("a link carrying another stream key is refused", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("stream")},
		}
		links, err := digester.Fold(streamKey, events)
		if err != nil {
			t.Fatalf("fold a clean stream: %v", err)
		}
		links[0].StreamKey = "other-stream"

		_, err = digester.VerifyLinks(streamKey, events, links)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) || broken.Sequence != 1 {
			t.Fatalf("cross-stream link returned %v, want ErrChainBroken at sequence 1", err)
		}
	})

	t.Run("a chain link recorded for the wrong event is refused", func(t *testing.T) {
		events := []hashchain.EventDigest{
			{Sequence: 1, EventID: uuid.New(), Digest: fixedDigest("a")},
			{Sequence: 2, EventID: uuid.New(), Digest: fixedDigest("b")},
		}
		links, err := digester.Fold(streamKey, events)
		if err != nil {
			t.Fatalf("fold a clean stream: %v", err)
		}

		substituted := append([]hashchain.ChainedLink(nil), links...)
		substituted[1].EventID = uuid.New() // some other event entirely

		_, err = digester.VerifyLinks(streamKey, events, substituted)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("a substituted event id returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 2 {
			t.Fatalf("substitution located at sequence %d, want 2", broken.Sequence)
		}
	})

	t.Run("an empty stream reports no head rather than a vacuous success", func(t *testing.T) {
		_, err := digester.VerifyLinks(streamKey, nil, nil)
		var empty hashchain.ErrEmptyStream
		if !errors.As(err, &empty) {
			t.Fatalf("verifying an empty stream returned %v, want ErrEmptyStream", err)
		}
	})

	t.Run("an event appended without a chain link fails Verify against real data", func(t *testing.T) {
		g := newFixture(t)
		g.appendLinked(t, g.request(0))
		// Appended straight through internal/data/ledger, bypassing
		// hashchain.Appender: the event is real, but it was never linked.
		g.appendUnlinked(t, g.request(1))

		_, err := digester.Verify(context.Background(), g.db.Conn, g.tenant, streamKey)
		var broken hashchain.ErrChainBroken
		if !errors.As(err, &broken) {
			t.Fatalf("verifying a stream with an unlinked event returned %v, want ErrChainBroken", err)
		}
		if broken.Sequence != 2 {
			t.Fatalf("the missing link was located at sequence %d, want 2", broken.Sequence)
		}
	})
}

// TestTodo_DATA_004_Golden pins the exact chain hash a fixed sequence of
// event digests produces against a checked-in vector
// (testdata/golden_chain_head.txt), so an unnoticed change to the
// chain-link preimage, framing, or algorithm shows up as a diff here rather
// than as a silent change in meaning for every hash chain already recorded.
// Regenerate the vector with `go test -run TestTodo_DATA_004_Golden -update`
// after a deliberate, versioned change to the chain-link profile.
func TestTodo_DATA_004_Golden(t *testing.T) {
	t.Parallel()
	registry, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	digester := hashchain.NewDigester(registry)

	events := []hashchain.EventDigest{
		{Sequence: 1, EventID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Digest: fixedDigest("golden-a")},
		{Sequence: 2, EventID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Digest: fixedDigest("golden-b")},
		{Sequence: 3, EventID: uuid.MustParse("00000000-0000-0000-0000-000000000003"), Digest: fixedDigest("golden-c")},
	}

	links, err := digester.Fold("golden-stream", events)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	got := links[len(links)-1]

	if got.Algorithm != "sha256" {
		t.Fatalf("chain algorithm is %q, want sha256", got.Algorithm)
	}
	if links[0].PrevHash != hashchain.GenesisHash {
		t.Fatalf("first link prior hash is %q, want genesis", links[0].PrevHash)
	}
	if links[1].PrevHash != links[0].ChainHash || links[2].PrevHash != links[1].ChainHash {
		t.Fatal("a link's prior hash does not chain to its predecessor's chain hash")
	}

	golden(t, "golden_chain_head.txt", []byte(got.ChainHash+"\n"))

	// Two independent Fold calls over the same input must agree byte for
	// byte: the chain is a pure function of the event digests, never of
	// anything incidental such as map iteration order or wall-clock time -
	// this is what makes "replay proves the head is stable" checkable at
	// all.
	again, err := digester.Fold("golden-stream", events)
	if err != nil {
		t.Fatalf("fold again: %v", err)
	}
	if again[len(again)-1].ChainHash != got.ChainHash {
		t.Fatalf("two folds of the same events produced different heads: %s vs %s",
			got.ChainHash, again[len(again)-1].ChainHash)
	}
}

// golden compares got against the checked-in vector at testdata/name,
// mirroring internal/kernel/canonical's own golden-vector discipline: the
// checked-in bytes are the contract, and -update rewrites them after a
// deliberate change.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("chain head drifted from %s\n got: %s want: %s", path, got, want)
	}
}

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

// fixedDigest returns a syntactically valid, deterministic hex digest for a
// label, standing in for a real LEDGER-002/DATA-003 event digest in tests
// that only care about chaining behavior, not payload canonicalization.
func fixedDigest(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}
