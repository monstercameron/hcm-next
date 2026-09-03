package evidenceexport

import (
	"errors"
	"testing"
	"time"
)

// TestADMIN007ValidationMatrix keeps the operator contract exact at its
// boundary.  These are intentionally table-driven: each row is one refusal
// or acceptance that an export adapter must preserve.
func TestADMIN007ValidationMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	cases := []struct {
		name string
		edit func(*Request)
		want error
	}{
		{"missing tenant", func(r *Request) { r.Authorization.TenantID = "" }, ErrUnauthorized},
		{"wrong purpose", func(r *Request) { r.Authorization.Purpose = "backup" }, ErrUnauthorized},
		{"missing scope", func(r *Request) { r.Authorization.Scope = "" }, ErrUnauthorized},
		{"missing redaction proof", func(r *Request) { r.Authorization.RedactionProfile = "" }, ErrUnauthorized},
		{"missing source proof", func(r *Request) { r.Authorization.SourceProof = "" }, ErrUnauthorized},
		{"missing config proof", func(r *Request) { r.Authorization.ConfigProof = "" }, ErrUnauthorized},
		{"missing policy proof", func(r *Request) { r.Authorization.PolicyProof = "" }, ErrUnauthorized},
		{"expired", func(r *Request) { r.Authorization.ExpiresAt = now }, ErrExpired},
		{"oversized authorization ttl", func(r *Request) { r.Authorization.ExpiresAt = r.Authorization.IssuedAt.Add(MaxTTL + time.Nanosecond) }, ErrUnauthorized},
		{"unauthorized field", func(r *Request) { r.Records[0].Fields["secret"] = "x" }, ErrUnauthorized},
		{"empty query", func(r *Request) { r.Query = "" }, ErrInvalidRequest},
		{"reversed range", func(r *Request) { r.From = r.To }, ErrInvalidRequest},
		{"oversized range", func(r *Request) { r.From = r.To.Add(-MaxTTL - time.Nanosecond) }, ErrInvalidRequest},
		{"zero chunk size", func(r *Request) { r.ChunkSize = 0 }, ErrInvalidRequest},
		{"empty records", func(r *Request) { r.Records = nil }, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequest(now)
			tc.edit(&r)
			if _, err := NewManager().Start(r, now); !errors.Is(err, tc.want) {
				t.Fatalf("Start error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestADMIN007ProgressFailureRecoveryMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildView(op); got.Status != StatusPending || got.Completed != 0 || got.Total != 3 || got.NextChunk != 0 || !got.CanResume || got.CanRetry {
		t.Fatalf("pending view = %+v", got)
	}
	failed, err := m.Fail(op.ID, "source unavailable")
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildView(failed); got.Status != StatusFailed || got.Failure != "source unavailable" || !got.CanResume || !got.CanRetry || got.Completed != 0 {
		t.Fatalf("failed view = %+v", got)
	}
	recovered, err := m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := BuildView(recovered); got.Status != StatusComplete || got.Completed != got.Total || got.NextChunk != recovered.Manifest.ChunkCount || got.CanResume || got.CanRetry || got.ManifestDigest == "" {
		t.Fatalf("recovered view = %+v", got)
	}
	if _, err := m.Resume(op.ID, 0, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale post-completion checkpoint error = %v, want %v", err, ErrConflict)
	}
}

func TestADMIN007ImmutableManifestAndOfflineSignatureMatrix(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	op, err = m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	p := op.Artifact(validRequest(now).Records)
	if !VerifyOffline(p) {
		t.Fatal("valid package failed offline verification")
	}
	cases := []struct {
		name string
		edit func(*Package)
	}{
		{"scope", func(p *Package) { p.Manifest.Scope = "other" }},
		{"query", func(p *Package) { p.Manifest.Query = "other" }},
		{"redaction profile", func(p *Package) { p.Manifest.RedactionProfile = "full" }},
		{"source proof", func(p *Package) { p.Manifest.SourceProof = "forged" }},
		{"config proof", func(p *Package) { p.Manifest.ConfigProof = "forged" }},
		{"policy proof", func(p *Package) { p.Manifest.PolicyProof = "forged" }},
		{"expiry", func(p *Package) { p.Manifest.ExpiresAt = p.Manifest.ExpiresAt.Add(time.Minute) }},
		{"record digest", func(p *Package) { p.Records[0].Digest = "forged" }},
		{"record count", func(p *Package) { p.Records = p.Records[:1] }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := Package{Manifest: p.Manifest, Records: append([]Record(nil), p.Records...)}
			tc.edit(&copy)
			if !errors.Is(copy.Verify(), ErrTampered) {
				t.Fatalf("Verify() = nil for %s", tc.name)
			}
			if VerifyOffline(copy) {
				t.Fatalf("VerifyOffline() accepted %s tampering", tc.name)
			}
		})
	}
}
