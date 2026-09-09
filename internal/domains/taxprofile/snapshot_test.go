package taxprofile

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

const (
	formReleaseDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	ruleReleaseDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func snapshotFixture(t *testing.T) PinnedTaxInputSnapshot {
	t.Helper()
	p := validTaxProfile(t)
	s, err := BuildTaxProfileSnapshot(TaxProfileSnapshotRequest{Profile: p, EmploymentRef: "employment-1", PayGroupRef: "monthly", EffectiveAsOf: taxInstant(t, "2026-06-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-06-02T00:00:00Z"), FormReleaseDigest: formReleaseDigest, RuleReleaseDigest: ruleReleaseDigest, RegistrationPresence: PresencePresent, ElectionPresence: PresencePresent, ClassificationPresence: PresencePresent})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTaxProfileSnapshotFeedsPinnedTaxCalculationAndPayrollRun(t *testing.T) {
	s := snapshotFixture(t)
	if err := TaxAndPayrollConformance(s); err != nil {
		t.Fatal(err)
	}
	if s.ProfileDigest == "" || s.Digest == "" || s.FormReleaseDigest == "" || s.RuleReleaseDigest == "" {
		t.Fatal("snapshot is not fully pinned")
	}
	if s.RegistrationState != PresencePresent || s.ElectionState != PresencePresent {
		t.Fatal("present inputs lost their state")
	}
}

func TestTodo_TAXPROFILE_003_Property(t *testing.T) {
	states := []PresenceState{PresencePresent, PresenceUnknown, PresenceRedacted}
	for _, registration := range states {
		for _, election := range states {
			for _, classification := range states {
				p := validTaxProfile(t)
				s, buildErr := BuildTaxProfileSnapshot(TaxProfileSnapshotRequest{Profile: p, EmploymentRef: "employment-1", PayGroupRef: "monthly", EffectiveAsOf: taxInstant(t, "2026-06-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-06-02T00:00:00Z"), FormReleaseDigest: formReleaseDigest, RuleReleaseDigest: ruleReleaseDigest, RegistrationPresence: registration, ElectionPresence: election, ClassificationPresence: classification})
				if buildErr != nil {
					t.Fatal(buildErr)
				}
				wantUnsafe := registration != PresencePresent || election != PresencePresent || classification != PresencePresent
				if s.Unsafe() != wantUnsafe {
					t.Fatalf("states %s/%s/%s unsafe=%v", registration, election, classification, s.Unsafe())
				}
				err := TaxAndPayrollConformance(s)
				if wantUnsafe != errors.Is(err, ErrSnapshotUnsafe) {
					t.Fatalf("states %s/%s/%s err=%v", registration, election, classification, err)
				}
			}
		}
	}
}

func TestTodo_TAXPROFILE_003_Golden(t *testing.T) {
	s := snapshotFixture(t)
	canonical := s.canonical()
	if len(canonical) != 790 || canonicalbytes.Digest(canonical) != "sha256:7b7397044956478e909def69171f1af8ededd84ccbab8381b5137c6166060985" {
		t.Fatalf("canonical length=%d digest=%s", len(canonical), canonicalbytes.Digest(canonical))
	}
}

func TestTodo_TAXPROFILE_003_Race(t *testing.T) {
	s := snapshotFixture(t)
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- TaxCalculationConformance(s)
			results <- PayrollTaxConformance(s)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent consumer: %v", err)
		}
	}
}

func TestTodo_TAXPROFILE_003_Fault(t *testing.T) {
	p := validTaxProfile(t)
	if _, err := BuildTaxProfileSnapshot(TaxProfileSnapshotRequest{Profile: p, EmploymentRef: "employment-1", PayGroupRef: "monthly", EffectiveAsOf: taxInstant(t, "2026-06-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-06-02T00:00:00Z"), FormReleaseDigest: "form", RuleReleaseDigest: ruleReleaseDigest, RegistrationPresence: PresenceUnknown, ElectionPresence: PresencePresent}); !errors.Is(err, ErrSnapshotInvalid) {
		t.Fatalf("malformed release digest err=%v", err)
	}
	s := snapshotFixture(t)
	s.RegistrationState = PresenceUnknown
	s.Digest = "tampered"
	if !errors.Is(TaxCalculationConformance(s), ErrSnapshotInvalid) {
		t.Fatal("unknown state or digest accepted")
	}
}

func TestTodo_TAXPROFILE_003_Security(t *testing.T) {
	s := snapshotFixture(t)
	s.ElectionState = PresenceRedacted
	s.Digest = ""
	if !errors.Is(PayrollTaxConformance(s), ErrSnapshotInvalid) {
		t.Fatal("redacted snapshot bypassed integrity validation")
	}
	p := validTaxProfile(t)
	s, err := BuildTaxProfileSnapshot(TaxProfileSnapshotRequest{Profile: p, EmploymentRef: "employment-1", PayGroupRef: "monthly", EffectiveAsOf: taxInstant(t, "2026-06-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-06-02T00:00:00Z"), FormReleaseDigest: formReleaseDigest, RuleReleaseDigest: ruleReleaseDigest, RegistrationPresence: PresencePresent, ElectionPresence: PresenceRedacted, ClassificationPresence: PresencePresent})
	if err != nil || !errors.Is(PayrollTaxConformance(s), ErrSnapshotUnsafe) {
		t.Fatalf("redacted election accepted: build=%v conformance=%v", err, PayrollTaxConformance(s))
	}
}

func TestTodo_TAXPROFILE_003_Conformance(t *testing.T) {
	s := snapshotFixture(t)
	if err := TaxCalculationConformance(s); err != nil {
		t.Fatal(err)
	}
	if err := PayrollTaxConformance(s); err != nil {
		t.Fatal(err)
	}
	if err := TaxAndPayrollConformance(s); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TAXPROFILE_003_Mutation(t *testing.T) {
	s := snapshotFixture(t)
	before := s.Digest
	s.ProfileRevision++
	if s.Digest != before || s.Validate() == nil {
		t.Fatal("frozen snapshot mutation was accepted")
	}
}

func TestTaxProfileSnapshotUsesExactKnownAtCutoff(t *testing.T) {
	p := validTaxProfile(t)
	future := p.Elections[0]
	future.ElectionID = "election-future"
	future.EvidenceRef = "evidence:future"
	future.KnownAt = taxInstant(t, "2026-07-01T00:00:00Z")
	future.CanonicalDigest = ""
	future, err := NewWithholdingElectionRevision(future)
	if err != nil {
		t.Fatal(err)
	}
	p.Elections = append(p.Elections, future)
	p.CanonicalDigest = ""
	p, err = NewWorkerTaxProfileRevision(p)
	if err != nil {
		t.Fatal(err)
	}
	s, err := BuildTaxProfileSnapshot(TaxProfileSnapshotRequest{Profile: p, EmploymentRef: "employment-1", PayGroupRef: "monthly", EffectiveAsOf: taxInstant(t, "2026-06-01T00:00:00Z"), KnownAt: taxInstant(t, "2026-06-02T00:00:00Z"), FormReleaseDigest: formReleaseDigest, RuleReleaseDigest: ruleReleaseDigest, RegistrationPresence: PresencePresent, ElectionPresence: PresencePresent, ClassificationPresence: PresencePresent})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.ElectionDigests) != 1 || s.ElectionDigests[0] != p.Elections[0].CanonicalDigest {
		t.Fatalf("known-at cutoff pinned elections %v", s.ElectionDigests)
	}
}
