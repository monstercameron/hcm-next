package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-082: scoped page publication rollout. Published
// revisions (WEB-073/074) are all-or-nothing today: nothing binds a
// revision to the scopes that receive it or the date each scope
// goes live, so staged publication is caller convention. The studio
// needs a pure rollout — one pinned revision, target scopes with
// effective dates, a structural verdict, log-verified targeting,
// and a mechanical live-at query — failing closed on unpinned,
// unscoped, duplicated, or backdated targets. Rollback and
// retirement belong to WEB-083; rollout never moves a scope
// backward itself.
func TestTodo_WEB_082(t *testing.T) {
	rollout := PageRollout{Page: "studio", Version: 2, Digest: "abc123",
		Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-b", EffectiveFrom: 1700000000}}}
	if verdict := ValidatePageRollout(rollout); !verdict.Compatible {
		t.Fatalf("valid rollout refuses: %q", verdict.Reasons)
	}
	if !RolloutLiveAt(rollout, "org-a", 0) {
		t.Fatal("undated scope is not live at publication")
	}
	if RolloutLiveAt(rollout, "org-b", 1699999999) || !RolloutLiveAt(rollout, "org-b", 1700000000) {
		t.Fatal("effective date boundary mishandled")
	}
	if RolloutLiveAt(rollout, "org-c", 1800000000) {
		t.Fatal("unlisted scope is live")
	}

	for _, bad := range []struct {
		name    string
		rollout PageRollout
		reason  string
	}{
		{"missing page", PageRollout{Version: 1, Digest: "d", Scopes: []RolloutScope{{Scope: "org-a"}}}, "missing rollout page"},
		{"bad version", PageRollout{Page: "studio", Digest: "d", Scopes: []RolloutScope{{Scope: "org-a"}}}, "rollout revision version must be positive, got 0"},
		{"missing digest", PageRollout{Page: "studio", Version: 1, Scopes: []RolloutScope{{Scope: "org-a"}}}, "missing rollout digest"},
		{"no scopes", PageRollout{Page: "studio", Version: 1, Digest: "d"}, "rollout names no scopes"},
		{"blank scope", PageRollout{Page: "studio", Version: 1, Digest: "d", Scopes: []RolloutScope{{Scope: ""}}}, "blank rollout scope"},
		{"duplicate scope", PageRollout{Page: "studio", Version: 1, Digest: "d", Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-a"}}}, `duplicate rollout scope "org-a"`},
		{"negative date", PageRollout{Page: "studio", Version: 1, Digest: "d", Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: -1}}}, `negative effective date for scope "org-a"`},
	} {
		verdict := ValidatePageRollout(bad.rollout)
		if verdict.Compatible {
			t.Fatalf("%s validates", bad.name)
		}
		found := false
		for _, reason := range verdict.Reasons {
			if reason == bad.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s reasons = %q, want %q", bad.name, verdict.Reasons, bad.reason)
		}
	}

	// Rollout targets bind actually-published revisions: the digest
	// must match the revision log, and unknown or tampered targets
	// refuse.
	var log PageRevisionLog
	snap := PageDefinitionSnapshot{Page: "studio", Route: "/workspace/app/studio"}
	recorded, err := log.Record(snap.Page, snap, 2)
	if err != nil {
		t.Fatal(err)
	}
	verified := PageRollout{Page: "studio", Version: 2, Digest: recorded.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}
	if err := VerifyRolloutTarget(&log, verified); err != nil {
		t.Fatalf("published target refuses: %v", err)
	}
	for _, bad := range []struct {
		name    string
		rollout PageRollout
		want    string
	}{
		{"unknown version", PageRollout{Page: "studio", Version: 3, Digest: recorded.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}, `productui: rollout targets unpublished revision for page "studio" version 3`},
		{"tampered digest", PageRollout{Page: "studio", Version: 2, Digest: "tampered", Scopes: []RolloutScope{{Scope: "org-a"}}}, `productui: rollout digest mismatch for page "studio" version 2`},
		{"unknown page", PageRollout{Page: "nowhere", Version: 2, Digest: recorded.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}, `productui: rollout targets unpublished revision for page "nowhere" version 2`},
	} {
		if err := VerifyRolloutTarget(&log, bad.rollout); err == nil || err.Error() != bad.want {
			t.Fatalf("%s = %v, want %q", bad.name, err, bad.want)
		}
	}
}

// Golden: rollout verdicts and live-at answers over a target
// matrix.
func TestTodo_WEB_082_Golden(t *testing.T) {
	rollouts := []PageRollout{
		{Page: "studio", Version: 2, Digest: "abc123", Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-b", EffectiveFrom: 1700000000}}},
		{Page: "studio", Version: 0, Digest: "abc123", Scopes: []RolloutScope{{Scope: "org-a"}}},
		{Page: "studio", Version: 1, Scopes: []RolloutScope{{Scope: "org-a"}}},
		{Page: "studio", Version: 1, Digest: "abc123", Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-a", EffectiveFrom: 5}}},
		{Page: "studio", Version: 1, Digest: "abc123", Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: -2}}},
		{Page: "studio", Version: 1, Digest: "abc123"},
	}
	moments := []int64{0, 1699999999, 1700000000}
	var builder strings.Builder
	for _, rollout := range rollouts {
		verdict := ValidatePageRollout(rollout)
		if verdict.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join(verdict.Reasons, ";"))
		builder.WriteString("\x00")
		for _, moment := range moments {
			for _, scope := range []string{"org-a", "org-b"} {
				if RolloutLiveAt(rollout, scope, moment) {
					builder.WriteString("live")
				} else {
					builder.WriteString("dark")
				}
				builder.WriteString("\x00")
			}
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "77857608468c5a477e15be6d85365a41c6acf36eb9a57f57e307632083294e5b"
	if got != want {
		t.Fatalf("rollout digest = %s, want %s", got, want)
	}
}

// Browser: every registered page carries a two-scope rollout that
// validates and answers live-at deterministically.
func TestTodo_WEB_082_Browser(t *testing.T) {
	for _, definition := range PageDefinitions() {
		rollout := PageRollout{Page: definition.ID, Version: 1, Digest: "catalog",
			Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-b", EffectiveFrom: 1700000000}}}
		if verdict := ValidatePageRollout(rollout); !verdict.Compatible {
			t.Fatalf("page %q rollout refuses: %q", definition.ID, verdict.Reasons)
		}
		first := []bool{RolloutLiveAt(rollout, "org-a", 0), RolloutLiveAt(rollout, "org-b", 1699999999), RolloutLiveAt(rollout, "org-b", 1700000000)}
		second := []bool{RolloutLiveAt(rollout, "org-a", 0), RolloutLiveAt(rollout, "org-b", 1699999999), RolloutLiveAt(rollout, "org-b", 1700000000)}
		if !reflect.DeepEqual(first, []bool{true, false, true}) || !reflect.DeepEqual(first, second) {
			t.Fatalf("page %q live-at answers = %v", definition.ID, first)
		}
	}
}

// Conformance: scope isolation, revision binding across versions,
// verdict stability.
func TestTodo_WEB_082_Conformance(t *testing.T) {
	rollout := PageRollout{Page: "studio", Version: 2, Digest: "abc123",
		Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 100}, {Scope: "org-b", EffectiveFrom: 200}}}
	if RolloutLiveAt(rollout, "org-a", 150) == RolloutLiveAt(rollout, "org-b", 150) {
		t.Fatal("scope dates leak across scopes")
	}
	var log PageRevisionLog
	snap := PageDefinitionSnapshot{Page: "studio"}
	first, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := log.Record(snap.Page, snap, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRolloutTarget(&log, PageRollout{Page: "studio", Version: 1, Digest: first.Digest}); err != nil {
		t.Fatalf("version 1 target refuses: %v", err)
	}
	if err := VerifyRolloutTarget(&log, PageRollout{Page: "studio", Version: 2, Digest: second.Digest}); err != nil {
		t.Fatalf("version 2 target refuses: %v", err)
	}
	// A digest from one version never verifies another.
	if err := VerifyRolloutTarget(&log, PageRollout{Page: "studio", Version: 2, Digest: first.Digest}); err == nil {
		t.Fatal("cross-version digest verifies")
	}
	verdict := ValidatePageRollout(rollout)
	again := ValidatePageRollout(rollout)
	if !reflect.DeepEqual(verdict, again) {
		t.Fatal("rollout verdict is unstable")
	}
}
