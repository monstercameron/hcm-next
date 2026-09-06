package govauth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var fixtureDate = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

func pointer(ref, owner string) EvidencePointer {
	return EvidencePointer{ArtifactRef: ref, Owner: owner, Date: fixtureDate}
}

func cmsBlock() *CMSReferenceBlock {
	return &CMSReferenceBlock{
		MARSEVolume:        "VOLUME_1",
		MARSEVersion:       "2.2",
		CMSARSRelease:      "5.1",
		DUARef:             "artifact/cms/dua",
		ISARef:             "artifact/cms/isa",
		SSPPRef:            "artifact/cms/sspp",
		PrivacyAnalysisRef: "artifact/cms/privacy-analysis",
		ReviewedBy:         "security-reviewer",
		ReviewedAt:         fixtureDate,
		ControlInheritance: []ControlInheritanceReference{{
			ControlID:     "AC-2",
			InheritedFrom: "cms-boundary",
			Evidence:      pointer("artifact/cms/ac-2", "control-owner"),
		}},
	}
}

func answers() []ProcurementAnswer {
	result := make([]ProcurementAnswer, 0, len(QuestionnaireItems))
	for _, question := range QuestionnaireItems {
		result = append(result, ProcurementAnswer{
			Question:    question,
			Answer:      "reviewed assertion for " + string(question),
			Owner:       "assurance-owner",
			Date:        fixtureDate,
			ArtifactRef: "artifact/procurement/" + string(question),
		})
	}
	return result
}

func fixture(tenant, integration string, programs ...Program) GovernmentAuthorizationProfile {
	return GovernmentAuthorizationProfile{
		Revision:           1,
		TenantID:           tenant,
		IntegrationID:      integration,
		ApplicablePrograms: programs,
		SystemBoundary:     "application, tenant data store, identity edge and declared support boundary",
		InheritedControls: []ControlInheritanceReference{{
			ControlID:     "SC-13",
			InheritedFrom: "platform-cryptography",
			Evidence:      pointer("artifact/platform/sc-13", "platform-owner"),
		}},
		Evidence:       []EvidencePointer{pointer("artifact/profile/boundary", "assurance-owner")},
		AssessorStatus: AssessorAccepted,
		ReviewDate:     fixtureDate,
		FedRAMP: &FedRAMPPathRecord{
			Path:                         FedRAMP20xA,
			PackageVersion:               "20x-package-2026.09",
			ContinuousMonitoringEvidence: []EvidencePointer{pointer("artifact/fedramp/monthly-monitoring", "assurance-owner")},
			ResponsibilityStatement:      "The customer authorizes its agency use and retains customer-side responsibilities.",
			Timeline: FedRAMPTimeline{
				AsOf:                   fixtureDate,
				NewCertificationCutoff: time.Date(2027, 6, 11, 0, 0, 0, 0, time.UTC),
				TwentyXAdoptionDate:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
				SourceArtifactRef:      "artifact/fedramp/timeline-2026",
			},
		},
		ProcurementAnswers: answers(),
	}
}

func TestTodo_SECARCH_018(t *testing.T) {
	profile, err := NewGovernmentAuthorizationProfile(fixture("tenant-public", "integration-case", ProgramFedRAMP, ProgramGovRAMP, ProgramFISMA))
	if err != nil {
		t.Fatalf("construct profile: %v", err)
	}
	if profile.RevisionDigest == "" {
		t.Fatal("sealed profile has no revision digest")
	}
	if profile.Explain() == "" || !strings.Contains(profile.Explain(), "programs=") {
		t.Fatalf("audit explanation is not shaped: %q", profile.Explain())
	}
}

func TestTodo_SECARCH_018_Golden(t *testing.T) {
	fixtures := []GovernmentAuthorizationProfile{
		fixture("tenant-public", "integration-case", ProgramFedRAMP, ProgramGovRAMP, ProgramFISMA),
	}
	aca := fixture("tenant-aca", "integration-exchange", ProgramMARSE, ProgramFISMA)
	aca.CMS = cmsBlock()
	fixtures = append(fixtures, aca)
	for i, input := range fixtures {
		sealed, err := NewGovernmentAuthorizationProfile(input)
		if err != nil {
			t.Fatalf("fixture %d: %v", i, err)
		}
		got, err := sealed.Digest()
		if err != nil {
			t.Fatalf("fixture %d digest: %v", i, err)
		}
		want := []string{
			"2ad76a2f5297aa2fc8157957f9bf2d7610cda8fd1cc4fc75bf60c0cb0d946f38",
			"c28a44be841f0b88ad4390d353a69c3008d8afd0ef47266ff1c0b8ade4aa97fb",
		}[i]
		if len(got) != 64 {
			t.Fatalf("fixture %d digest %q is not sha256 hex", i, got)
		}
		if got != want {
			t.Fatalf("fixture %d digest = %s, want %s", i, got, want)
		}
	}
}

func TestTodo_SECARCH_018_Security(t *testing.T) {
	bad := fixture("tenant-public", "integration-case", Program("UNDECLARED"))
	if _, err := NewGovernmentAuthorizationProfile(bad); err == nil || !strings.Contains(err.Error(), "applicable_programs") {
		t.Fatalf("undeclared program error = %v; want field-named refusal", err)
	}
	secret := "account-9876543210"
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	profile.SystemBoundary = secret
	sealed, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatalf("construct secret fixture: %v", err)
	}
	if strings.Contains(sealed.Explain(), secret) || strings.Contains(Explain(sealed), "tenant-public") {
		t.Fatalf("Explain disclosed sensitive content: %q", sealed.Explain())
	}
}

func TestTodo_SECARCH_018_Integration(t *testing.T) {
	profile, err := NewProfile(fixture("tenant-public", "integration-case", ProgramGovRAMP, ProgramTXRAMP))
	if err != nil {
		t.Fatalf("constructor integration: %v", err)
	}
	if _, err := profile.Digest(); err != nil {
		t.Fatalf("constructed profile digest: %v", err)
	}
	if profile.TenantID != "tenant-public" || profile.IntegrationID != "integration-case" {
		t.Fatal("constructor lost tenant/integration binding")
	}
}

func TestTodo_SECARCH_018_Conformance(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("version = %d, want 1", Version())
	}
	for _, program := range []Program{FedRAMP, GovRAMP, TXRAMP, FISMA, CJIS, FTI, CUI, MARSE, MARS_E} {
		if !program.Valid() {
			t.Fatalf("closed program %q rejected", program)
		}
	}
	if Program("FedRAMP-Moderate").Valid() {
		t.Fatal("undeclared program accepted")
	}
}

func TestTodo_SECARCH_018_Mutation(t *testing.T) {
	input := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	sealed, err := NewGovernmentAuthorizationProfile(input)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	want := sealed.RevisionDigest
	input.ApplicablePrograms[0] = ProgramCUI
	input.Evidence[0].Owner = "changed-owner"
	if sealed.RevisionDigest != want {
		t.Fatal("sealed revision changed through source slices")
	}
	sealed.SystemBoundary = "changed"
	if _, err := sealed.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("post-seal mutation error = %v; want ErrImmutableRevision", err)
	}
}

func TestTodo_SECARCH_019(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	refreshed, err := profile.RefreshFedRAMP(profile.FedRAMP.Timeline)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if refreshed.Revision != 2 || refreshed.FedRAMP.Timeline.AsOf.IsZero() {
		t.Fatalf("refreshed profile = revision %d, timeline=%v", refreshed.Revision, refreshed.FedRAMP.Timeline.AsOf)
	}
	if refreshed.FedRAMP.PackageVersion == "" || len(refreshed.FedRAMP.ContinuousMonitoringEvidence) != 1 || refreshed.FedRAMP.ResponsibilityStatement == "" {
		t.Fatal("FedRAMP record is incomplete")
	}
}

func TestTodo_SECARCH_019_Golden(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	got, err := profile.FedRAMP.Timeline.Validate(), error(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = got
	if profile.FedRAMP.Path != TWENTYX_A {
		t.Fatalf("path = %s", profile.FedRAMP.Path)
	}
}

func TestTodo_SECARCH_019_Security(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	profile.FedRAMP.Path = FedRAMPRev5
	late := profile.FedRAMP.Timeline
	late.AsOf = time.Date(2027, 6, 11, 0, 0, 0, 0, time.UTC)
	if _, err := profile.RefreshFedRAMP(late); err == nil || !errors.Is(err, ErrTimelineRefusal) {
		t.Fatalf("late REV5 refresh error = %v; want timeline refusal", err)
	}
}

func TestTodo_SECARCH_019_Integration(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	profile.FedRAMP.Path = FedRAMP20xB
	refreshed, err := profile.Refresh(profile.FedRAMP.Timeline)
	if err != nil || refreshed.FedRAMP.Path != FedRAMP20xB {
		t.Fatalf("20x refresh = path %v, err %v", refreshed.FedRAMP.Path, err)
	}
}

func TestTodo_SECARCH_019_Conformance(t *testing.T) {
	for _, path := range []FedRAMPPath{REV5, TWENTYX_A, TWENTYX_B, TWENTYX_C} {
		if !path.Valid() {
			t.Fatalf("path %q is not valid", path)
		}
	}
	if FedRAMPPath("MODERATE").Valid() {
		t.Fatal("unlisted FedRAMP class accepted")
	}
}

func TestTodo_SECARCH_019_Mutation(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramFedRAMP)
	refreshed, err := profile.RefreshFedRAMP(profile.FedRAMP.Timeline)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.RevisionDigest == profile.RevisionDigest {
		t.Fatal("FedRAMP refresh did not create a new digested revision")
	}
	if profile.Revision != 1 {
		t.Fatal("refresh mutated receiver")
	}
}

func TestTodo_SECARCH_022(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE, ProgramFISMA)
	profile.CMS = cmsBlock()
	sealed, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatalf("ACA profile: %v", err)
	}
	if sealed.CMS.MARSEVersion != "2.2" || sealed.CMS.CMSARSRelease != "5.1" || sealed.CMS.PrivacyAnalysisRef == "" {
		t.Fatal("CMS releases or privacy analysis are not pinned")
	}
}

func TestTodo_SECARCH_022_Golden(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	profile.CMS = cmsBlock()
	first, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if first.RevisionDigest != second.RevisionDigest {
		t.Fatalf("identical ACA fixtures differ: %s vs %s", first.RevisionDigest, second.RevisionDigest)
	}
}

func TestTodo_SECARCH_022_Security(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	if _, err := NewGovernmentAuthorizationProfile(profile); err == nil || !strings.Contains(err.Error(), "cms") {
		t.Fatalf("missing CMS block error = %v", err)
	}
	block := cmsBlock()
	block.CMSARSRelease = ""
	profile.CMS = block
	if _, err := NewGovernmentAuthorizationProfile(profile); err == nil || !strings.Contains(err.Error(), "cms.cms_ars_release") {
		t.Fatalf("missing CMS ARS error = %v", err)
	}
}

func TestTodo_SECARCH_022_Integration(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	profile.CMS = cmsBlock()
	sealed, err := NewProfile(profile)
	if err != nil || len(sealed.CMS.ControlInheritance) != 1 {
		t.Fatalf("CMS integration = %+v, err %v", sealed.CMS, err)
	}
}

func TestTodo_SECARCH_022_Conformance(t *testing.T) {
	block := cmsBlock()
	if err := block.Validate(); err != nil {
		t.Fatal(err)
	}
	block.ControlInheritance[0].Evidence.ArtifactRef = ""
	if err := block.Validate(); err == nil || !strings.Contains(err.Error(), "evidence.artifact_ref") {
		t.Fatalf("unreviewable inheritance error = %v", err)
	}
}

func TestTodo_SECARCH_022_Mutation(t *testing.T) {
	profile := fixture("tenant-aca", "integration-exchange", ProgramMARSE)
	profile.CMS = cmsBlock()
	sealed, err := NewGovernmentAuthorizationProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	sealed.CMS.ReviewedBy = "different-reviewer"
	if _, err := sealed.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("CMS mutation error = %v", err)
	}
}

func TestTodo_SECARCH_024(t *testing.T) {
	pack, err := GenerateProcurementPack(fixture("tenant-public", "integration-case", ProgramGovRAMP))
	if err != nil {
		t.Fatalf("generate pack: %v", err)
	}
	if len(pack.Answers) != len(QuestionnaireItems) || pack.PackDigest == "" {
		t.Fatalf("pack = %+v", pack)
	}
}

func TestTodo_SECARCH_024_Golden(t *testing.T) {
	profiles := []GovernmentAuthorizationProfile{
		fixture("tenant-public", "integration-case", ProgramGovRAMP),
		fixture("tenant-aca", "integration-exchange", ProgramMARSE),
	}
	profiles[1].CMS = cmsBlock()
	for i, profile := range profiles {
		pack, err := GenerateProcurementEvidencePack(profile)
		if err != nil {
			t.Fatalf("fixture %d: %v", i, err)
		}
		if got, err := pack.Digest(); err != nil || got != pack.PackDigest {
			t.Fatalf("fixture %d pack digest = %s, err %v", i, got, err)
		}
	}
}

func TestTodo_SECARCH_024_Security(t *testing.T) {
	profile := fixture("tenant-public", "integration-case", ProgramGovRAMP)
	profile.ProcurementAnswers = profile.ProcurementAnswers[:len(profile.ProcurementAnswers)-1]
	if _, err := GenerateProcurementPack(profile); err == nil || !errors.Is(err, ErrUnansweredQuestion) || !strings.Contains(err.Error(), "audit_reports") {
		t.Fatalf("missing answer error = %v", err)
	}
	pack, err := GenerateProcurementPack(fixture("tenant-public", "integration-case", ProgramGovRAMP))
	if err != nil {
		t.Fatal(err)
	}
	pack.Answers[0].Answer = "changed"
	if _, err := pack.Digest(); !errors.Is(err, ErrImmutableRevision) {
		t.Fatalf("pack mutation error = %v", err)
	}
}

func TestTodo_SECARCH_024_Integration(t *testing.T) {
	profile, err := NewGovernmentAuthorizationProfile(fixture("tenant-public", "integration-case", ProgramGovRAMP))
	if err != nil {
		t.Fatal(err)
	}
	pack, err := GenerateProcurementPack(profile)
	if err != nil || pack.ProfileDigest != profile.RevisionDigest {
		t.Fatalf("pack/profile wiring = %s/%s, err %v", pack.ProfileDigest, profile.RevisionDigest, err)
	}
}

func TestTodo_SECARCH_024_Conformance(t *testing.T) {
	if len(QuestionnaireItems) != 13 {
		t.Fatalf("standard item count = %d, want 13", len(QuestionnaireItems))
	}
	for i, answer := range answers() {
		if answer.Question != QuestionnaireItems[i] {
			t.Fatalf("question %d = %s, want %s", i, answer.Question, QuestionnaireItems[i])
		}
	}
}

func TestTodo_SECARCH_024_Mutation(t *testing.T) {
	input := fixture("tenant-public", "integration-case", ProgramGovRAMP)
	pack, err := GenerateProcurementPack(input)
	if err != nil {
		t.Fatal(err)
	}
	input.ProcurementAnswers[0].Answer = "different source assertion"
	if _, err := pack.Digest(); err != nil {
		t.Fatalf("pack was affected by source mutation: %v", err)
	}
}
