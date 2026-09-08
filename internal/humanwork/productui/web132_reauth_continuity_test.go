package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-132: interruption and recovery continuity.
// The session warning carries the current address on its
// re-authentication link so signing in again restores the
// user's place, but the resume-target policy lives inline
// in the view-coupled adapter: the first surface
// re-implements which destinations accept a resume target
// by convention and an external URL can gain a resume
// parameter. The compiler needs the governed policy —
// workspace destinations take the target, everything else
// passes through untouched — so recovery continuity
// resolves today from one point.
func TestTodo_WEB_132(t *testing.T) {
	resumed := ResolveResumeHref("/workspace/sign-in", "/workspace/people?team=retail")
	if !strings.Contains(resumed, "resume=") {
		t.Fatalf("workspace destination loses its target: %q", resumed)
	}
	if !strings.Contains(resumed, "/workspace%2Fpeople") && !strings.Contains(resumed, "resume=%2Fworkspace") {
		t.Fatalf("resume target misencoded: %q", resumed)
	}

	for _, base := range []string{
		"https://accounts.example.com/sign-in",
		"//accounts.example.com/sign-in",
		"/sign-in",
		"/other/sign-in",
		"  ",
		"http://[::1]:namedport",
	} {
		if got := ResolveResumeHref(base, "/workspace/people"); got != base {
			t.Fatalf("unsafe destination rewritten: %q -> %q", base, got)
		}
	}
}

// Golden: resume hrefs over destination/target pairs.
func TestTodo_WEB_132_Golden(t *testing.T) {
	bases := []string{
		"/workspace/sign-in",
		"/workspace/sign-in?mode=sso",
		"/workspace/sign-in?resume=%2Fworkspace%2Fold",
		"https://accounts.example.com/sign-in",
		"/sign-in",
		"",
	}
	targets := []string{"/workspace/people", "/workspace/", ""}
	var builder strings.Builder
	for _, base := range bases {
		for _, target := range targets {
			fmt.Fprintf(&builder, "%s|%s\x00", base, ResolveResumeHref(base, target))
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "2a750f5ae2e09c98fafe07841d394062b8b574072e62ed0dc23459e8e5ac7990"
	if got != want {
		t.Fatalf("resume digest = %s, want %s", got, want)
	}
}

// Browser: resolution is deterministic and pure.
func TestTodo_WEB_132_Browser(t *testing.T) {
	pairs := [][2]string{
		{"/workspace/sign-in", "/workspace/people"},
		{"/workspace/sign-in?a=b", "/workspace/"},
		{"https://x.example/s", "/workspace/people"},
	}
	for _, pair := range pairs {
		first := ResolveResumeHref(pair[0], pair[1])
		second := ResolveResumeHref(pair[0], pair[1])
		if first != second {
			t.Fatal("resolution is nondeterministic")
		}
	}
}

// Conformance: existing queries survive, an existing
// resume target is replaced, blank targets still attach.
func TestTodo_WEB_132_Conformance(t *testing.T) {
	kept := ResolveResumeHref("/workspace/sign-in?mode=sso", "/workspace/people")
	if !strings.Contains(kept, "mode=sso") || !strings.Contains(kept, "resume=") {
		t.Fatalf("query lost: %q", kept)
	}
	replaced := ResolveResumeHref("/workspace/sign-in?resume=%2Fworkspace%2Fold", "/workspace/new")
	if strings.Contains(replaced, "Old") || strings.Contains(replaced, "%2Fold") {
		t.Fatalf("stale target kept: %q", replaced)
	}
	if !strings.Contains(replaced, "resume=") {
		t.Fatalf("replacement lost: %q", replaced)
	}
	blank := ResolveResumeHref("/workspace/sign-in", "")
	if !strings.Contains(blank, "resume=") {
		t.Fatalf("blank target unattaches: %q", blank)
	}
	stable := ResolveResumeHref("/workspace/sign-in", "/workspace/people")
	if stable != ResolveResumeHref("/workspace/sign-in", "/workspace/people") {
		t.Fatal("resolution is unstable")
	}
}

// Integration: the warning adapter's rule still holds —
// workspace bases gain the shell's current address while
// foreign bases pass through.
func TestTodo_WEB_132_Integration(t *testing.T) {
	current := "/workspace/people?team=retail"
	gained := ResolveResumeHref("/workspace/sign-in", current)
	if !strings.Contains(gained, "resume=") {
		t.Fatalf("adapter rule lost: %q", gained)
	}
	foreign := "https://accounts.example.com/sign-in"
	if got := ResolveResumeHref(foreign, current); got != foreign {
		t.Fatalf("foreign base rewritten: %q", got)
	}
}

// Fault: unparsable bases, blank bases, and scheme
// tricks pass through untouched.
func TestTodo_WEB_132_Fault(t *testing.T) {
	for _, base := range []string{"", "   ", "http://[::1]:namedport", "javascript:alert(1)"} {
		if got := ResolveResumeHref(base, "/workspace/people"); got != base {
			t.Fatalf("fault base rewritten: %q -> %q", base, got)
		}
	}
}
