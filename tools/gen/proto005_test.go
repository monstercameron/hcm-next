package gen

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	commonv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/common/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
)

// buildDescriptorSet invokes `buf build --as-file-descriptor-set` into a
// temp file under dir and returns the raw bytes plus the parsed
// FileDescriptorSet.
func buildDescriptorSet(t *testing.T, repoRoot, dir, name string) ([]byte, *descriptorpb.FileDescriptorSet) {
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
	return raw, fds
}

// TestTodo_PROTO_005 proves pinned generation produces a stable Go API and
// descriptor digest: two clean `buf build` runs produce byte-identical
// FileDescriptorSets with source info present, unknown-field bytes survive a
// decode/encode round trip by default, and the generated Go clients compile
// and function against an in-memory server.
func TestTodo_PROTO_005(t *testing.T) {
	repoRoot := findRepoRoot(t)

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	raw1, fds1 := buildDescriptorSet(t, repoRoot, dir1, "descriptor.binpb")
	raw2, fds2 := buildDescriptorSet(t, repoRoot, dir2, "descriptor.binpb")

	t.Run("StableDigestAcrossCleanRuns", func(t *testing.T) {
		if len(raw1) == 0 {
			t.Fatal("first descriptor set build produced zero bytes")
		}
		digest1 := sha256.Sum256(raw1)
		digest2 := sha256.Sum256(raw2)
		if digest1 != digest2 {
			t.Fatalf("descriptor set digest differs across clean runs: %x vs %x", digest1, digest2)
		}
	})

	t.Run("SourceInfoPresent", func(t *testing.T) {
		found := false
		for _, f := range fds1.GetFile() {
			if len(f.GetPackage()) < 8 || f.GetPackage()[:8] != "hcmnext." {
				continue
			}
			locs := f.GetSourceCodeInfo().GetLocation()
			if len(locs) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatal("no hcmnext file in the built descriptor set carries source_code_info locations")
		}
	})

	_ = fds2 // parsed to prove the second build is also well-formed; compared above via raw bytes.

	t.Run("UnknownFieldSurvivesDecodeEncodeRoundTrip", func(t *testing.T) {
		known := &commonv1.ScopeContext{TenantId: "tenant-1", OrganizationScopeId: "org-1", Purpose: "payroll"}
		knownBytes, err := proto.Marshal(known)
		if err != nil {
			t.Fatalf("marshal known message: %v", err)
		}

		// Append a field number ScopeContext does not declare, so decoding
		// must fall back to default unknown-field preservation rather than
		// rejecting or silently dropping it.
		const unknownFieldNumber = 999
		tail := protowire.AppendTag(nil, unknownFieldNumber, protowire.VarintType)
		tail = protowire.AppendVarint(tail, 7)
		wireBytes := append(append([]byte{}, knownBytes...), tail...)

		decoded := &commonv1.ScopeContext{}
		if err := proto.Unmarshal(wireBytes, decoded); err != nil {
			t.Fatalf("unmarshal message with unknown field: %v", err)
		}
		if decoded.GetTenantId() != known.GetTenantId() {
			t.Fatalf("known field lost alongside unknown field: got tenant_id %q", decoded.GetTenantId())
		}
		unknown := decoded.ProtoReflect().GetUnknown()
		if len(unknown) == 0 {
			t.Fatal("expected the unknown field to be preserved by default decoding, got zero unknown bytes")
		}

		reEncoded, err := proto.Marshal(decoded)
		if err != nil {
			t.Fatalf("re-marshal message carrying unknown field: %v", err)
		}
		roundTripped := &commonv1.ScopeContext{}
		if err := proto.Unmarshal(reEncoded, roundTripped); err != nil {
			t.Fatalf("unmarshal re-encoded message: %v", err)
		}
		if len(roundTripped.ProtoReflect().GetUnknown()) == 0 {
			t.Fatal("unknown field did not survive an encode/decode/encode/decode round trip")
		}
	})

	t.Run("GeneratedClientCompilesAndFunctions", func(t *testing.T) {
		srv := newFakeRegistryServer()
		conn := dialBufconn(t, func(s *grpc.Server) {
			registryv1.RegisterRegistryServiceServer(s, srv)
		})
		client := registryv1.NewRegistryServiceClient(conn)
		want := fixtureCapabilities()[0]
		resp, err := client.GetCapability(context.Background(), &registryv1.GetCapabilityRequest{
			CapabilityId: want.GetCapabilityId(),
			Version:      want.GetVersion(),
		})
		if err != nil {
			t.Fatalf("GetCapability via generated client: %v", err)
		}
		if !proto.Equal(want, resp.GetCapability()) {
			t.Fatalf("generated client round trip mismatch:\nwant %v\ngot  %v", want, resp.GetCapability())
		}
	})
}
