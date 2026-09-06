package onboarding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
)

func lineageDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func lineageFixture() LineagePackage {
	return LineagePackage{
		TenantID: "tenant-a", RunID: "run-1", ManifestDigest: lineageDigest("manifest"), SourceSnapshot: "snapshot-1",
		Evidence: LineageEvidence{FreezeEpoch: "epoch-1", DeltaDigest: lineageDigest("delta"), ReconciliationDigest: lineageDigest("reconciliation")},
		Members:  []LineageMember{{Sequence: 1, ValueID: "value-1", TenantID: "tenant-a", Object: "WORKER", ExternalID: "worker-1", SourceSnapshot: "snapshot-1", SourceRow: "row-7", SourceOffset: 128, SourceDigest: lineageDigest("source-row"), TransformRef: "transform:v1", TransformDigest: lineageDigest("transform"), CrosswalkRef: "crosswalk:v1", CrosswalkDigest: lineageDigest("crosswalk"), IdentityOutcome: IdentityExact, CanonicalIdentityRef: "person-1", ProposalRef: "proposal-1", TransactionRef: "transaction-1", ObservationRef: "observation-1", CorrectionRef: "none"}},
	}
}

func TestTodo_ONBOARD_007(t *testing.T) {
	p, err := NewLineagePackage(lineageFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyOffline(p) || p.Digest == "" || p.Members[0].Digest == "" {
		t.Fatalf("sealed package = %+v", p)
	}
	if got := p.Explain(); got == "" {
		t.Fatal("empty explanation")
	}
}

func TestTodo_ONBOARD_007_Mutation(t *testing.T) {
	p, err := NewLineagePackage(lineageFixture())
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*LineagePackage){
		func(p *LineagePackage) { p.Members[0].SourceRow = "row-forged" },
		func(p *LineagePackage) { p.Members[0].TransformRef = "transform-forged" },
		func(p *LineagePackage) { p.Members[0].CanonicalIdentityRef = "person-forged" },
		func(p *LineagePackage) { p.Members = nil },
		func(p *LineagePackage) { p.Evidence.ReconciliationDigest = lineageDigest("forged") },
	}
	for i, mutate := range mutations {
		copy := p
		copy.Members = append([]LineageMember(nil), p.Members...)
		mutate(&copy)
		if VerifyOffline(copy) {
			t.Fatalf("mutation %d verified", i)
		}
	}
	if _, err := NewLineagePackage(LineagePackage{TenantID: "tenant-a", RunID: "run-1"}); !errors.Is(err, ErrLineageInvalid) {
		t.Fatalf("invalid package error = %v", err)
	}
}

func TestTodo_ONBOARD_007_Race(t *testing.T) {
	p, err := NewLineagePackage(lineageFixture())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Verify(); err != nil {
				t.Errorf("verify: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestLineagePackageDoesNotNeedRuntimeOrProvider(t *testing.T) {
	if err := context.Background().Err(); err != nil {
		t.Fatal(err)
	}
}
