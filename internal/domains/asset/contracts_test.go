package asset

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func assetRef(kind, id string) values.EntityRef {
	sum := sha256.Sum256([]byte(id))
	raw := hex.EncodeToString(sum[:16])
	canonicalID := raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	return values.EntityRef{Tenant: "tenant-asset", Kind: values.Kind(kind), Id: canonicalID}
}
func assetRev(t *testing.T, stream string, n uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision(stream, n)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func assetReceipt(t *testing.T, id string, verified bool) Receipt {
	t.Helper()
	return Receipt{ID: assetRef("receipt", id), Issuer: assetRef("system", "issuer"), IssuedAt: time.Unix(1, 0).UTC(), Verified: verified, Evidence: "evidence"}
}

func TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t *testing.T) {
	s := NewCustodyStore()
	at := time.Unix(10, 0).UTC()
	inv := InventoryRevision{InventoryID: assetRef("asset", "laptop-1"), Owner: assetRef("organization", "org"), Classification: "LAPTOP", SerialNumber: "SN-1", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := s.Register(inv); err != nil {
		t.Fatal(err)
	}
	w := assetRef("worker", "worker-1")
	if err := s.Assign(inv.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "a", true), assetReceipt(t, "i", true), values.UnspecifiedRevision(), assetRev(t, "custody", 1), at); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign(inv.InventoryID, assetRef("worker", "worker-2"), "HQ", "GOOD", assetReceipt(t, "b", true), assetReceipt(t, "j", true), assetRev(t, "custody", 1), assetRev(t, "custody", 2), at); !errors.Is(err, ErrAlreadyAssigned) {
		t.Fatalf("double assignment err=%v", err)
	}
	if err := s.Assign(assetRef("asset", "missing"), w, "HQ", "GOOD", assetReceipt(t, "c", true), assetReceipt(t, "k", true), values.UnspecifiedRevision(), assetRev(t, "custody", 1), at); !errors.Is(err, ErrUnknownInventory) {
		t.Fatalf("unknown inventory err=%v", err)
	}
	if err := s.CompleteReturn(inv.InventoryID, "HQ", "GOOD", assetReceipt(t, "r", false), assetReceipt(t, "ri", true), assetRev(t, "custody", 1), assetRev(t, "custody", 3), at); !errors.Is(err, ErrUnverifiedReturn) {
		t.Fatalf("unverified return err=%v", err)
	}
}

func TestTodo_ASSET_001_Property(t *testing.T) {
	for n := 1; n <= 32; n++ {
		s := NewCustodyStore()
		at := time.Unix(int64(100+n), 0).UTC()
		id := fmt.Sprintf("property-%d", n)
		inv := InventoryRevision{InventoryID: assetRef("asset", id), Owner: assetRef("organization", "org-"+id), Classification: "LAPTOP", SerialNumber: "SN-" + id, Revision: assetRev(t, "inventory-"+id, 1), EffectiveAt: at, Status: Available}
		if err := s.Register(inv); err != nil {
			t.Fatalf("case %d register: %v", n, err)
		}
		worker := assetRef("worker", "worker-"+id)
		first := assetRev(t, "custody-"+id, uint64(n))
		if err := s.Assign(inv.InventoryID, worker, "HQ", "GOOD", assetReceipt(t, "a-"+id, true), assetReceipt(t, "i-"+id, true), values.UnspecifiedRevision(), first, at.Add(time.Minute)); err != nil {
			t.Fatalf("case %d assign: %v", n, err)
		}
		wrongExpected := assetRev(t, "custody-"+id, uint64(n+100))
		if err := s.BeginReturn(inv.InventoryID, worker, "HQ", "GOOD", wrongExpected, assetRev(t, "custody-"+id, uint64(n+1)), at.Add(2*time.Minute)); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("case %d stale CAS=%v", n, err)
		}
		history := s.History(inv.InventoryID)
		if len(history) != 1 || history[0].Status != Assigned || history[0].Worker != worker || !history[0].Revision.Equal(first) {
			t.Fatalf("case %d failed mutation changed authority: %+v", n, history)
		}
	}
}
func TestTodo_ASSET_001_Golden(t *testing.T) {
	at := time.Unix(20, 0).UTC()
	i := InventoryRevision{InventoryID: assetRef("asset", "golden"), Owner: values.EntityRef{Tenant: "tenant-asset", Kind: "organization", Id: "4c102969-7ee3-5871-5d38-1b03416ec15f"}, Classification: "LAPTOP", SerialNumber: "SN", Revision: assetRev(t, "golden-inventory", 1), EffectiveAt: at, Status: Available}
	c := CustodyRevision{Asset: i.InventoryID, Worker: assetRef("worker", "golden-worker"), Location: "HQ", Condition: "GOOD", AssigneeReceipt: assetReceipt(t, "golden-a", true), IssuerReceipt: assetReceipt(t, "golden-i", true), Revision: assetRev(t, "golden-custody", 1), EffectiveAt: at.Add(time.Minute), Status: Assigned}
	const wantInventory = "0724736368656d612768636d6e6578742e646f6d61696e732e61737365742e496e76656e746f72795265766973696f6e0f24736368656d615f76657273696f6e01020c696e76656e746f72795f69643f657265663a76313a74656e616e742d61737365743a61737365743a64643536646534312d333739352d316439632d393236382d316230333431366563313566056f776e657246657265663a76313a74656e616e742d61737365743a6f7267616e697a6174696f6e3a34633130323936392d376565332d353837312d356433382d3162303334313665633135660e636c617373696669636174696f6e064c4150544f500d73657269616c5f6e756d62657202534e087265766973696f6e1a7265763a76313a676f6c64656e2d696e76656e746f72793a73310c6566666563746976655f617414313937302d30312d30315430303a30303a32305a0673746174757309415641494c41424c45"
	const wantCustody = "0724736368656d612568636d6e6578742e646f6d61696e732e61737365742e437573746f64795265766973696f6e0f24736368656d615f76657273696f6e01020561737365743f657265663a76313a74656e616e742d61737365743a61737365743a64643536646534312d333739352d316439632d393236382d31623033343136656331356606776f726b65724101657265663a76313a74656e616e742d61737365743a776f726b65723a66316431313361622d346236392d346561382d313762662d666164353066346562353430" +
		"086c6f636174696f6e02485109636f6e646974696f6e04474f4f441061737369676e65655f726563656970748202010724736368656d611d68636d6e6578742e646f6d61696e732e61737365742e526563656970740f24736368656d615f76657273696f6e010202696441657265663a76313a74656e616e742d61737365743a726563656970743a64313935346139372d613435652d303361352d303166382d3965623034336132643034360669737375657240657265663a76313a74656e616e742d61737365743a73797374656d3a35333563366638652d623531312d663564392d363661312d623037323564663932656266096973737565645f617414313937302d30312d30315430303a30303a30315a" +
		"08766572696669656401010865766964656e63650865766964656e63650e6973737565725f726563656970748202010724736368656d611d68636d6e6578742e646f6d61696e732e61737365742e526563656970740f24736368656d615f76657273696f6e010202696441657265663a76313a74656e616e742d61737365743a726563656970743a38663361623733352d363839612d353430322d346364612d3366306633343139643663640669737375657240657265663a76313a74656e616e742d61737365743a73797374656d3a35333563366638652d623531312d663564392d363661312d623037323564663932656266096973737565645f617414313937302d30312d30315430303a30303a30315a08766572696669656401010865766964656e63650865766964656e6365087265766973696f6e187265763a76313a676f6c64656e2d637573746f64793a73310c6566666563746976655f617414313937302d30312d30315430303a30313a32305a067374617475730841535349474e4544"
	if got := hex.EncodeToString(i.Canonical()); got != wantInventory {
		t.Fatalf("inventory canonical bytes changed\n got: %s\nwant: %s", got, wantInventory)
	}
	if got := hex.EncodeToString(c.Canonical()); got != wantCustody {
		t.Fatalf("custody canonical bytes changed\n got: %s\nwant: %s", got, wantCustody)
	}
}
func TestTodo_ASSET_001_Race(t *testing.T) {
	s := NewCustodyStore()
	at := time.Unix(10, 0).UTC()
	inv := InventoryRevision{InventoryID: assetRef("asset", "race"), Owner: assetRef("organization", "org"), Classification: "LAPTOP", SerialNumber: "SN", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := s.Register(inv); err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		worker   values.EntityRef
		revision values.RevisionToken
		at       time.Time
		err      error
	}
	const contenders = 16
	ready := make([]attempt, contenders)
	done := make(chan attempt, contenders)
	for n := range ready {
		ready[n] = attempt{worker: assetRef("worker", fmt.Sprintf("race-worker-%d", n)), revision: assetRev(t, "race", uint64(n+1)), at: at.Add(time.Duration(n+1) * time.Minute)}
	}
	for _, contender := range ready {
		go func(a attempt) {
			a.err = s.Assign(inv.InventoryID, a.worker, "HQ", "GOOD", assetReceipt(t, "ra", true), assetReceipt(t, "ri", true), values.UnspecifiedRevision(), a.revision, a.at)
			done <- a
		}(contender)
	}
	var winner attempt
	wins := 0
	for range contenders {
		result := <-done
		if result.err == nil {
			winner, wins = result, wins+1
		} else if !errors.Is(result.err, ErrRevisionConflict) && !errors.Is(result.err, ErrAlreadyAssigned) {
			t.Fatalf("unexpected loser error=%v", result.err)
		}
	}
	history := s.History(inv.InventoryID)
	current, ok := s.Current(inv.InventoryID)
	if wins != 1 || len(history) != 1 || !ok || current.Worker != winner.worker || !current.Revision.Equal(winner.revision) || current != history[0] {
		t.Fatalf("wins=%d winner=%+v current=%+v ok=%v history=%+v", wins, winner, current, ok, history)
	}
}
func TestTodo_ASSET_001_Fault(t *testing.T) {
	s := NewCustodyStore()
	if err := s.Register(InventoryRevision{}); err == nil {
		t.Fatalf("invalid inventory=%v", err)
	}
	if err := (CustodyRevision{Asset: assetRef("asset", "fault"), Location: "HQ", Condition: "GOOD", Revision: assetRev(t, "fault", 1), EffectiveAt: time.Unix(1, 0), Status: Assigned}).Validate(); err == nil {
		t.Fatal("assigned custody without worker accepted")
	}
}
func TestTodo_ASSET_001_Security(t *testing.T) {
	s := NewCustodyStore()
	at := time.Unix(10, 0).UTC()
	inv := InventoryRevision{InventoryID: assetRef("asset", "security"), Owner: assetRef("organization", "org"), Classification: "LAPTOP", SerialNumber: "SN", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := s.Register(inv); err != nil {
		t.Fatal(err)
	}
	foreign := values.EntityRef{Tenant: "other-tenant", Kind: values.Kind("worker"), Id: assetRef("worker", "foreign").Id}
	if err := s.Assign(inv.InventoryID, foreign, "HQ", "GOOD", assetReceipt(t, "sa", true), assetReceipt(t, "si", true), values.UnspecifiedRevision(), assetRev(t, "security", 1), at.Add(time.Minute)); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("cross-tenant assignment=%v", err)
	}
}
func TestTodo_ASSET_001_Conformance(t *testing.T) {
	var repository Repository = NewCustodyStore()
	at := time.Unix(30, 0).UTC()
	inventory := InventoryRevision{InventoryID: assetRef("asset", "conformance-001"), Owner: assetRef("organization", "owner-001"), Classification: "BADGE", SerialNumber: "B-1", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := repository.RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	event := CustodyRevision{Asset: inventory.InventoryID, Worker: assetRef("worker", "conformance-worker"), Location: "HQ", Condition: "NEW", AssigneeReceipt: assetReceipt(t, "conf-a", true), IssuerReceipt: assetReceipt(t, "conf-i", true), Revision: assetRev(t, "custody", 1), EffectiveAt: at.Add(time.Minute), Status: Assigned}
	if err := repository.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), event); err != nil {
		t.Fatal(err)
	}
	current, ok, err := repository.CurrentCustody(inventory.InventoryID)
	history, historyErr := repository.HistoryCustody(inventory.InventoryID)
	if err != nil || historyErr != nil || !ok || current != event || len(history) != 1 || history[0] != event {
		t.Fatalf("repository current=%+v ok=%v err=%v history=%+v historyErr=%v", current, ok, err, history, historyErr)
	}
	if err := repository.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), event); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("repository CAS semantics=%v", err)
	}
}
func TestTodo_ASSET_001_Mutation(t *testing.T) {
	s := NewCustodyStore()
	at := time.Unix(10, 0).UTC()
	inv := InventoryRevision{InventoryID: assetRef("asset", "mutation"), Owner: assetRef("organization", "org"), Classification: "LAPTOP", SerialNumber: "SN", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := s.Register(inv); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign(inv.InventoryID, assetRef("worker", "m"), "HQ", "GOOD", assetReceipt(t, "ma", true), assetReceipt(t, "mi", true), values.UnspecifiedRevision(), assetRev(t, "mutation", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	h := s.History(inv.InventoryID)
	h[0].Status = Lost
	h = append(h, CustodyRevision{Status: Retired})
	current, _ := s.Current(inv.InventoryID)
	stored := s.History(inv.InventoryID)
	if current.Status != Assigned || len(stored) != 1 || stored[0].Status != Assigned {
		t.Fatalf("mutating returned history changed state: current=%+v history=%+v", current, stored)
	}
}
