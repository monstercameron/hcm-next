// Package contracts holds generated-contract conformance tests for the
// workflow, human-task, operator, DataOps, integration and evidence service
// contracts owned by PROTO-003 and PROTO-004. It is a separate package from
// tools/gen so it can own its own fixtures without reaching into that
// package's unexported helpers.
package contracts

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// findRepoRoot walks upward from the working directory until it finds
// buf.yaml, which lives at the repository root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "buf.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root: no buf.yaml found in any parent directory")
		}
		dir = parent
	}
}

// runBuf runs the buf CLI (must be on PATH) with the given arguments and cwd
// set to repoRoot, failing the test with combined stdout/stderr on error.
func runBuf(t *testing.T, repoRoot string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("buf", args...)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("buf %v: %v\noutput:\n%s", args, err, out.String())
	}
	return out.Bytes()
}

// buildDescriptorSet invokes `buf build --as-file-descriptor-set` into a temp
// file under dir and returns the parsed FileDescriptorSet, source_code_info
// included, so method leading-comments are inspectable. Generated Go
// descriptors omit source info to save size; buf build does not.
func buildDescriptorSet(t *testing.T, repoRoot, dir, name string) *descriptorpb.FileDescriptorSet {
	t.Helper()
	out := filepath.Join(dir, name)
	runBuf(t, repoRoot, "build", "--as-file-descriptor-set", "-o", out)
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read descriptor set %s: %v", out, err)
	}
	fds := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(raw, fds); err != nil {
		t.Fatalf("unmarshal descriptor set %s: %v", out, err)
	}
	return fds
}

// methodDisposition returns the leading comment recorded for one RPC method
// of one service within a proto file, and whether it was found at all. The
// path indices follow descriptor.proto: FileDescriptorProto.service is field
// 6, ServiceDescriptorProto.method is field 2.
func methodDisposition(fds *descriptorpb.FileDescriptorSet, file, service, method string) (string, bool) {
	for _, f := range fds.GetFile() {
		if f.GetName() != file {
			continue
		}
		for si, svc := range f.GetService() {
			if svc.GetName() != service {
				continue
			}
			for mi, m := range svc.GetMethod() {
				if m.GetName() != method {
					continue
				}
				for _, loc := range f.GetSourceCodeInfo().GetLocation() {
					p := loc.GetPath()
					if len(p) == 4 && p[0] == 6 && int(p[1]) == si && p[2] == 2 && int(p[3]) == mi {
						return loc.GetLeadingComments(), true
					}
				}
				return "", false
			}
		}
	}
	return "", false
}

// dispositionMarker is the fixed comment token every RPC method in the new
// workflow/humanwork/dataops/integration/evidence services must carry,
// stating in one place whether a P1A server implements or must refuse the
// method. "Total" means every declared method carries one, with no method
// left to infer its behavior from prose alone.
const dispositionMarker = "P1A disposition:"

// assertServiceHasTotalDispositionComments fails the test unless every
// method declared on the named service, in the given proto file, carries a
// leading comment containing dispositionMarker.
func assertServiceHasTotalDispositionComments(t *testing.T, fds *descriptorpb.FileDescriptorSet, file, service string, methods []string) {
	t.Helper()
	if len(methods) == 0 {
		t.Fatalf("%s: no methods given to check", service)
	}
	for _, method := range methods {
		comment, ok := methodDisposition(fds, file, service, method)
		if !ok {
			t.Errorf("%s.%s: method not found in built descriptor for %s", service, method, file)
			continue
		}
		if !strings.Contains(comment, dispositionMarker) {
			t.Errorf("%s.%s: leading comment does not contain %q; got %q",
				service, method, dispositionMarker, comment)
		}
	}
}

// enumInfo is one enum's identity plus its declared values, in declaration
// order, for the zero-value assertion below.
type enumInfo struct {
	fullName protoreflect.FullName
	values   protoreflect.EnumValueDescriptors
}

// collectEnums walks every message and top-level enum in every registered
// file whose package starts with packagePrefix, and returns every enum found.
// Importing the generated Go package for that prefix (done by the caller) is
// what causes its descriptors to register in the global registry.
func collectEnums(packagePrefix string) []enumInfo {
	var out []enumInfo
	var walkMsg func(mds protoreflect.MessageDescriptors)
	walkMsg = func(mds protoreflect.MessageDescriptors) {
		for i := 0; i < mds.Len(); i++ {
			m := mds.Get(i)
			eds := m.Enums()
			for j := 0; j < eds.Len(); j++ {
				out = append(out, enumInfo{fullName: eds.Get(j).FullName(), values: eds.Get(j).Values()})
			}
			walkMsg(m.Messages())
		}
	}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), packagePrefix) {
			return true
		}
		eds := fd.Enums()
		for j := 0; j < eds.Len(); j++ {
			out = append(out, enumInfo{fullName: eds.Get(j).FullName(), values: eds.Get(j).Values()})
		}
		walkMsg(fd.Messages())
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].fullName < out[j].fullName })
	return out
}

// assertEnumsHaveUnspecifiedZero fails the test unless every enum registered
// under packagePrefix declares its first (index 0) value at wire number 0
// with a name ending "_UNSPECIFIED" (or, for a bare top-level name, exactly
// "UNSPECIFIED"), and that at least minCount enums were actually found (so a
// registration failure reads as a failure, not a silent pass over nothing).
func assertEnumsHaveUnspecifiedZero(t *testing.T, packagePrefix string, minCount int) {
	t.Helper()
	enums := collectEnums(packagePrefix)
	if len(enums) < minCount {
		t.Fatalf("%s: expected at least %d registered enums, found %d", packagePrefix, minCount, len(enums))
	}
	for _, e := range enums {
		if e.values.Len() == 0 {
			t.Errorf("enum %s declares no values", e.fullName)
			continue
		}
		zero := e.values.Get(0)
		if zero.Number() != 0 {
			t.Errorf("enum %s: first declared value %s has number %d, want 0",
				e.fullName, zero.Name(), zero.Number())
		}
		name := string(zero.Name())
		if !strings.HasSuffix(name, "_UNSPECIFIED") && name != "UNSPECIFIED" {
			t.Errorf("enum %s: zero value is named %q, want a name ending _UNSPECIFIED", e.fullName, name)
		}
	}
}

// findMessage locates a message descriptor by full name among every file
// registered under packagePrefix. It fails the test if not found, so a typo
// in a presence-rule spot check reads as a failure rather than a silent skip.
func findMessage(t *testing.T, packagePrefix, fullName string) protoreflect.MessageDescriptor {
	t.Helper()
	var found protoreflect.MessageDescriptor
	var walkMsg func(mds protoreflect.MessageDescriptors)
	walkMsg = func(mds protoreflect.MessageDescriptors) {
		for i := 0; i < mds.Len(); i++ {
			m := mds.Get(i)
			if string(m.FullName()) == fullName {
				found = m
			}
			walkMsg(m.Messages())
		}
	}
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(string(fd.Package()), packagePrefix) {
			return true
		}
		walkMsg(fd.Messages())
		return true
	})
	if found == nil {
		t.Fatalf("message %s not found under package prefix %s", fullName, packagePrefix)
	}
	return found
}

// assertFieldPresence fails the test unless the named field of md declares
// explicit proto3 field presence (the `optional` keyword) exactly as
// wantOptional says. Explicit presence is the convention this schema uses for
// every field whose absence is a distinct, meaningful state (an unset
// timestamp, an unset ref) rather than the type's zero value.
func assertFieldPresence(t *testing.T, md protoreflect.MessageDescriptor, fieldName string, wantOptional bool) {
	t.Helper()
	fd := md.Fields().ByName(protoreflect.Name(fieldName))
	if fd == nil {
		t.Fatalf("%s: field %q not found", md.FullName(), fieldName)
	}
	got := fd.HasOptionalKeyword()
	if got != wantOptional {
		t.Errorf("%s.%s: HasOptionalKeyword() = %v, want %v", md.FullName(), fieldName, got, wantOptional)
	}
}

// roundTrip marshals m, unmarshals into a fresh instance of the same concrete
// type, and returns the fresh instance for comparison.
func roundTrip(t *testing.T, m proto.Message) proto.Message {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatalf("marshal %T: %v", m, err)
	}
	out := m.ProtoReflect().New().Interface()
	if err := proto.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal %T: %v", m, err)
	}
	return out
}

// assertGoldenRoundTrip fails the test unless every message in want survives
// a marshal/unmarshal round trip byte-for-byte equal in content.
func assertGoldenRoundTrip(t *testing.T, want []proto.Message) {
	t.Helper()
	for _, w := range want {
		got := roundTrip(t, w)
		if !proto.Equal(w, got) {
			t.Errorf("round trip mismatch for %T:\nwant %v\ngot  %v", w, w, got)
		}
	}
}

// assertBufGenerateIdempotent runs `buf generate -o <dir>` twice into two
// fresh temp directories and fails the test unless the resulting gen/go trees
// are byte-identical, proving pinned generation is deterministic across runs.
func assertBufGenerateIdempotent(t *testing.T) {
	t.Helper()
	repoRoot := findRepoRoot(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	runBuf(t, repoRoot, "generate", "-o", dir1)
	runBuf(t, repoRoot, "generate", "-o", dir2)
	diffTreesByteIdentical(t, filepath.Join(dir1, "gen", "go"), filepath.Join(dir2, "gen", "go"))
}

// listTreeFiles returns the sorted, slash-normalized relative paths of every
// regular file under root.
func listTreeFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(files)
	return files
}

// diffTreesByteIdentical fails the test with a precise reason unless dir1 and
// dir2 contain exactly the same set of relative file paths with
// byte-identical content.
func diffTreesByteIdentical(t *testing.T, dir1, dir2 string) {
	t.Helper()
	files1 := listTreeFiles(t, dir1)
	files2 := listTreeFiles(t, dir2)
	if fmt.Sprint(files1) != fmt.Sprint(files2) {
		t.Fatalf("generated file sets differ:\n%v\nvs\n%v", files1, files2)
	}
	if len(files1) == 0 {
		t.Fatal("generated tree is empty; nothing to compare")
	}
	for _, rel := range files1 {
		b1, err := os.ReadFile(filepath.Join(dir1, rel))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(dir1, rel), err)
		}
		b2, err := os.ReadFile(filepath.Join(dir2, rel))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(dir2, rel), err)
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("file %s differs between %s and %s", rel, dir1, dir2)
		}
	}
}
