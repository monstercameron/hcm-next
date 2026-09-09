package buildinfo

import "testing"

func TestCurrent(t *testing.T) {
	info := Current()
	if info.Module != "github.com/monstercameron/human-capital-management-suite" {
		t.Fatalf("module %q", info.Module)
	}
	if info.GoVersion == "" {
		t.Log("GoVersion empty outside built binary, acceptable")
	}
}

func TestCurrentTwice(t *testing.T) {
	a := Current()
	b := Current()
	if a.Module != b.Module {
		t.Fatal("module mismatch")
	}
	if a.GoVersion != b.GoVersion {
		t.Fatalf("go version mismatch %q vs %q", a.GoVersion, b.GoVersion)
	}
}

func TestInfoFields(t *testing.T) {
	var info Info
	_ = info.Module
	_ = info.Revision
	_ = info.Modified
	_ = info.GoVersion
	info.Modified = true
	if !info.Modified {
		t.Fatal("field")
	}
}
