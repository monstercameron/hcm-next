package bufprotovalidatekit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	kit "github.com/monstercameron/hcm-next/tools/quality/bufprotovalidatekit"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}

func fixtureDescriptor(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootTB(t), "tools", "quality", "bufprotovalidatekit", "testdata", "request_descriptor.json"))
	if err != nil {
		t.Fatalf("read descriptor fixture: %v", err)
	}
	var raw descriptorpb.FileDescriptorProto
	if err := protojson.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode descriptor fixture: %v", err)
	}
	file, err := protodesc.NewFile(&raw, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("compile descriptor fixture: %v", err)
	}
	message := file.Messages().ByName("Request")
	if message == nil {
		t.Fatal("Request descriptor is missing")
	}
	return message
}

func fixtureSpec(t testing.TB) kit.Spec {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootTB(t), "tools", "quality", "bufprotovalidatekit", "testdata", "structural_rules.json"))
	if err != nil {
		t.Fatalf("read rule fixture: %v", err)
	}
	var spec kit.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("decode rule fixture: %v", err)
	}
	return spec
}

func fixtureValidator(t testing.TB) (*kit.Validator, protoreflect.MessageDescriptor) {
	t.Helper()
	descriptor := fixtureDescriptor(t)
	validator, err := kit.Compile(descriptor, fixtureSpec(t))
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	return validator, descriptor
}

func requestMessage(descriptor protoreflect.MessageDescriptor, workerID, note, typeURL string, payload []byte) *dynamicpb.Message {
	message := dynamicpb.NewMessage(descriptor)
	message.Set(descriptor.Fields().ByName("worker_id"), protoreflect.ValueOfString(workerID))
	message.Set(descriptor.Fields().ByName("note"), protoreflect.ValueOfString(note))
	if typeURL != "" || payload != nil {
		dynamic := &anypb.Any{TypeUrl: typeURL, Value: append([]byte(nil), payload...)}
		message.Set(descriptor.Fields().ByName("attachment"), protoreflect.ValueOfMessage(dynamic.ProtoReflect()))
	}
	return message
}

func repoRootTB(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}
