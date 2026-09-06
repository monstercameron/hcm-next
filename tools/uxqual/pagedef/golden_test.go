package pagedef

import "testing"

// TestTodo_WEB_002_Golden pins the exact Digest() of the two real Promotion
// pages, so a silent structural change to either PageDefinition -- a
// reordered region, a retyped RPC ref, a changed heading, a dropped
// landmark -- shows up as a failing digest comparison here rather than
// only being noticed the next time some other test happens to look at the
// page's shape.
func TestTodo_WEB_002_Golden(t *testing.T) {
	cases := []struct {
		name   string
		pd     PageDefinition
		digest string
	}{
		{"list", PromotionListPageDefinition(), "sha256:9d223d335352fad485c1327799b704964f20a3c5d3955b843a9116998743a28a"},
		{"detail", PromotionDetailPageDefinition(), "sha256:afca82af0138a7f9a3f7db815de034b820355f473596d1f824da3c925c25bf11"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if v := tc.pd.Validate(); len(v) != 0 {
				t.Fatalf("PromotionPageDefinition(%s).Validate() = %v, want no violations", tc.name, v)
			}
			got := tc.pd.Digest()
			if got != tc.digest {
				t.Fatalf("PromotionPageDefinition(%s).Digest() = %q, want pinned golden %q\n"+
					"(if this page definition's shape changed intentionally, update the pinned digest in this test)",
					tc.name, got, tc.digest)
			}
		})
	}

	t.Run("digests are stable across repeated construction", func(t *testing.T) {
		listFirst, listSecond := PromotionListPageDefinition().Digest(), PromotionListPageDefinition().Digest()
		if listFirst != listSecond {
			t.Fatalf("PromotionListPageDefinition().Digest() is not stable across repeated construction")
		}
		detailFirst, detailSecond := PromotionDetailPageDefinition().Digest(), PromotionDetailPageDefinition().Digest()
		if detailFirst != detailSecond {
			t.Fatalf("PromotionDetailPageDefinition().Digest() is not stable across repeated construction")
		}
	})

	t.Run("the two pages have different digests", func(t *testing.T) {
		if PromotionListPageDefinition().Digest() == PromotionDetailPageDefinition().Digest() {
			t.Fatalf("the list and detail page definitions must not collide on a digest")
		}
	})
}
