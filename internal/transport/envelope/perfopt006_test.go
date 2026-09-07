package envelope_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/hcm-next/internal/transport/envelope"
)

func perfEnvelope() *envelope.Error {
	return envelope.New(envelope.CodeFailedPrecondition, "intent.stale_revision", "a precondition for the operation is not met").
		WithViolation("cycle.revision", "the cycle revision is stale", "cycle.revision.current").
		WithViolation("cycle.version", "the cycle version is stale", "cycle.version.current").
		WithViolation("request.expected_digest", "the expected digest is stale", "request.expected_digest.current").
		WithCorrelation("req-perfopt-006").
		WithEvidence(envelope.Evidence{ID: "ev:perfopt:006", Kind: "domain_decision", Digest: "sha256:perfopt006"})
}

func TestTodo_PERFOPT_006_Golden(t *testing.T) {
	cases := map[string][]byte{
		"full":    mustMarshalDetail(t, perfEnvelope()),
		"minimal": mustMarshalDetail(t, envelope.New(envelope.CodeNotFound, "resource.missing", "the resource does not exist or is not visible")),
		"unsafe":  mustMarshalDetail(t, envelope.New(envelope.CodeUnavailable, "transport.failure", "password leaked by provider")),
	}
	for name, got := range cases {
		assertGolden(t, filepath.Join("perfopt006_envelope_"+name+".bin"), got)
	}
}

func TestTodo_PERFOPT_006(t *testing.T) {
	const maxAllocs = 9 // pre-change benchmark: 12 allocs/op; ceiling is 25% lower
	got := testing.AllocsPerRun(100, func() {
		err := perfEnvelope()
		_ = err.Detail()
	})
	if got > maxAllocs {
		t.Fatalf("allocations = %v, want <= %d", got, maxAllocs)
	}
}

func BenchmarkTodo_PERFOPT_006(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := perfEnvelope()
		_ = err.Detail()
	}
}

func mustMarshalDetail(t *testing.T, err *envelope.Error) []byte {
	t.Helper()
	raw, marshalErr := proto.Marshal(err.Detail())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	return raw
}

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with HCMNEXT_UPDATE_GOLDEN=1)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s differs:\n got %s\nwant %s", path, hex.EncodeToString(got), hex.EncodeToString(want))
	}
}
