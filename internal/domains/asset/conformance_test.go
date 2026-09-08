package asset

import (
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func conformanceStore(t *testing.T) (*CustodyStore, InventoryRevision, values.EntityRef, time.Time) {
	t.Helper()
	s := NewCustodyStore()
	at := time.Unix(100, 0).UTC()
	i := InventoryRevision{InventoryID: assetRef("asset", "conformance"), Owner: assetRef("organization", "owner"), Classification: "LAPTOP", SerialNumber: "S-CONFORMANCE", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := s.Register(i); err != nil {
		t.Fatal(err)
	}
	return s, i, assetRef("worker", "worker"), at
}

func TestAssetConformanceTracksLossOffboardingRecoveryAndProviderDrift(t *testing.T) {
	s, inv, worker, at := conformanceStore(t)
	if err := s.Assign(inv.InventoryID, worker, "HQ", "GOOD", assetReceipt(t, "assign-a", true), assetReceipt(t, "assign-i", true), values.UnspecifiedRevision(), assetRev(t, "custody", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportLost(inv.InventoryID, worker, "HQ", "MISSING", assetRev(t, "custody", 1), assetRev(t, "custody", 2), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	o, err := s.OpenOffboarding(worker, []values.EntityRef{inv.InventoryID}, at.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !o.ClosedAt.IsZero() {
		t.Fatal("offboarding closed before return")
	}
	if err := s.CloseOffboarding(worker, at.Add(4*time.Minute)); !errors.Is(err, ErrOffboardingOpen) {
		t.Fatalf("close with open return=%v", err)
	}
	if open, ok := s.Offboarding(worker); !ok || !open.ClosedAt.IsZero() || len(open.Assets) != 1 {
		t.Fatalf("open obligation was lost: %+v ok=%v", open, ok)
	}
	recoveryA, recoveryI := assetReceipt(t, "recovery-a", true), assetReceipt(t, "recovery-i", true)
	if _, err := s.RecoverAsset("recovery-1", inv.InventoryID, "HQ", "GOOD", recoveryA, recoveryI, assetRev(t, "custody", 2), assetRev(t, "custody", 3), at.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RecoverAsset("recovery-1", inv.InventoryID, "HQ", "GOOD", recoveryA, recoveryI, assetRev(t, "custody", 2), assetRev(t, "custody", 3), at.Add(5*time.Minute)); err != nil || !got.Applied || len(s.History(inv.InventoryID)) != 3 {
		t.Fatalf("retry recovery got=%+v err=%v history=%d", got, err, len(s.History(inv.InventoryID)))
	}
	if err := s.CloseOffboarding(worker, at.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if closed, ok := s.Offboarding(worker); !ok || closed.ClosedAt.IsZero() {
		t.Fatalf("closure not recorded: %+v ok=%v", closed, ok)
	}
	drift, found, err := s.ReconcileExternal(ExternalObservation{Asset: inv.InventoryID, Provider: "mdm", ObservedStatus: Lost, ObservedAt: at.Add(7 * time.Minute), ObservationID: "mdm-1"})
	if err != nil || !found || drift.Authority != Returned || drift.Observed != Lost {
		t.Fatalf("drift=%+v found=%v err=%v", drift, found, err)
	}
	current, ok := s.Current(inv.InventoryID)
	if !ok || current.Status != Returned {
		t.Fatalf("provider observation changed authority: %+v", current)
	}
}

func TestTodo_ASSET_002_Property(t *testing.T) {
	s, i, w, at := conformanceStore(t)
	if err := s.Assign(i.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "p-a", true), assetReceipt(t, "p-i", true), values.UnspecifiedRevision(), assetRev(t, "custody", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportLost(i.InventoryID, w, "HQ", "MISSING", assetRev(t, "custody", 1), assetRev(t, "custody", 2), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportLost(i.InventoryID, w, "HQ", "MISSING", assetRev(t, "custody", 2), assetRev(t, "custody", 3), at.Add(time.Minute)); !errors.Is(err, ErrChronology) {
		t.Fatalf("chronology regression=%v", err)
	}
}
func TestTodo_ASSET_002_Golden(t *testing.T) {
	_, i, _, at := conformanceStore(t)
	event := CustodyRevision{Asset: i.InventoryID, Location: "HQ", Condition: "RECOVERED", AssigneeReceipt: assetReceipt(t, "golden-a", true), IssuerReceipt: assetReceipt(t, "golden-i", true), Revision: assetRev(t, "custody", 3), EffectiveAt: at.Add(5 * time.Minute), Status: Returned}
	const want = "0724736368656d612568636d6e6578742e646f6d61696e732e61737365742e437573746f64795265766973696f6e0f24736368656d615f76657273696f6e01020561737365743f657265663a76313a74656e616e742d61737365743a61737365743a36633164366365642d663237362d656133662d643734382d36613231326664363262333206776f726b65720100086c6f636174696f6e02485109636f6e646974696f6e095245434f56455245441061737369676e65655f726563656970748202010724736368656d611d68636d6e6578742e646f6d61696e732e61737365742e526563656970740f24736368656d615f76657273696f6e010202696441657265663a76313a74656e616e742d61737365743a726563656970743a64313935346139372d613435652d303361352d303166382d3965623034336132643034360669737375657240657265663a76313a74656e616e742d61737365743a73797374656d3a35333563366638652d623531312d663564392d363661312d623037323564663932656266096973737565645f617414313937302d30312d30315430303a30303a30315a08766572696669656401010865766964656e63650865766964656e63650e6973737565725f726563656970748202010724736368656d611d68636d6e6578742e646f6d61696e732e61737365742e526563656970740f24736368656d615f76657273696f6e010202696441657265663a76313a74656e616e742d61737365743a726563656970743a38663361623733352d363839612d353430322d346364612d3366306633343139643663640669737375657240657265663a76313a74656e616e742d61737365743a73797374656d3a35333563366638652d623531312d663564392d363661312d623037323564663932656266096973737565645f617414313937302d30312d30315430303a30303a30315a08766572696669656401010865766964656e63650865766964656e6365087265766973696f6e117265763a76313a637573746f64793a73330c6566666563746976655f617414313937302d30312d30315430303a30363a34305a067374617475730852455455524e4544"
	if got := hex.EncodeToString(event.Canonical()); got != want {
		t.Fatalf("canonical bytes changed\n got: %s\nwant: %s", got, want)
	}
}
func TestTodo_ASSET_002_Race(t *testing.T) {
	s, i, w, at := conformanceStore(t)
	if err := s.Assign(i.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "ra", true), assetReceipt(t, "ri", true), values.UnspecifiedRevision(), assetRev(t, "race", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportLost(i.InventoryID, w, "HQ", "MISSING", assetRev(t, "race", 1), assetRev(t, "race", 2), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	a, issuer := assetReceipt(t, "recover-a", true), assetReceipt(t, "recover-i", true)
	done := make(chan error, 2)
	for n := 0; n < 2; n++ {
		go func() {
			_, err := s.RecoverAsset("same-recovery", i.InventoryID, "HQ", "GOOD", a, issuer, assetRev(t, "race", 2), assetRev(t, "race", 3), at.Add(3*time.Minute))
			done <- err
		}()
	}
	e1, e2 := <-done, <-done
	if e1 != nil || e2 != nil || len(s.History(i.InventoryID)) != 3 {
		t.Fatalf("concurrent retry was not one successful append: %v, %v, history=%d", e1, e2, len(s.History(i.InventoryID)))
	}
}
func TestTodo_ASSET_002_Fault(t *testing.T) {
	s, i, w, at := conformanceStore(t)
	if _, err := s.RecoverAsset("", i.InventoryID, "HQ", "GOOD", assetReceipt(t, "fa", true), assetReceipt(t, "fi", true), values.UnspecifiedRevision(), assetRev(t, "fault", 1), at.Add(time.Minute)); !errors.Is(err, ErrInvalidAsset) {
		t.Fatalf("empty operation=%v", err)
	}
	if err := s.Assign(i.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "fa", true), assetReceipt(t, "fi", true), values.UnspecifiedRevision(), assetRev(t, "fault", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportLost(i.InventoryID, w, "HQ", "MISSING", assetRev(t, "fault", 1), assetRev(t, "fault", 2), at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	a, issuer := assetReceipt(t, "recover-a", true), assetReceipt(t, "recover-i", true)
	if _, err := s.RecoverAsset("fault-recovery", i.InventoryID, "HQ", "GOOD", a, issuer, assetRev(t, "fault", 2), assetRev(t, "fault", 3), at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecoverAsset("fault-recovery", i.InventoryID, "OTHER", "GOOD", a, issuer, assetRev(t, "fault", 2), assetRev(t, "fault", 3), at.Add(3*time.Minute)); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("altered retry payload=%v", err)
	}
}
func TestTodo_ASSET_002_Security(t *testing.T) {
	s, i, w, at := conformanceStore(t)
	_, _, err := s.ReconcileExternal(ExternalObservation{Asset: i.InventoryID, Provider: "mdm", ObservedStatus: Assigned, ObservedAt: at.Add(time.Minute), ObservationID: "sec"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Current(i.InventoryID); ok {
		t.Fatal("external inventory observation created physical custody")
	}
	other := w
	other.Tenant = "tenant-other"
	if err := s.ReportLost(i.InventoryID, other, "HQ", "MISSING", values.UnspecifiedRevision(), assetRev(t, "security", 1), at.Add(2*time.Minute)); !errors.Is(err, ErrTenantBoundary) {
		t.Fatalf("cross-tenant loss=%v", err)
	}
}
func TestTodo_ASSET_002_Conformance(t *testing.T) {
	s, i, w, at := conformanceStore(t)
	if err := s.Assign(i.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "ca", true), assetReceipt(t, "ci", true), values.UnspecifiedRevision(), assetRev(t, "conformance", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenOffboarding(w, []values.EntityRef{i.InventoryID}, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenOffboarding(w, []values.EntityRef{i.InventoryID}, at.Add(3*time.Minute)); !errors.Is(err, ErrOffboardingOpen) {
		t.Fatalf("open obligation overwritten=%v", err)
	}
	if err := s.CloseOffboarding(w, at); !errors.Is(err, ErrOffboardingOpen) {
		t.Fatalf("backdated closure=%v", err)
	}
}

func TestAssetOffboardingAndCustodyAttributionResistCallerMutation(t *testing.T) {
	s, inventory, worker, at := conformanceStore(t)
	otherWorker := assetRef("worker", "other-worker")
	if err := s.Apply(inventory.InventoryID, values.UnspecifiedRevision(), CustodyRevision{Asset: inventory.InventoryID, Worker: worker, Location: "HQ", Condition: "MISSING", Revision: assetRev(t, "initial", 1), EffectiveAt: at.Add(time.Minute), Status: Lost}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("loss without custody history=%v", err)
	}
	if err := s.Apply(inventory.InventoryID, values.UnspecifiedRevision(), CustodyRevision{Asset: inventory.InventoryID, Location: "HQ", Condition: "FOUND", AssigneeReceipt: assetReceipt(t, "initial-ra", true), IssuerReceipt: assetReceipt(t, "initial-ri", true), Revision: assetRev(t, "initial", 1), EffectiveAt: at.Add(time.Minute), Status: Returned}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("return without custody history=%v", err)
	}
	if err := s.Assign(inventory.InventoryID, worker, "HQ", "GOOD", assetReceipt(t, "attr-a", true), assetReceipt(t, "attr-i", true), values.UnspecifiedRevision(), assetRev(t, "attribution", 1), at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportLost(inventory.InventoryID, otherWorker, "HQ", "MISSING", assetRev(t, "attribution", 1), assetRev(t, "attribution", 2), at.Add(2*time.Minute)); !errors.Is(err, ErrNotAssigned) {
		t.Fatalf("loss attributed to another worker=%v", err)
	}
	if err := s.BeginReturn(inventory.InventoryID, otherWorker, "HQ", "GOOD", assetRev(t, "attribution", 1), assetRev(t, "attribution", 2), at.Add(2*time.Minute)); !errors.Is(err, ErrNotAssigned) {
		t.Fatalf("return attributed to another worker=%v", err)
	}
	if _, err := s.OpenOffboarding(otherWorker, []values.EntityRef{inventory.InventoryID}, at.Add(2*time.Minute)); !errors.Is(err, ErrNotAssigned) {
		t.Fatalf("other worker claimed asset obligation=%v", err)
	}
	assets := []values.EntityRef{inventory.InventoryID}
	obligation, err := s.OpenOffboarding(worker, assets, at.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	assets[0] = assetRef("asset", "caller-replacement")
	obligation.Assets[0] = assetRef("asset", "returned-replacement")
	stored, ok := s.Offboarding(worker)
	if !ok || len(stored.Assets) != 1 || stored.Assets[0] != inventory.InventoryID {
		t.Fatalf("caller mutated mandatory asset set: %+v", stored)
	}
	if err := s.CloseOffboarding(worker, at.Add(3*time.Minute)); !errors.Is(err, ErrOffboardingOpen) {
		t.Fatalf("mutated output bypassed open obligation=%v", err)
	}
}
func TestTodo_ASSET_002_Mutation(t *testing.T) {
	s, i, w, at := conformanceStore(t)
	_ = s.Assign(i.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "ma", true), assetReceipt(t, "mi", true), values.UnspecifiedRevision(), assetRev(t, "mutation", 1), at.Add(time.Minute))
	h := s.History(i.InventoryID)
	h[0].Status = Lost
	h2 := s.History(i.InventoryID)
	if h2[0].Status != Assigned {
		t.Fatal("history mutation altered store")
	}
}
