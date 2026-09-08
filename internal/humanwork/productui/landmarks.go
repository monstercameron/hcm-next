package productui

// ShellLandmark is one named ARIA landmark the shell resolves for a view:
// the implicit banner/contentinfo regions stay unnamed by design, while
// every navigation region and the main region carry localized names.
type ShellLandmark struct {
	Role  string
	Label string
}

// ResolveShellLandmarks inventories the named landmarks the shell renders
// for a view, in document order. It reads the same sources the renderers
// do — navigation projection, breadcrumb visibility, page identity — so
// the inventory cannot drift from the document it describes.
func ResolveShellLandmarks(view View) []ShellLandmark {
	landmarks := []ShellLandmark{
		{Role: "complementary", Label: view.Locale.Text("nav.workspace")},
		{Role: "navigation", Label: view.Locale.Text("nav.main")},
	}
	if len(view.NavigationSupport) > 0 {
		landmarks = append(landmarks, ShellLandmark{Role: "navigation", Label: view.Locale.Text("nav.support")})
	}
	if showBreadcrumbTrail(view, ResolveBreadcrumbs(view)) {
		landmarks = append(landmarks, ShellLandmark{Role: "navigation", Label: view.Locale.Text("shell.breadcrumbs")})
	}
	landmarks = append(landmarks, ShellLandmark{Role: "main", Label: ResolvePageIdentity(view).Title})
	return landmarks
}

// drawerEscapeCloses reports whether a key press closes an open shell
// dialog. One predicate owns the contract so the search, launcher, and
// drawer surfaces cannot drift apart key by key.
func drawerEscapeCloses(key string) bool {
	return key == "Escape"
}
