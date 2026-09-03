package cosignkit

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeVerifier struct {
	evidence Evidence
	err      error
	calls    int
}

func (f *fakeVerifier) Verify(context.Context, Artifact, Policy) (Evidence, error) {
	f.calls++
	return f.evidence, f.err
}

func testPolicy(now time.Time) (Artifact, Policy, Evidence) {
	a := Artifact{Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	p := Policy{Version: "cosign-v1", SubjectDigest: a.Digest, BuilderIdentity: "repo:acme/hcm", Issuer: "https://token.actions.githubusercontent.com", SourceRepository: "acme/hcm", WorkflowRef: "refs/tags/v1", PredicateType: "https://slsa.dev/provenance/v1", SBOMDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", MaxAttestationAge: 24 * time.Hour, RequireTransparency: true}
	e := Evidence{PolicyVersion: p.Version, Verified: true, SubjectDigest: a.Digest, BuilderIdentity: p.BuilderIdentity, Issuer: p.Issuer, SourceRepository: p.SourceRepository, WorkflowRef: p.WorkflowRef, PredicateType: p.PredicateType, SBOMDigest: p.SBOMDigest, TransparencyValid: true, AttestedAt: now.Add(-time.Hour)}
	return a, p, e
}

func TestCosignAdmissionRejectsWrongSubjectIdentityIssuerOrSBOM(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name, field, code string
		mutate            func(*Evidence)
	}{
		{"subject", "subjectDigest", "WRONG_SUBJECT_DIGEST", func(e *Evidence) { e.SubjectDigest = "sha256:" + "c" + string(make([]byte, 63)) }},
		{"builder", "builderIdentity", "UNTRUSTED_BUILDER", func(e *Evidence) { e.BuilderIdentity = "attacker" }},
		{"issuer", "issuer", "WRONG_ISSUER", func(e *Evidence) { e.Issuer = "attacker" }},
		{"sbom", "sbomDigest", "DETACHED_SBOM", func(e *Evidence) {
			e.SBOMDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, p, e := testPolicy(now)
			tc.mutate(&e)
			d, err := (Checker{Verifier: &fakeVerifier{evidence: e}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
			if d.Allowed || d.Code != tc.code || err == nil {
				t.Fatalf("decision=%+v err=%v", d, err)
			}
		})
	}
}

func TestCosignAdmissionAcceptsVerifiedEvidence(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	a, p, e := testPolicy(now)
	d, err := (Checker{Verifier: &fakeVerifier{evidence: e}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
	if err != nil || !d.Allowed || d.Code != "ADMITTED" {
		t.Fatalf("decision=%+v err=%v", d, err)
	}
}

func TestTodo_TOOL_023_Fault(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	a, p, e := testPolicy(now)
	for _, tc := range []struct {
		name, code string
		change     func(*Evidence)
	}{{"unsigned", "UNSIGNED", func(e *Evidence) { e.Verified = false }}, {"transparency", "INVALID_TRANSPARENCY", func(e *Evidence) { e.TransparencyValid = false }}, {"replay", "REPLAYED_ATTESTATION", func(e *Evidence) { e.Replay = true }}, {"stale", "STALE_ATTESTATION", func(e *Evidence) { e.AttestedAt = now.Add(-48 * time.Hour) }}} {
		t.Run(tc.name, func(t *testing.T) {
			x := e
			tc.change(&x)
			d, _ := (Checker{Verifier: &fakeVerifier{evidence: x}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
			if d.Allowed || d.Code != tc.code {
				t.Fatalf("decision=%+v", d)
			}
		})
	}
}

func TestTodo_TOOL_023_Conformance(t *testing.T) {
	a, p, e := testPolicy(time.Now().UTC())
	p.SubjectDigest = "sha256:bad"
	v := &fakeVerifier{evidence: e}
	d, err := (Checker{Verifier: v}).Check(context.Background(), a, p)
	if d.Allowed || !errors.Is(err, ErrRejected) || v.calls != 0 {
		t.Fatalf("decision=%+v err=%v calls=%d", d, err, v.calls)
	}
}
