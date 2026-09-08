package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-083: page rollback and retirement. Rollout (WEB-082)
// advances scopes forward only: nothing moves a scope back to an
// older revision after an incident, and nothing ends a page's life
// — a dead page keeps serving its last rollout. The lifecycle
// needs verified rollback (a structurally valid rollout to a
// strictly older log-pinned revision over live scopes only) and
// reasoned retirement (a dated end-of-life that darkens serving),
// failing closed on cross-page, forward-or-equal, out-of-scope,
// unvalidated, and unpublished targets.
func TestTodo_WEB_083(t *testing.T) {
	var log PageRevisionLog
	snap := PageDefinitionSnapshot{Page: "studio", Route: "/workspace/app/studio"}
	first, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := log.Record(snap.Page, snap, 2)
	if err != nil {
		t.Fatal(err)
	}
	live := PageRollout{Page: "studio", Version: 2, Digest: second.Digest,
		Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-b", EffectiveFrom: 1700000000}}}
	back := PageRollout{Page: "studio", Version: 1, Digest: first.Digest,
		Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 1800000000}}}
	if err := VerifyRollbackTarget(&log, live, back); err != nil {
		t.Fatalf("valid rollback refuses: %v", err)
	}

	for _, bad := range []struct {
		name     string
		rollback PageRollout
		live     PageRollout
		want     string
	}{
		{"same version", PageRollout{Page: "studio", Version: 2, Digest: second.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}, live,
			`productui: rollback version 2 is not older than live version 2`},
		{"newer version", PageRollout{Page: "studio", Version: 3, Digest: "whatever", Scopes: []RolloutScope{{Scope: "org-a"}}}, live,
			`productui: rollback version 3 is not older than live version 2`},
		{"cross page", PageRollout{Page: "people", Version: 1, Digest: first.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}, live,
			`productui: rollback crosses pages from "studio" to "people"`},
		{"foreign scope", PageRollout{Page: "studio", Version: 1, Digest: first.Digest, Scopes: []RolloutScope{{Scope: "org-c"}}}, live,
			`productui: rollback scope "org-c" is not in the live rollout`},
		{"tampered digest", PageRollout{Page: "studio", Version: 1, Digest: "tampered", Scopes: []RolloutScope{{Scope: "org-a"}}}, live,
			`productui: rollout digest mismatch for page "studio" version 1`},
		{"phantom live", PageRollout{Page: "studio", Version: 1, Digest: first.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}},
			PageRollout{Page: "studio", Version: 9, Digest: "unknown", Scopes: []RolloutScope{{Scope: "org-a"}}},
			`productui: rollout targets unpublished revision for page "studio" version 9`},
		{"invalid structure", PageRollout{Page: "studio", Version: 1, Digest: first.Digest}, live,
			`productui: invalid rollback rollout: rollout names no scopes`},
	} {
		if err := VerifyRollbackTarget(&log, bad.live, bad.rollback); err == nil || err.Error() != bad.want {
			t.Fatalf("%s = %v, want %q", bad.name, err, bad.want)
		}
	}

	// Retirement carries a dated end-of-life with a recorded reason,
	// and an active retirement darkens serving.
	retirement := PageRetirement{Page: "studio", EffectiveFrom: 1800000000, Reason: "program ended"}
	if verdict := ValidatePageRetirement(retirement); !verdict.Compatible {
		t.Fatalf("valid retirement refuses: %q", verdict.Reasons)
	}
	for _, bad := range []struct {
		name       string
		retirement PageRetirement
		reason     string
	}{
		{"missing page", PageRetirement{EffectiveFrom: 1, Reason: "x"}, "missing retirement page"},
		{"missing reason", PageRetirement{Page: "studio"}, "missing retirement reason"},
		{"negative date", PageRetirement{Page: "studio", EffectiveFrom: -1, Reason: "x"}, "negative retirement date"},
	} {
		verdict := ValidatePageRetirement(bad.retirement)
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
	if err := VerifyRetirementTarget(&log, retirement); err != nil {
		t.Fatalf("published page retirement refuses: %v", err)
	}
	if err := VerifyRetirementTarget(&log, PageRetirement{Page: "nowhere", Reason: "x"}); err == nil {
		t.Fatal("unpublished page retires")
	}
	if RetirementActiveAt(retirement, 1799999999) || !RetirementActiveAt(retirement, 1800000000) {
		t.Fatal("retirement boundary mishandled")
	}
	if !RolloutServableAt(live, nil, "org-a", 1700000001) {
		t.Fatal("unretired live rollout does not serve")
	}
	if RolloutServableAt(live, &retirement, "org-a", 1800000001) {
		t.Fatal("retired rollout still serves")
	}
	if !RolloutServableAt(live, &retirement, "org-a", 1799999999) {
		t.Fatal("pre-retirement rollout dark early")
	}
	other := PageRetirement{Page: "people", Reason: "x"}
	if !RolloutServableAt(live, &other, "org-a", 1800000001) {
		t.Fatal("foreign retirement darkens the page")
	}
}

// Golden: rollback verification outcomes and retirement serving
// answers over a target matrix.
func TestTodo_WEB_083_Golden(t *testing.T) {
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
	live := PageRollout{Page: "studio", Version: 2, Digest: second.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}
	rollbacks := []PageRollout{
		{Page: "studio", Version: 1, Digest: first.Digest, Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 1800000000}}},
		{Page: "studio", Version: 2, Digest: second.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}},
		{Page: "people", Version: 1, Digest: first.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}},
		{Page: "studio", Version: 1, Digest: "tampered", Scopes: []RolloutScope{{Scope: "org-a"}}},
		{Page: "studio", Version: 1, Digest: first.Digest},
	}
	retirements := []*PageRetirement{
		nil,
		{Page: "studio", EffectiveFrom: 1800000000, Reason: "program ended"},
		{Page: "people", Reason: "other ended"},
	}
	var builder strings.Builder
	for _, rollback := range rollbacks {
		if err := VerifyRollbackTarget(&log, live, rollback); err != nil {
			builder.WriteString("refused")
			builder.WriteString("\x00")
			builder.WriteString(err.Error())
		} else {
			builder.WriteString("verified")
		}
		builder.WriteString("\x00")
		for _, retirement := range retirements {
			for _, moment := range []int64{1799999999, 1800000000} {
				if RolloutServableAt(rollback, retirement, "org-a", moment) {
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
	const want = "d95f74747403e3ce3a3fc71d21d0b9e125e022e6694817d6277ab6bcc870c663"
	if got != want {
		t.Fatalf("rollback digest = %s, want %s", got, want)
	}
}

// Browser: every registered page rolls a two-revision log back and
// retires — deterministically.
func TestTodo_WEB_083_Browser(t *testing.T) {
	for _, definition := range PageDefinitions() {
		var log PageRevisionLog
		snap := PageDefinitionSnapshot{Page: definition.ID, Route: "/workspace/app/catalog"}
		first, err := log.Record(snap.Page, snap, 1)
		if err != nil {
			t.Fatal(err)
		}
		second, err := log.Record(snap.Page, snap, 2)
		if err != nil {
			t.Fatal(err)
		}
		live := PageRollout{Page: definition.ID, Version: 2, Digest: second.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}
		back := PageRollout{Page: definition.ID, Version: 1, Digest: first.Digest, Scopes: []RolloutScope{{Scope: "org-a", EffectiveFrom: 1800000000}}}
		if err := VerifyRollbackTarget(&log, live, back); err != nil {
			t.Fatalf("page %q rollback refuses: %v", definition.ID, err)
		}
		retirement := &PageRetirement{Page: definition.ID, EffectiveFrom: 1900000000, Reason: "catalog end"}
		if verdict := ValidatePageRetirement(*retirement); !verdict.Compatible {
			t.Fatalf("page %q retirement refuses: %q", definition.ID, verdict.Reasons)
		}
		if err := VerifyRetirementTarget(&log, *retirement); err != nil {
			t.Fatalf("page %q retirement target refuses: %v", definition.ID, err)
		}
		firstRun := []bool{RolloutServableAt(live, nil, "org-a", 0), RolloutServableAt(live, retirement, "org-a", 1899999999), RolloutServableAt(live, retirement, "org-a", 1900000000)}
		secondRun := []bool{RolloutServableAt(live, nil, "org-a", 0), RolloutServableAt(live, retirement, "org-a", 1899999999), RolloutServableAt(live, retirement, "org-a", 1900000000)}
		if !reflect.DeepEqual(firstRun, []bool{true, true, false}) || !reflect.DeepEqual(firstRun, secondRun) {
			t.Fatalf("page %q serving answers = %v", definition.ID, firstRun)
		}
	}
}

// Conformance: retirement lineages accumulate per page, rollback
// never invents revisions, verdict stability.
func TestTodo_WEB_083_Conformance(t *testing.T) {
	var log PageRevisionLog
	snap := PageDefinitionSnapshot{Page: "studio"}
	recorded, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	live := PageRollout{Page: "studio", Version: 1, Digest: recorded.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}
	// Rolling back the only revision has nowhere to go.
	older := PageRollout{Page: "studio", Version: 0, Digest: recorded.Digest, Scopes: []RolloutScope{{Scope: "org-a"}}}
	if err := VerifyRollbackTarget(&log, live, older); err == nil {
		t.Fatal("rollback below version 1 verifies")
	}
	// Retirement reasons are retained evidence: blank reasons refuse
	// even for published pages.
	if verdict := ValidatePageRetirement(PageRetirement{Page: "studio", Reason: "   "}); verdict.Compatible {
		t.Fatal("whitespace reason retires")
	}
	verdict := ValidatePageRetirement(PageRetirement{Page: "studio", Reason: "x"})
	again := ValidatePageRetirement(PageRetirement{Page: "studio", Reason: "x"})
	if !reflect.DeepEqual(verdict, again) {
		t.Fatal("retirement verdict is unstable")
	}
}
