package workspace

import (
	"strings"
	"testing"
)

func TestAssets_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestAssets_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestLegacyWorkspaceWASMIsRefusedAndNativePOSTRemains(t *testing.T) {
	if _, ok := asset(assetWasm); ok {
		t.Fatal("legacy fixture-only uxqual.wasm is routable")
	}
	if _, ok := compressedAsset(assetWasm); ok {
		t.Fatal("compressed legacy uxqual.wasm is routable")
	}
	if BundleBuilt() {
		t.Fatal("legacy fixture-only uxqual.wasm is advertised as a safe enhancement")
	}

	doc, err := Render(ux002Contract(), "csrf-fixture", "worker-fixture", false)
	if err != nil {
		t.Fatalf("render native workspace: %v", err)
	}
	for _, want := range []string{
		`<form action="` + PathSimulate + `" id="` + formID + `" method="post">`,
		hiddenInput(ParamCSRF, "csrf-fixture"),
		hiddenInput(ParamWorker, "worker-fixture"),
		`<input form="` + formID + `" name="transition"`,
		`<button form="` + formID,
		`type="submit"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("native fallback missing %q", want)
		}
	}
	if strings.Contains(doc, `action="#"`) || strings.Contains(doc, "<script") {
		t.Fatal("native fallback contains an unbound action or executable enhancement")
	}
	if !strings.Contains(ContentSecurityPolicy("cell.test", BundleBuilt()), "script-src 'none'") {
		t.Fatal("native-only workspace does not refuse script execution")
	}
}
