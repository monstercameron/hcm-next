package dsr

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PRIV_005_Property is the PROPERTY matrix test for PRIV-005:
// digest determinism. [DataSubjectRequest.Digest] must be a pure function of
// the record's own fields -- two independently-built records with identical
// field values always digest identically, and changing any single field
// that [DataSubjectRequest.canonicalBytes] claims to cover always changes
// the digest. The same two properties are checked for
// [SubjectClaims.canonicalBytes] through the request that embeds it.
func TestTodo_PRIV_005_Property(t *testing.T) {
	t.Run("two independently-built identical requests digest identically", func(t *testing.T) {
		a := fixtureRequest(t)
		b := fixtureRequest(t) // built from scratch again, not a copy of a
		if a.Digest() != b.Digest() {
			t.Fatalf("independently-built identical requests digest differently: %s vs %s", a.Digest(), b.Digest())
		}
		if a.EvidenceID != b.EvidenceID {
			t.Fatalf("independently-built identical requests have different evidence ids: %s vs %s", a.EvidenceID, b.EvidenceID)
		}
	})

	t.Run("Digest changes when any single covered field changes", func(t *testing.T) {
		base := fixtureRequest(t)
		baseDigest := base.Digest()

		mutants := map[string]DataSubjectRequest{
			"id":           withID(base, base.ID+"-x"),
			"tenant":       withTenant(base, fxOtherTenant),
			"kind":         withKind(base, KindErasure),
			"jurisdiction": withJurisdiction(base, legal.Jurisdiction{Country: "US", State: "NY"}),
			"channel":      withChannel(base, ChannelEmail),
			"claims_email": withClaimsEmail(base, base.Claims.Email+"-x"),
			"claims_name":  withClaimsName(base, base.Claims.FullName+"-x"),
			"duplicate_of": withDuplicateOf(base, "some-other-request"),
		}
		for name, mutant := range mutants {
			if mutant.Digest() == baseDigest {
				t.Errorf("mutating %s did not change Digest (mutant indistinguishable from base)", name)
			}
		}
	})

	t.Run("verification transitions change Digest and are reflected in EvidenceID", func(t *testing.T) {
		req := fixtureRequest(t)
		before := req.Digest()

		verified, err := req.Verify(fixtureEvidence(t, AssuranceFloor(KindAccess)))
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if verified.Digest() == before {
			t.Error("Verify did not change Digest")
		}
		if verified.EvidenceID != requestEvidencePrefix+verified.Digest() {
			t.Error("EvidenceID does not match the post-verification digest")
		}
	})
}

func withID(r DataSubjectRequest, v string) DataSubjectRequest              { r.ID = v; return r }
func withTenant(r DataSubjectRequest, v values.TenantId) DataSubjectRequest { r.Tenant = v; return r }
func withKind(r DataSubjectRequest, v Kind) DataSubjectRequest              { r.Kind = v; return r }
func withJurisdiction(r DataSubjectRequest, v legal.Jurisdiction) DataSubjectRequest {
	r.Jurisdiction = v
	return r
}
func withChannel(r DataSubjectRequest, v Channel) DataSubjectRequest { r.Channel = v; return r }
func withClaimsEmail(r DataSubjectRequest, v string) DataSubjectRequest {
	r.Claims.Email = v
	return r
}
func withClaimsName(r DataSubjectRequest, v string) DataSubjectRequest {
	r.Claims.FullName = v
	return r
}
func withDuplicateOf(r DataSubjectRequest, v string) DataSubjectRequest { r.DuplicateOf = v; return r }
