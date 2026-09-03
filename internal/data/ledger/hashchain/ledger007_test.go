package hashchain_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
)

// These focused tests mirror the LEDGER-007 registry matrix.  DATA-004 owns
// the database fixture tests; this file pins the reusable, pure verifier
// contract and the security boundary around stream identity.
func TestTodo_LEDGER_007(t *testing.T) {
	d, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	digester := hashchain.NewDigester(d)
	events := []hashchain.EventDigest{
		{Sequence: 1, EventID: uuid.New(), Digest: ledger007Digest("one")},
		{Sequence: 2, EventID: uuid.New(), Digest: ledger007Digest("two")},
	}
	links, err := digester.Fold("s", events)
	if err != nil {
		t.Fatal(err)
	}
	head, err := digester.VerifyLinks("s", events, links)
	if err != nil || head.Sequence != 2 || head.ChainHash != links[1].ChainHash {
		t.Fatalf("intact chain head=%+v err=%v", head, err)
	}

	links[0].StreamKey = "other-stream"
	var broken hashchain.ErrChainBroken
	if _, err := digester.VerifyLinks("s", events, links); !errors.As(err, &broken) || broken.Sequence != 1 {
		t.Fatalf("cross-stream link err=%v, want ErrChainBroken at sequence 1", err)
	}
}

func TestTodo_LEDGER_007_Golden(t *testing.T) {
	r, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	d := hashchain.NewDigester(r)
	events := []hashchain.EventDigest{{Sequence: 1, EventID: uuid.Nil, Digest: ledger007Digest("golden")}}
	links, err := d.Fold("golden", events)
	if err != nil {
		t.Fatal(err)
	}
	if links[0].Algorithm != "sha256" || links[0].PrevHash != hashchain.GenesisHash {
		t.Fatalf("unexpected golden link: %+v", links[0])
	}
	if again, _ := d.Fold("golden", events); again[0].ChainHash != links[0].ChainHash {
		t.Fatal("same input did not reproduce the same chain hash")
	}
}

func TestTodo_LEDGER_007_Race(t *testing.T) {
	r, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	d := hashchain.NewDigester(r)
	events := []hashchain.EventDigest{{Sequence: 1, EventID: uuid.Nil, Digest: ledger007Digest("parallel")}}
	want, err := d.Fold("s", events)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, e := d.Fold("s", events)
			if e != nil || got[0].ChainHash != want[0].ChainHash {
				t.Errorf("concurrent fold got=%v err=%v", got, e)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_LEDGER_007_Security(t *testing.T) {
	r, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	d := hashchain.NewDigester(r)
	events := []hashchain.EventDigest{{Sequence: 1, EventID: uuid.New(), Digest: ledger007Digest("security")}}
	links, err := d.Fold("s", events)
	if err != nil {
		t.Fatal(err)
	}
	// Empty stream identity is not a wildcard and must not verify.
	links[0].StreamKey = ""
	if _, err := d.VerifyLinks("s", events, links); err == nil {
		t.Fatal("link with omitted stream identity verified")
	}
}

func TestTodo_LEDGER_007_Mutation(t *testing.T) {
	r, err := hashchain.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	d := hashchain.NewDigester(r)
	if _, _, err := d.Link(hashchain.GenesisHash, ""); err == nil {
		t.Fatal("empty event digest was accepted")
	}
	if _, err := d.Fold("s", []hashchain.EventDigest{{Sequence: 2, Digest: ledger007Digest("gap")}}); err == nil {
		t.Fatal("sequence gap was accepted")
	}
}

func ledger007Digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
