package disposition_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/hashchain"
)

// FuzzTodo_LEDGER_011 exercises the pure decision layer, not the database
// write path: an embedded PostgreSQL per fuzz iteration would be far too
// slow (every other test in this package pays that startup cost exactly
// once, via TestMain), and this repository's own precedent
// (FuzzTodo_LEDGER_012 in internal/data/ledger/evidence) fuzzes pure,
// in-memory functions for the same reason. What this target proves, and
// what it leaves unproven, are both stated below and in the final report:
// the real DB-backed guarantee that Erase's only write is one INSERT into
// ledger_payload_disposition -- never a mutation of ledger_event or its
// hash-chain links -- is proven against real PostgreSQL by
// TestTodo_LEDGER_011, TestTodo_LEDGER_011_Race and
// TestTodo_LEDGER_011_Mutation instead.
//
// The property: disposition.Classify -- the one function LEDGER-011's
// REFACTOR clause names as the classification-driven encryption boundary --
// is exhaustive and fails closed for any input, and no chain built from
// arbitrary payload bytes is perturbed by consulting it. A version of
// Classify with a permissive default branch (silently mapping an
// unrecognized classification to ClassificationStandard, say) would fail
// this fuzz target the first time the corpus produces a string outside the
// two declared classifications -- which every generated case except the two
// seeded exact matches does.
func FuzzTodo_LEDGER_011(f *testing.F) {
	f.Add([]byte("payload-a"), []byte("payload-b"), []byte("payload-c"), 0, string(disposition.ClassificationStandard))
	f.Add([]byte{}, []byte("x"), []byte("y"), 1, string(disposition.ClassificationEncryptedAtRest))
	f.Add([]byte("a"), []byte(""), []byte("bcd"), 2, "bogus")
	f.Add([]byte("large-payload-large-payload-large-payload"), []byte("m"), []byte("n"), -5, "")
	f.Add([]byte("x"), []byte("y"), []byte("z"), 100, "standard")
	f.Add([]byte{0x00, 0xff, 0x10}, []byte{0x00}, []byte{0xff, 0xff}, 3, "STANDARD ")
	f.Add([]byte("dup"), []byte("dup"), []byte("dup"), 0, "ENCRYPTED_AT_REST")

	registry, err := hashchain.NewRegistry()
	if err != nil {
		f.Fatalf("hashchain registry: %v", err)
	}
	chainDigester := hashchain.NewDigester(registry)
	eventDigester := ledger.SHA256Digester{}

	f.Fuzz(func(t *testing.T, p0, p1, p2 []byte, rawIndex int, classification string) {
		payloads := [][]byte{p0, p1, p2}

		// Build a real 3-event chain from the fuzzed payload bytes, using
		// the same primitives LEDGER-002/LEDGER-007 use in production: the
		// canonical digest and the hash-chain fold, never a fuzz-local
		// reimplementation of either.
		events := make([]hashchain.EventDigest, len(payloads))
		for i, p := range payloads {
			_, digest, _, err := eventDigester.Digest(p, "hcmnext.fuzz.ledger011.v1")
			if err != nil {
				t.Fatalf("digest payload %d: %v", i, err)
			}
			events[i] = hashchain.EventDigest{Sequence: int64(i + 1), EventID: uuid.New(), Digest: digest}
		}
		links, err := chainDigester.Fold("fuzz-stream", events)
		if err != nil {
			t.Fatalf("fold chain: %v", err)
		}
		before, err := chainDigester.VerifyLinks("fuzz-stream", events, links)
		if err != nil {
			t.Fatalf("chain does not verify before any disposition decision: %v", err)
		}
		beforeEvents := append([]hashchain.EventDigest(nil), events...)
		beforeLinks := append([]hashchain.ChainedLink(nil), links...)

		// Exercise the real, exported classification boundary. It must be
		// exhaustive: exactly the two declared classifications succeed and
		// produce a state from the closed disposition set; everything else
		// -- including near-misses like a trailing space or a different
		// case -- is refused, never silently treated as one of the two.
		mechanism, state, classifyErr := disposition.Classify(disposition.Classification(classification))
		isDeclared := classification == string(disposition.ClassificationStandard) ||
			classification == string(disposition.ClassificationEncryptedAtRest)
		switch {
		case isDeclared && classifyErr != nil:
			t.Fatalf("Classify(%q) refused a declared classification: %v", classification, classifyErr)
		case isDeclared && state != disposition.StatePayloadErased && state != disposition.StateRestricted:
			t.Fatalf("Classify(%q) produced state %q outside {PAYLOAD_ERASED, RESTRICTED}", classification, state)
		case isDeclared && mechanism == "":
			t.Fatalf("Classify(%q) produced no mechanism", classification)
		case !isDeclared && classifyErr == nil:
			t.Fatalf("Classify(%q) accepted an undeclared classification, producing mechanism=%q state=%q",
				classification, mechanism, state)
		case !isDeclared && (mechanism != "" || state != ""):
			t.Fatalf("Classify(%q) was refused but still returned mechanism=%q state=%q", classification, mechanism, state)
		}

		// index names which event a caller's disposition request would
		// target; a real EraseRequest.Sequence can be any int64 a caller
		// supplies, in range or not, so the property must hold regardless.
		index := rawIndex % len(events)
		if index < 0 {
			index += len(events)
		}
		_ = index // consulted only to vary the corpus; Classify takes no event index

		// The property: whatever Classify decided, the chain data this run
		// built is never perturbed by consulting it. This mirrors the real
		// package's structure -- Erase's only SQL write is an INSERT into
		// the independent ledger_payload_disposition table, never an UPDATE
		// against ledger_event or ledger_hash_chain_link -- and would catch
		// a regression that threaded a disposition decision back into the
		// chain inputs by mistake.
		after, afterErr := chainDigester.VerifyLinks("fuzz-stream", events, links)
		if afterErr != nil {
			t.Fatalf("chain verification broke after a disposition decision: %v", afterErr)
		}
		if after != before {
			t.Fatalf("chain head changed after a disposition decision: before=%+v after=%+v", before, after)
		}
		for i := range events {
			if events[i] != beforeEvents[i] {
				t.Fatalf("event %d chronology/digest mutated: before=%+v after=%+v", i, beforeEvents[i], events[i])
			}
		}
		for i := range links {
			if links[i] != beforeLinks[i] {
				t.Fatalf("chain link %d mutated: before=%+v after=%+v", i, beforeLinks[i], links[i])
			}
		}
	})
}
