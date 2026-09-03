package object

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func putFixture(t *testing.T) (*Fake, Info) {
	t.Helper()
	s := NewFake()
	i, err := s.PutIfAbsent(context.Background(), PutRequest{ArtifactID: "artifact-1", Content: []byte("0123456789"), MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	return s, i
}

func TestTodo_ARTIFACT_001(t *testing.T) {
	s, want := putFixture(t)
	got, err := s.Stat(context.Background(), Request{ArtifactID: want.ArtifactID})
	if err != nil || got.Digest != want.Digest || got.Size != 10 {
		t.Fatalf("stat = %#v, %v", got, err)
	}
	r, _, err := s.Get(context.Background(), Request{ArtifactID: want.ArtifactID})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	if string(b) != "0123456789" {
		t.Fatalf("get = %q", b)
	}
}

func TestTodo_ARTIFACT_001_Golden(t *testing.T) {
	s, info := putFixture(t)
	if !strings.HasPrefix(info.Digest, "sha256:") || len(info.Digest) != len("sha256:")+64 {
		t.Fatalf("digest = %q", info.Digest)
	}
	if _, err := s.PutIfAbsent(context.Background(), PutRequest{ArtifactID: "../escape", Content: []byte("x")}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("path error = %v", err)
	}
}

func TestTodo_ARTIFACT_001_Conformance(t *testing.T) {
	s, info := putFixture(t)
	replay, err := s.PutIfAbsent(context.Background(), PutRequest{ArtifactID: info.ArtifactID, Content: []byte("0123456789"), MediaType: "text/plain"})
	if err != nil || replay.Generation != info.Generation {
		t.Fatalf("replay = %#v, %v", replay, err)
	}
	if _, err := s.PutIfAbsent(context.Background(), PutRequest{ArtifactID: info.ArtifactID, Content: []byte("different"), MediaType: "text/plain"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("overwrite = %v", err)
	}
}

func TestTodo_ARTIFACT_001_Fault(t *testing.T) {
	s, info := putFixture(t)
	wrong := info.Generation + 1
	if _, err := s.Stat(context.Background(), Request{ArtifactID: info.ArtifactID, ExpectedGeneration: &wrong}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("generation = %v", err)
	}
	if _, _, err := s.GetRange(context.Background(), RangeRequest{ArtifactID: info.ArtifactID, Start: 8, End: ptr(int64(2))}); !errors.Is(err, ErrRangeUnsupported) {
		t.Fatalf("range = %v", err)
	}
}

func TestTodo_ARTIFACT_001_Integration(t *testing.T) {
	s, info := putFixture(t)
	r, got, err := s.GetRange(context.Background(), RangeRequest{ArtifactID: info.ArtifactID, Start: 2, End: ptr(int64(6))})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	if string(b) != "2345" || got.Digest != info.Digest {
		t.Fatalf("range = %q %#v", b, got)
	}
}

func TestTodo_ARTIFACT_001_Race(t *testing.T) {
	s := NewFake()
	done := make(chan error, 2)
	req := PutRequest{ArtifactID: "same", Content: []byte("same")}
	for i := 0; i < 2; i++ {
		go func() { _, err := s.PutIfAbsent(context.Background(), req); done <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzTodo_ARTIFACT_001(f *testing.F) {
	f.Add("fuzz", []byte("x"))
	f.Fuzz(func(t *testing.T, id string, data []byte) {
		if id == "" || len(data) == 0 {
			return
		}
		s := NewFake()
		_, _ = s.PutIfAbsent(context.Background(), PutRequest{ArtifactID: id, Content: data})
	})
}

func TestTodo_ARTIFACT_001_Abort(t *testing.T) {
	s, info := putFixture(t)
	if err := s.Abort(context.Background(), Request{ArtifactID: info.ArtifactID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(context.Background(), Request{ArtifactID: info.ArtifactID}); !errors.Is(err, ErrAborted) {
		t.Fatalf("abort = %v", err)
	}
}

func ptr(v int64) *int64 { return &v }
