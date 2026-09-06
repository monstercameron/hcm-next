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

func TestCosignAdmission_RejectsInvalidInputsWithoutVerification(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	a, p, e := testPolicy(now)
	for _, tc := range []struct {
		name   string
		mutate func(*Policy)
	}{
		{"missing version", func(p *Policy) { p.Version = "" }},
		{"invalid subject digest", func(p *Policy) { p.SubjectDigest = "sha256:bad" }},
		{"zero age", func(p *Policy) { p.MaxAttestationAge = 0 }},
		{"missing builder", func(p *Policy) { p.BuilderIdentity = "" }},
		{"missing issuer", func(p *Policy) { p.Issuer = "" }},
		{"missing source", func(p *Policy) { p.SourceRepository = "" }},
		{"missing workflow", func(p *Policy) { p.WorkflowRef = "" }},
		{"missing predicate", func(p *Policy) { p.PredicateType = "" }},
		{"invalid sbom digest", func(p *Policy) { p.SBOMDigest = "sha256:bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := p
			tc.mutate(&policy)
			verifier := &fakeVerifier{evidence: e}
			d, err := (Checker{Verifier: verifier, Now: func() time.Time { return now }}).Check(context.Background(), a, policy)
			if d.Allowed || d.Code != "INVALID_POLICY" || !errors.Is(err, ErrRejected) || verifier.calls != 0 {
				t.Fatalf("decision=%+v err=%v calls=%d", d, err, verifier.calls)
			}
		})
	}
	t.Run("invalid artifact and wrong subject do not call verifier", func(t *testing.T) {
		verifier := &fakeVerifier{evidence: e}
		bad := a
		bad.Digest = "not-a-digest"
		d, err := (Checker{Verifier: verifier, Now: func() time.Time { return now }}).Check(context.Background(), bad, p)
		if d.Allowed || d.Code != "INVALID_ARTIFACT_DIGEST" || !errors.Is(err, ErrRejected) || verifier.calls != 0 {
			t.Fatalf("invalid artifact decision=%+v err=%v calls=%d", d, err, verifier.calls)
		}
		bad = a
		policy := p
		policy.SubjectDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		d, err = (Checker{Verifier: verifier, Now: func() time.Time { return now }}).Check(context.Background(), bad, policy)
		if d.Allowed || d.Code != "WRONG_SUBJECT_DIGEST" || !errors.Is(err, ErrRejected) || verifier.calls != 0 {
			t.Fatalf("wrong subject decision=%+v err=%v calls=%d", d, err, verifier.calls)
		}
	})
	t.Run("nil verifier is rejected", func(t *testing.T) {
		d, err := (Checker{Now: func() time.Time { return now }}).Check(context.Background(), a, p)
		if d.Allowed || d.Code != "VERIFIER_UNAVAILABLE" || !errors.Is(err, ErrRejected) {
			t.Fatalf("decision=%+v err=%v", d, err)
		}
	})
}

func TestCosignAdmission_EvidenceBranchesAndFreshness(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	a, p, e := testPolicy(now)
	for _, tc := range []struct {
		name, field, code string
		mutate            func(*Evidence)
	}{
		{"stale policy", "policyVersion", "STALE_POLICY", func(e *Evidence) { e.PolicyVersion = "old" }},
		{"wrong source", "sourceRepository", "WRONG_SOURCE", func(e *Evidence) { e.SourceRepository = "other" }},
		{"wrong workflow", "workflowRef", "WRONG_WORKFLOW", func(e *Evidence) { e.WorkflowRef = "other" }},
		{"wrong predicate", "predicateType", "WRONG_PREDICATE", func(e *Evidence) { e.PredicateType = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := e
			tc.mutate(&changed)
			d, err := (Checker{Verifier: &fakeVerifier{evidence: changed}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
			if d.Allowed || d.Code != tc.code || d.Field != tc.field || !errors.Is(err, ErrRejected) {
				t.Fatalf("decision=%+v err=%v", d, err)
			}
		})
	}
	t.Run("verifier error is wrapped as rejection and state records one call", func(t *testing.T) {
		verifier := &fakeVerifier{err: ErrVerification}
		d, err := (Checker{Verifier: verifier, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
		if d.Allowed || d.Code != "VERIFICATION_FAILED" || d.Field != "verifier" || !errors.Is(err, ErrRejected) || verifier.calls != 1 {
			t.Fatalf("decision=%+v err=%v calls=%d", d, err, verifier.calls)
		}
	})
	t.Run("future evidence is stale while exact age is accepted", func(t *testing.T) {
		future := e
		future.AttestedAt = now.Add(time.Minute + time.Nanosecond)
		d, err := (Checker{Verifier: &fakeVerifier{evidence: future}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
		if d.Allowed || d.Code != "STALE_ATTESTATION" || !errors.Is(err, ErrRejected) {
			t.Fatalf("future decision=%+v err=%v", d, err)
		}
		exact := e
		exact.AttestedAt = now.Add(-p.MaxAttestationAge)
		d, err = (Checker{Verifier: &fakeVerifier{evidence: exact}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
		if err != nil || !d.Allowed || d.Code != "ADMITTED" {
			t.Fatalf("exact-age decision=%+v err=%v", d, err)
		}
	})
	t.Run("transparency is optional when policy does not require it", func(t *testing.T) {
		p.RequireTransparency = false
		e.TransparencyValid = false
		d, err := (Checker{Verifier: &fakeVerifier{evidence: e}, Now: func() time.Time { return now }}).Check(context.Background(), a, p)
		if err != nil || !d.Allowed {
			t.Fatalf("optional transparency decision=%+v err=%v", d, err)
		}
	})
}
