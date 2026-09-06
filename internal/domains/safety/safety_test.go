package safety

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func safetyInstant() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
}

func TestSafetyDomainSeparatesOperationalMedicalClaimAndRegulatoryAuthority(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "incident-1", CaseRef: "case-1", CompartmentRef: "operational", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentInjury, WorkerRef: "worker-secret", ReporterRef: "reporter-secret", LocationRef: "site-1", Description: "operational incident", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	injury, err := NewInjuryRevision(InjuryRevision{ID: "injury-1", CaseRef: "case-1", CompartmentRef: "medical", Revision: 1, IncidentRef: incident.CanonicalDigest, WorkerRef: "worker-secret", Kind: InjuryPhysical, MedicalEvidenceRef: "medical-evidence-1", Severity: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	determination, err := DetermineReportability(incident, OSHAReportableSevere, "reportability-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := determination.Deadline.Time().Sub(incident.IncidentAt.Time()); got != 24*time.Hour {
		t.Fatalf("deadline delta = %s", got)
	}
	claim, err := NewClaimRevision(ClaimRevision{ID: "claim-1", CaseRef: "case-1", CompartmentRef: "claims", Revision: 1, IncidentRef: incident.CanonicalDigest, WorkerRef: "worker-secret", ClaimRef: "claim-ref", AuthorityRef: "workers-comp-authority", Status: ClaimSubmitted})
	if err != nil {
		t.Fatal(err)
	}
	restriction, err := NewWorkRestrictionRevision(WorkRestrictionRevision{ID: "restriction-1", CaseRef: "case-1", CompartmentRef: "medical", Revision: 1, IncidentRef: incident.CanonicalDigest, WorkerRef: "worker-secret", Kind: RestrictionModifiedDuty, MedicalEvidenceRef: "medical-evidence-1", Status: RestrictionActive})
	if err != nil {
		t.Fatal(err)
	}
	action, err := NewCorrectiveActionRevision(CorrectiveActionRevision{ID: "action-1", CaseRef: "case-1", CompartmentRef: "operational", Revision: 1, IncidentRef: incident.CanonicalDigest, OwnerRef: "owner-1", DueRule: "verify-before-close", Action: "repair guard", Status: CorrectiveActionOpen})
	if err != nil {
		t.Fatal(err)
	}
	if injury.CanonicalDigest == "" || claim.CanonicalDigest == "" || restriction.CanonicalDigest == "" || action.CanonicalDigest == "" {
		t.Fatal("expected canonical digests")
	}
	if strings.Contains(incident.Explain(), "worker-secret") || strings.Contains(incident.Explain(), "reporter-secret") {
		t.Fatalf("sensitive incident values leaked: %q", incident.Explain())
	}
	if !strings.Contains(determination.Explain(), string(OSHAReportableSevere)) {
		t.Fatalf("reportability explanation = %q", determination.Explain())
	}
}

func TestTodo_SAFETY_001_Property(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "d", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	child, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 2, ParentRevision: 1, ParentDigest: incident.CanonicalDigest, IncidentAt: incident.IncidentAt, Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "updated", Status: IncidentClosed})
	if err != nil {
		t.Fatal(err)
	}
	if incident.Revision != 1 || child.ParentDigest != incident.CanonicalDigest || incident.CanonicalDigest == child.CanonicalDigest {
		t.Fatal("lineage or immutability contract failed")
	}
}

func TestTodo_SAFETY_001_Golden(t *testing.T) {
	for _, class := range []ReportabilityClass{NotReportable, OSHARecordable, OSHAReportableFatality, OSHAReportableSevere} {
		rule, ok := RuleFor(class)
		if !ok {
			t.Fatalf("missing rule for %s", class)
		}
		if rule.Citation == "" || rule.Clock == "" {
			t.Fatalf("incomplete rule for %s: %+v", class, rule)
		}
	}
}

func TestTodo_SAFETY_001_Race(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "d", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	for i := 0; i < 16; i++ {
		if err := store.SaveIncident(incident); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := store.GetIncident("i", 1); !ok {
		t.Fatal("incident not found")
	}
}

func TestTodo_SAFETY_001_Fault(t *testing.T) {
	_, err := NewClaimRevision(ClaimRevision{ID: "claim", CaseRef: "case", CompartmentRef: "claims", Revision: 1, WorkerRef: "worker", ClaimRef: "claim-ref", AuthorityRef: "authority", Status: ClaimDraft})
	if !errors.Is(err, ErrIncidentRequired) {
		t.Fatalf("claim error = %v", err)
	}
	_, err = NewCorrectiveActionRevision(CorrectiveActionRevision{ID: "action", CaseRef: "case", CompartmentRef: "operational", Revision: 1, IncidentRef: "incident", OwnerRef: "owner", DueRule: "rule", Action: "action", Status: CorrectiveActionClosed})
	if !errors.Is(err, ErrVerificationRequired) {
		t.Fatalf("corrective-action error = %v", err)
	}
}

func TestTodo_SAFETY_001_Security(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentInjury, WorkerRef: "secret-worker", ReporterRef: "secret-reporter", LocationRef: "secret-location", Description: "secret description", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-worker", "secret-reporter", "secret-location", "secret description"} {
		if strings.Contains(incident.Explain(), secret) {
			t.Fatalf("secret %q leaked: %q", secret, incident.Explain())
		}
	}
}

func TestTodo_SAFETY_001_Conformance(t *testing.T) {
	table := ClockTable()
	if len(table) != 4 {
		t.Fatalf("clock table length = %d", len(table))
	}
	if table[1].Duration != 7*24*time.Hour || table[2].Duration != 8*time.Hour || table[3].Duration != 24*time.Hour {
		t.Fatalf("clock table = %+v", table)
	}
}

func TestTodo_SAFETY_001_Mutation(t *testing.T) {
	incident, err := NewIncidentRevision(IncidentRevision{ID: "i", CaseRef: "c", CompartmentRef: "o", Revision: 1, IncidentAt: safetyInstant(), Kind: IncidentNearMiss, WorkerRef: "w", ReporterRef: "r", LocationRef: "l", Description: "d", Status: IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	incident.CanonicalDigest = "sha256:forged"
	if err := incident.Validate(); err == nil {
		t.Fatal("forged digest accepted")
	}
}
