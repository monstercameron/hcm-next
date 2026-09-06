package uicomponents

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestInitialsAreStableAcrossFrontendSurfaces(t *testing.T) {
	for name, want := range map[string]string{
		"Jane Doe": "JD",
		"Priya":    "P",
		" Éva  王 ": "É王",
		"":         "?",
	} {
		if got := Initials(name); got != want {
			t.Errorf("Initials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestAvatarUsesTheShellClassAndAccessibleName(t *testing.T) {
	node := Avatar(AvatarProps{Name: "Jane Doe", Class: "avatar profile"})
	out, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`aria-label="Jane Doe"`, `class="avatar profile"`, `>JD<`} {
		if !strings.Contains(out, want) {
			t.Fatalf("avatar missing %q: %s", want, out)
		}
	}
}

func TestDecorativeAvatarIsHiddenFromAssistiveTechnology(t *testing.T) {
	out, err := ui.RenderToString(Avatar(AvatarProps{Name: "Jane Doe", Class: "jn-subject-avatar", Decorative: true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `aria-hidden="true"`) || strings.Contains(out, `aria-label=`) {
		t.Fatalf("decorative avatar accessibility = %s", out)
	}
}

func TestAvatarRendersPhotoWithAccessibleFallbackContract(t *testing.T) {
	out, err := ui.RenderToString(Avatar(AvatarProps{
		Name: "Priya Shah", PhotoURL: "/workspace/assets/person-priya.png", Class: "avatar profile",
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<img`, `src="/workspace/assets/person-priya.png"`, `alt="Priya Shah"`,
		`class="avatar profile"`, `loading="lazy"`, `decoding="async"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("photo avatar missing %q: %s", want, out)
		}
	}
}
