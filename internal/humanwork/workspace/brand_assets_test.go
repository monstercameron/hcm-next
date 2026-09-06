package workspace

import (
	"strings"
	"testing"
)

func TestCustomerLogoIsAnExplicitSafeWorkspaceAsset(t *testing.T) {
	body, ok := asset(assetHarborcareLogo)
	if !ok {
		t.Fatal("allowlisted customer logo is not embedded")
	}
	if got := assetContentType(assetHarborcareLogo); got != "image/svg+xml" {
		t.Fatalf("logo content type = %q", got)
	}
	markup := string(body)
	if !strings.Contains(markup, "<svg") || !strings.Contains(markup, "Harborcare") {
		t.Fatal("embedded customer logo does not contain the expected accessible identity")
	}
	for _, forbidden := range []string{"<script", "javascript:", `href="http://`, `href="https://`} {
		if strings.Contains(strings.ToLower(markup), forbidden) {
			t.Fatalf("embedded customer logo contains forbidden content %q", forbidden)
		}
	}
}
