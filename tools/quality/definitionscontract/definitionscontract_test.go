package definitionscontract

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCheckReportsSortedDefinitionsAndDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("zeta.yaml", "version: 1\n")
	write("nested/alpha.json", "{\"ok\":true}\n")
	write("README.md", "# Definitions\n")
	before := snapshot(t, root)
	report, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"README.md", "nested/alpha.json", "zeta.yaml"}
	if !reflect.DeepEqual(report.Files, want) {
		t.Fatalf("files = %#v, want %#v", report.Files, want)
	}
	if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("checker mutated definitions: before %#v after %#v", before, after)
	}
}

func TestCheckRejectsUnsupportedAndMalformedFiles(t *testing.T) {
	tests := []struct{ name, file, body string }{
		{"unsupported source", "runtime.go", "package runtime"},
		{"malformed json", "bad.json", "{"},
		{"malformed yaml", "bad.yaml", "a: ["},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.file), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Validate(root); err == nil {
				t.Fatalf("Validate(%q) succeeded", tc.file)
			}
		})
	}
}

func TestCheckRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.yaml")
	if err := os.WriteFile(target, []byte("ok: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.yaml")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Validate(root); err == nil {
		t.Fatal("symlink accepted")
	}
}

func snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	r := map[string][]byte{}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, _ := filepath.Rel(root, path)
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			r[rel] = data
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return r
}
