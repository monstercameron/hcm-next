package assurancemeta

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidationHelpers_Boundaries(t *testing.T) {
	if got := object(nil); string(got) != "{}" {
		t.Fatalf("object(nil) = %s", got)
	}
	if got := object([]byte(`[]`)); string(got) != "[]" {
		t.Fatalf("object(nonempty) = %s", got)
	}
	if !oneOf("B", "A", "B") || oneOf("C", "A", "B") {
		t.Fatal("oneOf boundary result is incorrect")
	}
	if err := ensureTenant(context.Background(), nil, uuid.Nil); !errors.Is(err, ErrNilTenant) {
		t.Fatalf("ensureTenant(nil tenant) = %v, want ErrNilTenant", err)
	}
}

func TestSLODefinitionValidate_Errors(t *testing.T) {
	good := SLODefinition{TenantID: uuid.New(), SLOID: uuid.New(), SLOVersion: 1, ObjectiveKind: "AVAILABILITY", TargetRatio: 0.99, WindowSeconds: 60, EffectiveFrom: time.Now()}
	cases := []struct {
		name   string
		mutate func(*SLODefinition)
		want   error
	}{
		{"nil id", func(d *SLODefinition) { d.SLOID = uuid.Nil }, ErrNilTenant},
		{"missing version", func(d *SLODefinition) { d.SLOVersion = 0 }, ErrMissingVersion},
		{"bad objective", func(d *SLODefinition) { d.ObjectiveKind = "NO" }, ErrInvalidEnum},
		{"zero ratio", func(d *SLODefinition) { d.TargetRatio = 0 }, ErrImpossibleRatio},
		{"ratio over one", func(d *SLODefinition) { d.TargetRatio = 1.1 }, ErrImpossibleRatio},
		{"missing window", func(d *SLODefinition) { d.WindowSeconds = 0 }, ErrMissingWindow},
		{"reversed effective interval", func(d *SLODefinition) { end := d.EffectiveFrom; d.EffectiveTo = &end }, ErrMissingWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSLOObservationValidate_Errors(t *testing.T) {
	good := SLOObservation{TenantID: uuid.New(), ObservationID: uuid.New(), SLOID: uuid.New(), SLOVersion: 1,
		WindowStart: time.Now(), WindowEnd: time.Now().Add(time.Hour), GoodEvents: 1, TotalEvents: 2,
		ErrorBudgetRemaining: 0.5, SourceQuality: "COMPLETE"}
	cases := []struct {
		name   string
		mutate func(*SLOObservation)
		want   error
	}{
		{"nil id", func(o *SLOObservation) { o.ObservationID = uuid.Nil }, ErrNilTenant},
		{"missing version", func(o *SLOObservation) { o.SLOVersion = 0 }, ErrMissingVersion},
		{"empty window", func(o *SLOObservation) { o.WindowEnd = o.WindowStart }, ErrMissingWindow},
		{"negative good", func(o *SLOObservation) { o.GoodEvents = -1 }, ErrImpossibleRatio},
		{"negative total", func(o *SLOObservation) { o.TotalEvents = -1 }, ErrImpossibleRatio},
		{"good exceeds total", func(o *SLOObservation) { o.GoodEvents = 3 }, ErrImpossibleRatio},
		{"budget below range", func(o *SLOObservation) { o.ErrorBudgetRemaining = -1.1 }, ErrImpossibleRatio},
		{"budget above range", func(o *SLOObservation) { o.ErrorBudgetRemaining = 1.1 }, ErrImpossibleRatio},
		{"bad quality", func(o *SLOObservation) { o.SourceQuality = "NO" }, ErrInvalidEnum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestControlEvidenceValidate_Errors(t *testing.T) {
	good := ControlEvidence{TenantID: uuid.New(), EvidenceID: uuid.New(), ControlVersion: 1, ArtifactDigest: "digest",
		WindowStart: time.Now(), WindowEnd: time.Now().Add(time.Hour), FreshnessDeadline: time.Now().Add(2 * time.Hour),
		Completeness: "COMPLETE", Result: "PASS", RetentionClass: "PERMANENT"}
	cases := []struct {
		name   string
		mutate func(*ControlEvidence)
		want   error
	}{
		{"nil id", func(e *ControlEvidence) { e.EvidenceID = uuid.Nil }, ErrNilTenant},
		{"missing version", func(e *ControlEvidence) { e.ControlVersion = 0 }, ErrMissingVersion},
		{"missing digest", func(e *ControlEvidence) { e.ArtifactDigest = "" }, ErrMissingDigest},
		{"reversed window", func(e *ControlEvidence) { e.WindowEnd = e.WindowStart }, ErrMissingWindow},
		{"missing freshness", func(e *ControlEvidence) { e.FreshnessDeadline = e.WindowEnd }, ErrMissingFreshness},
		{"bad completeness", func(e *ControlEvidence) { e.Completeness = "NO" }, ErrInvalidEnum},
		{"bad result", func(e *ControlEvidence) { e.Result = "NO" }, ErrInvalidEnum},
		{"bad retention", func(e *ControlEvidence) { e.RetentionClass = "NO" }, ErrInvalidEnum},
		{"fail without deficiency", func(e *ControlEvidence) { e.Result = "FAIL" }, ErrInvalidEnum},
		{"partial without deficiency", func(e *ControlEvidence) { e.Result = "PARTIAL" }, ErrInvalidEnum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAuditPackageValidate_Errors(t *testing.T) {
	good := AuditPackage{TenantID: uuid.New(), PackageID: uuid.New(), ManifestDigest: "digest", SignatureRef: "artifact://sig/1",
		Completeness: "PARTIAL", GeneratedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Status: "GENERATED"}
	cases := []struct {
		name   string
		mutate func(*AuditPackage)
		want   error
	}{
		{"nil id", func(p *AuditPackage) { p.PackageID = uuid.Nil }, ErrNilTenant},
		{"missing digest", func(p *AuditPackage) { p.ManifestDigest = "" }, ErrMissingDigest},
		{"raw signature", func(p *AuditPackage) { p.SignatureRef = "MEUCIQD" }, ErrRawSignature},
		{"bad completeness", func(p *AuditPackage) { p.Completeness = "NO" }, ErrInvalidEnum},
		{"bad status", func(p *AuditPackage) { p.Status = "NO" }, ErrInvalidEnum},
		{"complete empty", func(p *AuditPackage) { p.Completeness = "COMPLETE" }, ErrEmptyPackage},
		{"expiry not after generated", func(p *AuditPackage) { p.ExpiresAt = p.GeneratedAt }, ErrMissingWindow},
		{"delivered before generated", func(p *AuditPackage) { at := p.GeneratedAt.Add(-time.Minute); p.DeliveredAt = &at }, ErrMissingWindow},
		{"delivered without timestamp", func(p *AuditPackage) { p.Status = "DELIVERED" }, ErrMissingWindow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mutate(&in)
			if err := in.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}
