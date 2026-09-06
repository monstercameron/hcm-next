package pipeline

import (
	"testing"

	legal "github.com/monstercameron/hcm-next/internal/governance/legal"
)

// TestTodo_LEGAL_015_Golden pins the exact digests LEGAL-015 requires for a
// fixture pack: the Washington minimum-wage schedule (RCW 49.46.020), a real
// state-law example rather than a synthetic one. Every digest below is
// content-addressed over a fixed dev signer key (see [fixedSigner]) and a
// fixed set of principal ids, so any change to the canonical encoding of a
// draft candidate, a [legal.ReviewRecord], a [legal.PackRelease], or a
// pipeline [Event] shows up here as a diff against a literal, not as a
// silent pass.
func TestTodo_LEGAL_015_Golden(t *testing.T) {
	const (
		wantDraftDigest   = "eef50b47ac0b90acba653dc35f61111c2d314312928ceeaf37acf54540439f9c"
		wantReviewDigest  = "7e57dc2cbbdc17f98288574a493aaac9aa5abe0a7a9db0c85f978ee0fd5fd400"
		wantReleaseDigest = "45a931e27007b90f343a47f9c5fac5e209a7673378578942996c16e7605105a1"
		wantEvent0Digest  = "04aae3c1f9fb91e77341c7a8a2b6f089f8a2244da616862e95c30f4f5db6cb64"
		wantEvent1Digest  = "a1b75c40ad57fe5d1cd0e941a0ca276cd23ea484d0c35e2b07673d6c198c3ed1"
		wantEvent2Digest  = "4724dc750a1c658d346aeacfabe421da92e9cec43caef3807ffdf524c46dfc00"
	)

	p, err := Author(waMinimumWageDefinitionJSON(), "author-alice", fixedSigner(t, 0x01))
	if err != nil {
		t.Fatalf("Author: %v", err)
	}
	if p.Events[0].ArtifactDigest != wantDraftDigest {
		t.Errorf("draft digest = %s, want golden %s", p.Events[0].ArtifactDigest, wantDraftDigest)
	}
	if p.Events[0].Digest != wantEvent0Digest {
		t.Errorf("AUTHORED event digest = %s, want golden %s", p.Events[0].Digest, wantEvent0Digest)
	}

	findings := []legal.ReviewFinding{
		{ObligationID: "us-wa-minimum-wage-floor", Severity: legal.FindingSeverityInfo, Note: "confirmed against RCW 49.46.020 text"},
	}
	if err := p.Review("reviewer-bob", legal.ReviewStatusVendorBaseline, findings, fixedSigner(t, 0x02)); err != nil {
		t.Fatalf("Review: %v", err)
	}
	if p.ReviewRecord.ComputeDigest() != wantReviewDigest {
		t.Errorf("review record digest = %s, want golden %s", p.ReviewRecord.ComputeDigest(), wantReviewDigest)
	}
	if p.Events[1].Digest != wantEvent1Digest {
		t.Errorf("REVIEWED event digest = %s, want golden %s", p.Events[1].Digest, wantEvent1Digest)
	}

	registry := legal.NewRegistry()
	release, err := p.Publish("publisher-carol", fixedSigner(t, 0x03), "", nil, registry)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if release.Digest != wantReleaseDigest {
		t.Errorf("release digest = %s, want golden %s", release.Digest, wantReleaseDigest)
	}
	if p.Events[2].Digest != wantEvent2Digest {
		t.Errorf("PUBLISHED event digest = %s, want golden %s", p.Events[2].Digest, wantEvent2Digest)
	}

	if err := p.VerifyChain(); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}

	t.Logf("draft=%s review=%s release=%s event0=%s event1=%s event2=%s",
		p.Events[0].ArtifactDigest, p.ReviewRecord.ComputeDigest(), release.Digest,
		p.Events[0].Digest, p.Events[1].Digest, p.Events[2].Digest)
}
