package sbom

import "testing"

func TestSplitModAt(t *testing.T) {
	cases := []struct {
		node        string
		wantPath    string
		wantVersion string
	}{
		{"example.com/mod@v1.2.3", "example.com/mod", "v1.2.3"},
		{"example.com/mainmodule", "example.com/mainmodule", ""},
		{"golang.org/x/mod/v2@v2.0.0", "golang.org/x/mod/v2", "v2.0.0"},
	}
	for _, tc := range cases {
		path, version := splitModAt(tc.node)
		if path != tc.wantPath || version != tc.wantVersion {
			t.Errorf("splitModAt(%q) = (%q, %q), want (%q, %q)", tc.node, path, version, tc.wantPath, tc.wantVersion)
		}
	}
}

func TestPurl(t *testing.T) {
	got := purl("github.com/monstercameron/hcm-next", "v1.0.0")
	want := "pkg:golang/github.com/monstercameron/hcm-next@v1.0.0"
	if got != want {
		t.Errorf("purl = %q, want %q", got, want)
	}
}

func TestDeterministicSerial_StableAndUUIDShaped(t *testing.T) {
	components := []Component{
		{Name: "a", Version: "v1.0.0"},
		{Name: "b", Version: "v2.0.0"},
	}
	first := deterministicSerial("mod", "v0.0.0-devel", components)
	second := deterministicSerial("mod", "v0.0.0-devel", components)
	if first != second {
		t.Fatalf("deterministicSerial is not stable across calls: %q != %q", first, second)
	}
	if len(first) != len("urn:uuid:")+36 {
		t.Errorf("deterministicSerial = %q, wrong length for a UUID URN", first)
	}
	if first[:9] != "urn:uuid:" {
		t.Errorf("deterministicSerial = %q, want urn:uuid: prefix", first)
	}

	changed := deterministicSerial("mod", "v0.0.1", components)
	if changed == first {
		t.Errorf("deterministicSerial did not change when root version changed")
	}
}

func TestOptionsDefaults(t *testing.T) {
	var o Options
	if o.rootVersion() != DefaultRootVersion {
		t.Errorf("rootVersion() = %q, want %q", o.rootVersion(), DefaultRootVersion)
	}
	if o.generatorVersion() != "dev" {
		t.Errorf("generatorVersion() = %q, want dev", o.generatorVersion())
	}
	if o.now().IsZero() {
		t.Errorf("now() returned zero time")
	}

	o = Options{RootVersion: "v9.9.9", GeneratorVersion: "1.2.3"}
	if o.rootVersion() != "v9.9.9" {
		t.Errorf("rootVersion() override not honored: %q", o.rootVersion())
	}
	if o.generatorVersion() != "1.2.3" {
		t.Errorf("generatorVersion() override not honored: %q", o.generatorVersion())
	}
}
