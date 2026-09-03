package commercial

import (
	"errors"
	"testing"
)

func TestPilotCommercialPackageMatchesReleaseEntitlementsCostsRisksAndExitTerms(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Release != ReleaseP1A || p.ManifestDigest != P1AManifestDigest {
		t.Fatalf("package is not bound to signed P1A manifest")
	}
	if len(p.Entitlements) != 8 || p.Pricing.MinimumCents != 1500000 || p.Pricing.MaximumCents != 4000000 {
		t.Fatalf("pilot scope or price drifted: %+v", p)
	}
	if p.Authority.WriteAuthority || p.Authority.Topology != "EXTERNAL_OBSERVATION" {
		t.Fatalf("P1A must not grant write authority")
	}
	if p.Exit.RetentionDays == 0 || !p.Exit.DeletionCertificate || p.StopThresholdPct <= p.RepriceThresholdPct {
		t.Fatalf("exit and stop terms are incomplete")
	}
}

func TestPilotCommercialPackageCanonicalRoundTrip(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	b, err := p.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.ManifestDigest != p.ManifestDigest || got.Pricing.Currency != "USD" {
		t.Fatalf("round trip changed contract")
	}
}

func TestPilotCommercialPackageRejectsAuthorityExpansion(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	p.Authority.WriteAuthority = true
	if err := p.Validate(); !errors.Is(err, ErrAuthority) {
		t.Fatalf("expected authority error, got %v", err)
	}
}

func TestPilotCommercialPackageRejectsPriceDrift(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	p.Pricing.MaximumCents++
	if err := p.Validate(); !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("expected package error, got %v", err)
	}
}

func TestPilotCommercialPackageAcceptsJSONWhitespace(t *testing.T) {
	p := DefaultPilotCommercialPackage()
	b, err := p.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if _, err := Parse(b); err != nil {
		t.Fatalf("whitespace should not alter valid JSON, got %v", err)
	}
}
