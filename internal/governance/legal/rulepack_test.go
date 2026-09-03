package legal

import (
	"errors"
	"testing"
	"time"
)

func TestSeedPacksValidate(t *testing.T) {
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := ca.Validate(); err != nil {
		t.Errorf("California pack fails Validate: %v", err)
	}
	ny, err := NewYorkPromotionPack()
	if err != nil {
		t.Fatalf("NewYorkPromotionPack: %v", err)
	}
	if err := ny.Validate(); err != nil {
		t.Errorf("New York pack fails Validate: %v", err)
	}

	// Every seeded citation must name the correct research file and stay
	// unreviewed: this package never upgrades its own review status.
	for _, pack := range []RulePack{ca, ny} {
		var wantFile string
		switch pack.Jurisdiction.State {
		case "CA":
			wantFile = californiaSourceFile
		case "NY":
			wantFile = newYorkSourceFile
		}
		for _, c := range allCitations(pack) {
			if c.SourceFile != wantFile {
				t.Errorf("%s: citation source file = %q, want %q", pack.PackID, c.SourceFile, wantFile)
			}
			if c.Status != ReviewStatusUnreviewed {
				t.Errorf("%s: citation status = %s, want UNREVIEWED", pack.PackID, c.Status)
			}
			if c.Section == "" {
				t.Errorf("%s: citation has an empty statutory section", pack.PackID)
			}
		}
	}
}

func allCitations(p RulePack) []Citation {
	var out []Citation
	for _, o := range p.Notices {
		out = append(out, o.Citation)
	}
	for _, o := range p.FieldRestrictions {
		out = append(out, o.Citation)
	}
	for _, o := range p.RetentionRules {
		out = append(out, o.Citation)
	}
	for _, o := range p.LeaveInteractions {
		out = append(out, o.Citation)
	}
	for _, o := range p.PayFrequencyConstraints {
		out = append(out, o.Citation)
	}
	for _, o := range p.FinalPayDeadlines {
		out = append(out, o.Citation)
	}
	for _, o := range p.PayTransparencyDuties {
		out = append(out, o.Citation)
	}
	for _, o := range p.NonCompeteThresholds {
		out = append(out, o.Citation)
	}
	for _, o := range p.EVerifyChecks {
		out = append(out, o.Citation)
	}
	for _, o := range p.MiniWARNTriggers {
		out = append(out, o.Citation)
	}
	return out
}

func TestRegistryRejectsDuplicateRegistration(t *testing.T) {
	reg := NewRegistry()
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := reg.Register(ca); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := reg.Register(ca); !errors.Is(err, ErrRulePackDuplicate) {
		t.Fatalf("second Register = %v, want ErrRulePackDuplicate", err)
	}
}

func TestRegistryLookupPrefersMoreSpecificLocalityFallback(t *testing.T) {
	reg := NewRegistry()
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := reg.Register(ca); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A worker in a specific CA locality still resolves to the state-level
	// pack: the registry falls back from an unmatched exact locality key to
	// the state-level key rather than reporting no coverage.
	sf := Jurisdiction{Country: "US", State: "CA", Locality: "San Francisco"}
	date := mustDate(t, 2026, time.June, 1)
	pack, err := reg.Lookup(sf, date)
	if err != nil {
		t.Fatalf("Lookup(locality fallback): %v", err)
	}
	if pack.PackID != ca.PackID {
		t.Errorf("Lookup returned %q, want %q", pack.PackID, ca.PackID)
	}
}

func TestRegistryLookupReportsRuleCoverageUnknown(t *testing.T) {
	reg := testRegistryForRulepackTest(t)
	date := mustDate(t, 2026, time.June, 1)
	if _, err := reg.Lookup(Jurisdiction{Country: "US", State: "TX"}, date); !errors.Is(err, ErrRuleCoverageUnknown) {
		t.Fatalf("Lookup(unregistered jurisdiction) = %v, want ErrRuleCoverageUnknown", err)
	}
}

func TestRegistryGetExactMissingReleaseIsRuleCoverageUnknown(t *testing.T) {
	reg := testRegistryForRulepackTest(t)
	_, err := reg.GetExact(RulePackRelease{PackID: "does-not-exist", Version: 99, Jurisdiction: testCAJurisdiction()})
	if !errors.Is(err, ErrRuleCoverageUnknown) {
		t.Fatalf("GetExact(missing release) = %v, want ErrRuleCoverageUnknown", err)
	}
}

func TestRegisterCopiesObligationSlicesDefensively(t *testing.T) {
	reg := NewRegistry()
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if err := reg.Register(ca); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Mutating the caller's own slice after registration must never reach
	// the stored pack.
	ca.Notices[0].ID = "corrupted-after-register"

	date := mustDate(t, 2026, time.June, 1)
	stored, err := reg.Lookup(testCAJurisdiction(), date)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if stored.Notices[0].ID == "corrupted-after-register" {
		t.Fatal("Register did not copy the Notices slice; caller mutation reached the registry")
	}
}

func TestEvaluateRejectsAnUnverifiedContext(t *testing.T) {
	reg := testRegistryForRulepackTest(t)
	signer := fixedSigner(t, 0x07)
	now := mustInstant(t, 1_770_800_000)
	ctx, err := Resolve(validInput(t, testCAJurisdiction()), reg, signer, now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	ctx.effectiveDate = mustDate(t, 2030, time.January, 1) // tamper after the fact

	if _, err := Evaluate(ctx, PromotionProposalSnapshot{}, reg); err == nil {
		t.Fatal("Evaluate accepted a tampered context")
	}
}

// testRegistryForRulepackTest mirrors testRegistry from context_test.go; it
// exists under a different name only to avoid confusion when this file is
// read on its own. Both build the same CA+NY fixture registry.
func testRegistryForRulepackTest(t *testing.T) *Registry {
	t.Helper()
	return testRegistry(t)
}
