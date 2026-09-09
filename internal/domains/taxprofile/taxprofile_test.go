package taxprofile

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func taxInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func taxInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	first := taxInstant(t, start)
	if end == "" {
		out, err := values.NewOpenInstantInterval(first)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	out, err := values.NewInstantInterval(first, taxInstant(t, end))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func validRegistration(t *testing.T, jurisdiction string) TaxRegistrationRevision {
	t.Helper()
	registration, err := NewTaxRegistrationRevision(TaxRegistrationRevision{RegistrationIDRef: "registration-ref:" + jurisdiction, Jurisdiction: jurisdiction, AuthorityRef: "authority:" + jurisdiction, Revision: 1, Effective: taxInterval(t, "2026-01-01T00:00:00Z", ""), KnownAt: taxInstant(t, "2026-01-01T01:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	return registration
}

func validElection(t *testing.T) WithholdingElectionRevision {
	t.Helper()
	election, err := NewWithholdingElectionRevision(WithholdingElectionRevision{ElectionID: "election-1", WorkerRef: "worker-1", Jurisdiction: "US-CA", Kind: ElectionStandardWithholding, FormRevisionRef: "form:w4:v2026", EvidenceRef: "evidence:election-1", Effective: taxInterval(t, "2026-01-01T00:00:00Z", ""), KnownAt: taxInstant(t, "2026-01-02T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	return election
}

func validExemption(t *testing.T) TaxExemptionRevision {
	t.Helper()
	exemption, err := NewTaxExemptionRevision(TaxExemptionRevision{ExemptionID: "exemption-1", WorkerRef: "worker-1", Jurisdiction: "US-CA", Kind: ExemptionState, EvidenceRefs: []string{"evidence:exemption-1"}, ExpiresAt: taxInstant(t, "2026-12-31T00:00:00Z"), Effective: taxInterval(t, "2026-01-01T00:00:00Z", ""), KnownAt: taxInstant(t, "2026-01-02T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	return exemption
}

func validTaxProfile(t *testing.T) WorkerTaxProfileRevision {
	t.Helper()
	registration := validRegistration(t, "US-CA")
	profile, err := NewWorkerTaxProfileRevision(WorkerTaxProfileRevision{WorkerRef: "worker-1", Revision: 1, ResidenceJurisdictions: []string{"US-CA"}, WorkJurisdictions: []string{"US-CA"}, FilingStatus: FilingSingle, Classification: ClassificationResident, ClassificationEvidenceRef: "evidence:classification-1", Registrations: []TaxRegistrationRevision{registration}, Elections: []WithholdingElectionRevision{validElection(t)}, Exemptions: []TaxExemptionRevision{validExemption(t)}, Effective: taxInterval(t, "2026-01-01T00:00:00Z", ""), KnownAt: taxInstant(t, "2026-01-03T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

// TestTaxProfileRejectsUnregisteredJurisdictionAndUnverifiedElection is the
// primary TAXPROFILE-001 acceptance case.
func TestTaxProfileRejectsUnregisteredJurisdictionAndUnverifiedElection(t *testing.T) {
	profile := validTaxProfile(t)
	if profile.CanonicalDigest == "" {
		t.Fatal("profile was not digested")
	}
	missingRegistration := profile
	missingRegistration.ResidenceJurisdictions = []string{"US-NY"}
	missingRegistration.CanonicalDigest = ""
	if _, err := NewWorkerTaxProfileRevision(missingRegistration); !errors.Is(err, ErrUnregisteredJurisdiction) {
		t.Fatalf("unregistered jurisdiction error = %v", err)
	}
	missingForm := profile
	missingForm.Elections = append([]WithholdingElectionRevision(nil), profile.Elections...)
	missingForm.Elections[0].FormRevisionRef = ""
	missingForm.CanonicalDigest = ""
	if _, err := NewWorkerTaxProfileRevision(missingForm); !errors.Is(err, ErrInvalidElection) {
		t.Fatalf("unverified election error = %v", err)
	}
}

func TestTodo_TAXPROFILE_001_Property(t *testing.T) {
	profile := validTaxProfile(t)
	if err := profile.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Explain(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TAXPROFILE_001_Golden(t *testing.T) {
	first := validTaxProfile(t)
	second := validTaxProfile(t)
	if first.CanonicalDigest != second.CanonicalDigest {
		t.Fatalf("digest drift: %s != %s", first.CanonicalDigest, second.CanonicalDigest)
	}
}

func TestTodo_TAXPROFILE_001_Race(t *testing.T) {
	profile := validTaxProfile(t)
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() { defer wait.Done(); _, _ = profile.Explain() }()
	}
	wait.Wait()
}

func TestTodo_TAXPROFILE_001_Fault(t *testing.T) {
	profile := validTaxProfile(t)
	noExpiry := profile
	noExpiry.Exemptions = append([]TaxExemptionRevision(nil), profile.Exemptions...)
	noExpiry.Exemptions[0].ExpiresAt = values.Instant{}
	noExpiry.CanonicalDigest = ""
	if _, err := NewWorkerTaxProfileRevision(noExpiry); !errors.Is(err, ErrInvalidExemption) {
		t.Fatalf("missing expiry error = %v", err)
	}
	overlap := profile
	overlap.Registrations = append([]TaxRegistrationRevision(nil), profile.Registrations...)
	overlap.Registrations = append(overlap.Registrations, validRegistration(t, "US-CA"))
	overlap.Registrations[1].Revision = 2
	overlap.Registrations[1].ParentRevision = 1
	overlap.Registrations[1].ParentDigest = profile.Registrations[0].CanonicalDigest
	overlap.Registrations[1].CanonicalDigest = ""
	overlap.CanonicalDigest = ""
	if _, err := NewWorkerTaxProfileRevision(overlap); !errors.Is(err, ErrRegistrationOverlap) {
		t.Fatalf("overlapping registration error = %v", err)
	}
}

func TestTodo_TAXPROFILE_001_Security(t *testing.T) {
	profile := validTaxProfile(t)
	explanation, err := profile.Explain()
	if err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf("%+v", explanation)
	if strings.Contains(text, "registration-ref:") || strings.Contains(text, "evidence:election-1") {
		t.Fatalf("sensitive reference leaked: %q", text)
	}
	registration := validRegistration(t, "US-CA")
	registration.RawRegistrationID = "123-45-6789"
	registration.CanonicalDigest = ""
	if _, err := NewTaxRegistrationRevision(registration); !errors.Is(err, ErrInvalidRegistration) {
		t.Fatalf("raw registration error = %v", err)
	}
}

func TestTodo_TAXPROFILE_001_Conformance(t *testing.T) {
	if Version() <= 0 || !FilingSingle.Valid() || !ClassificationResident.Valid() || !ElectionExempt.Valid() || !ExemptionFederal.Valid() {
		t.Fatal("closed vocabulary contract failed")
	}
	if FilingStatus("UNKNOWN").Valid() || TaxClassification("LOWERED_BY_CALLER").Valid() {
		t.Fatal("unknown classification accepted")
	}
}

func TestTodo_TAXPROFILE_001_Mutation(t *testing.T) {
	registration := validRegistration(t, "US-CA")
	profile := validTaxProfile(t)
	before := profile.CanonicalDigest
	profile.ResidenceJurisdictions[0] = "US-NY"
	if profile.CanonicalDigest != before {
		t.Fatal("profile digest changed in place")
	}
	if err := profile.Validate(); err == nil {
		t.Fatal("mutated profile retained a valid digest")
	}
	port, err := NewMemoryRegistrationPort([]TaxRegistrationRevision{registration})
	if err != nil {
		t.Fatal(err)
	}
	if len(port.Registrations()) != 1 {
		t.Fatal("registration port lost its snapshot")
	}
}
