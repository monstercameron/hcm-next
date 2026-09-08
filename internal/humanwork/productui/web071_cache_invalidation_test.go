package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-071: invalidate presentation caches on authority drift.
// Two client caches never invalidate. The roles permission row seeds
// its grant toggles from props once: when the projection drifts (a
// version-bumped grant, a changed grant, another role), the editor
// keeps displaying — and saving — the stale draft. The break-glass
// prompt keys dismissal to nothing: dismissing one incident hides the
// next emergency too. Both caches must reconcile against the incoming
// authority snapshot: local edits survive re-renders, drift resets.
func TestTodo_WEB_071(t *testing.T) {
	seed := RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Create: true}
	seeded := rolePermissionDraftState{key: permissionSnapshot(seed), view: false, create: false, update: true, remove: true}
	for _, drift := range []struct {
		name     string
		incoming RolePagePermission
		current  rolePermissionDraftState
		want     rolePermissionDraftState
		reset    bool
	}{
		{"local edits survive re-renders", seed, seeded,
			rolePermissionDraftState{key: permissionSnapshot(seed), view: false, create: false, update: true, remove: true}, false},
		{"version-bumped grants reset the draft", RolePagePermission{Version: 4, RoleID: "analyst", Page: PagePeople, View: true},
			seeded, rolePermissionDraftState{key: permissionSnapshot(RolePagePermission{Version: 4, RoleID: "analyst", Page: PagePeople, View: true}), view: true}, true},
		{"grant drift without a bump resets the draft", RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Delete: true},
			seeded, rolePermissionDraftState{key: permissionSnapshot(RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Delete: true}), view: true, remove: true}, true},
		{"another role resets the draft", RolePagePermission{Version: 3, RoleID: "manager", Page: PagePeople, View: true, Create: true},
			seeded, rolePermissionDraftState{key: permissionSnapshot(RolePagePermission{Version: 3, RoleID: "manager", Page: PagePeople, View: true, Create: true}), view: true, create: true}, true},
		{"identical projection keeps pristine state", seed,
			rolePermissionDraftState{key: permissionSnapshot(seed), view: true, create: true},
			rolePermissionDraftState{key: permissionSnapshot(seed), view: true, create: true}, false},
	} {
		got, reset := reconcileRolePermissionDraft(drift.incoming, drift.current)
		if reset != drift.reset || !reflect.DeepEqual(got, drift.want) {
			t.Fatalf("%s = (%+v, %t), want (%+v, %t)", drift.name, got, reset, drift.want, drift.reset)
		}
	}

	for _, prompt := range []struct {
		name         string
		incident     string
		dismissedRef string
		suppressed   bool
	}{
		{"no incident shows nothing", "", "", true},
		{"fresh incident shows", "INC-1", "", false},
		{"dismissed incident stays hidden", "INC-1", "INC-1", true},
		{"new incident resurfaces", "INC-2", "INC-1", false},
	} {
		if got := breakGlassSuppressed(prompt.incident, prompt.dismissedRef); got != prompt.suppressed {
			t.Fatalf("%s suppressed=%t, want %t", prompt.name, got, prompt.suppressed)
		}
	}

	// A single server render shows the incoming grants, never
	// client-only state: two checked boxes for view+create.
	markup, err := ui.RenderToString(ui.CreateElement(RolePagePermissionRow, RolePagePermissionRowProps{
		I18nProps:  I18nProps{Locale: ResolveProductLocale("en-US")},
		Page:       RolePageOption{ID: PagePeople, Label: "People"},
		Permission: RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Create: true},
		Editable:   true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	if checked := countCheckedBoxes(root); checked != 2 {
		t.Fatalf("permission row renders %d checked boxes, want 2 (view+create)", checked)
	}
}

// countCheckedBoxes counts checkbox inputs carrying the checked attribute.
func countCheckedBoxes(root *xhtml.Node) int {
	count := 0
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode && node.Data == "input" {
			checkbox, checked := false, false
			for _, attr := range node.Attr {
				if attr.Key == "type" && attr.Val == "checkbox" {
					checkbox = true
				}
				if attr.Key == "checked" {
					checked = true
				}
			}
			if checkbox && checked {
				count++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return count
}

// Golden: the draft-reconciliation matrix digest.
func TestTodo_WEB_071_Golden(t *testing.T) {
	seed := RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Create: true}
	local := rolePermissionDraftState{key: permissionSnapshot(seed), view: false, create: false, update: true, remove: true}
	incoming := []RolePagePermission{
		seed,
		{Version: 4, RoleID: "analyst", Page: PagePeople, View: true},
		{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Delete: true},
		{Version: 3, RoleID: "manager", Page: PagePeople, View: true, Create: true},
	}
	var builder strings.Builder
	for _, next := range incoming {
		draft, reset := reconcileRolePermissionDraft(next, local)
		builder.WriteString(strings.Join([]string{
			next.RoleID, string(next.Page), strings.Join([]string{grantWord(next.View), grantWord(next.Create), grantWord(next.Update), grantWord(next.Delete)}, ","),
		}, "\x00"))
		builder.WriteString("\x00")
		if reset {
			builder.WriteString("reset")
		} else {
			builder.WriteString("keep")
		}
		builder.WriteString("\x00")
		builder.WriteString(strings.Join([]string{grantWord(draft.view), grantWord(draft.create), grantWord(draft.update), grantWord(draft.remove)}, ","))
		builder.WriteString("\n")
	}
	for _, incident := range [][2]string{{"", ""}, {"INC-1", ""}, {"INC-1", "INC-1"}, {"INC-2", "INC-1"}} {
		builder.WriteString(incident[0])
		builder.WriteString("\x00")
		builder.WriteString(incident[1])
		builder.WriteString("\x00")
		if breakGlassSuppressed(incident[0], incident[1]) {
			builder.WriteString("suppressed")
		} else {
			builder.WriteString("shown")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "6769a70af3a270e078d331bb68c2a79b7444452f75c35104a003c122e23bff55"
	if got != want {
		t.Fatalf("reconciliation matrix digest = %s, want %s", got, want)
	}
}

func grantWord(granted bool) string {
	if granted {
		return "grant"
	}
	return "deny"
}

// web071RolesView builds a roles view with one permission card row.
func web071RolesView(locale string) View {
	view := testView(PageRoles)
	view.Locale = ResolveProductLocale(locale)
	view = ApplyLocale(view, view.Locale)
	view.AccessRoles = []AccessRole{{ID: "manager", Name: "People manager", Description: "Manages a team", System: true, Active: true}}
	view.RolePagePermissions = []RolePagePermission{{Version: 3, RoleID: "manager", Page: PageInsights, View: true, Create: true}}
	view.EffectivePermissions = []RolePagePermission{{Page: PageRoles, View: true, Create: true, Update: true}}
	return view
}

// Browser: the roles document parses with its permission table and no
// positive tabindex stops.
func TestTodo_WEB_071_Browser(t *testing.T) {
	doc, err := Render(web071RolesView("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if findClassNode(root, "role-page-table") == nil {
		t.Fatal("roles document loses the permission table")
	}
	var positive int
	var walk func(node *xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			for _, attr := range node.Attr {
				if attr.Key == "tabindex" && strings.TrimSpace(attr.Val) != "" && attr.Val != "0" && !strings.HasPrefix(attr.Val, "-") {
					positive++
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if positive != 0 {
		t.Fatalf("roles document carries %d positive tabindex stops", positive)
	}
}

// Conformance: no unresolved copy in three locales, determinism.
func TestTodo_WEB_071_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := web071RolesView(locale)
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("%s roles render leaks an unresolved key", locale)
		}
	}
	seed := RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Create: true}
	local := rolePermissionDraftState{key: permissionSnapshot(seed)}
	first, firstReset := reconcileRolePermissionDraft(seed, local)
	second, secondReset := reconcileRolePermissionDraft(seed, local)
	if firstReset != secondReset || !reflect.DeepEqual(first, second) {
		t.Fatal("draft reconciliation is nondeterministic")
	}
}

// Security: a drifted projection can never save through a stale draft —
// the reset draft carries exactly the incoming grants — and a fresh
// incident is never suppressed by an old dismissal.
func TestTodo_WEB_071_Security(t *testing.T) {
	seed := RolePagePermission{Version: 3, RoleID: "analyst", Page: PagePeople, View: true, Create: true}
	stale := rolePermissionDraftState{key: permissionSnapshot(seed), view: true, create: true, update: true, remove: true}
	revoked := RolePagePermission{Version: 4, RoleID: "analyst", Page: PagePeople, View: true}
	draft, reset := reconcileRolePermissionDraft(revoked, stale)
	if !reset {
		t.Fatal("revoked projection keeps the stale draft")
	}
	if draft.view != true || draft.create || draft.update || draft.remove {
		t.Fatalf("reset draft carries stale grants: %+v", draft)
	}
	if breakGlassSuppressed("INC-2026-119", "INC-2026-118") {
		t.Fatal("new incident suppressed by an old dismissal")
	}
}
