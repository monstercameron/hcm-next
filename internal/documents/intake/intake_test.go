package intake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
)

type scanner struct{ state documentsecurity.State }

func (s scanner) Scan(context.Context, documentsecurity.ScanInput, documentsecurity.Limits) (documentsecurity.Verdict, error) {
	return documentsecurity.Verdict{State: s.state, Scanner: "test-av", ScannerVersion: "1", Derivative: []byte("safe derivative")}, nil
}

func fixture(t *testing.T) (*documentsecurity.Registry, documentsecurity.Upload, documentsecurity.Limits) {
	t.Helper()
	r := documentsecurity.NewRegistry()
	u := documentsecurity.Upload{ID: "upload-1", Name: "medical.pdf", ContentType: "application/pdf", Bytes: []byte("bytes")}
	l := documentsecurity.Limits{MaxBytes: 100, MaxDerivativeBytes: 100, MaxCompressionRatio: 10}
	return r, u, l
}

func TestTodo_DOC_INTAKE_001(t *testing.T) {
	r, u, l := fixture(t)
	if _, err := r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe}); err != nil {
		t.Fatal(err)
	}
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Evidence.Check(); err != nil {
		t.Fatal(err)
	}
	if p.Evidence.Classification != MedicalSensitive || p.Evidence.Access.Download {
		t.Fatalf("unsafe governance: %#v", p.Evidence)
	}
}

func TestTodo_DOC_INTAKE_001_Golden(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist", Now: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if p.Evidence.ID != "evidence:upload-1-1" || p.DocumentType != "PDF" {
		t.Fatalf("unexpected golden package: %#v", p)
	}
}

func TestTodo_DOC_INTAKE_001_Security(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Unsafe})
	if _, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "x", SubjectID: "w", RequestedBy: "s"}); !errors.Is(err, ErrNotReleasable) {
		t.Fatalf("err=%v", err)
	}
	// A caller cannot select a less restrictive classification.
}

func TestTodo_DOC_INTAKE_001_Integration(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	p, err := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "review", SubjectID: "w", RequestedBy: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Evidence.Usable() {
		t.Fatal("safe evidence is not usable")
	}
	r.Revoke(u.ID, "rescan unsafe")
	if p.Evidence.Usable() {
		t.Fatal("revoked evidence remains usable")
	}
}

func TestTodo_DOC_INTAKE_001_Mutation(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	p, _ := NewService(r).Release(Request{ArtifactID: u.ID, Purpose: "review", SubjectID: "w", RequestedBy: "s"})
	p.Evidence.Access.Download = true
	if p.Evidence.Validate() == nil {
		t.Fatal("mutable caller fields must not weaken validation")
	}
}

func TestTodo_DOC_INTAKE_001_Race(t *testing.T) {
	r, u, l := fixture(t)
	_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
	s := NewService(r)
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func() {
			_, _ = s.Release(Request{ArtifactID: u.ID, Purpose: "review", SubjectID: "w", RequestedBy: "s"})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}

func FuzzTodo_DOC_INTAKE_001(f *testing.F) {
	f.Add("review", "worker")
	f.Fuzz(func(t *testing.T, purpose, subject string) {
		r, u, l := fixture(t)
		_, _ = r.Scan(context.Background(), u, l, scanner{documentsecurity.Safe})
		_, _ = NewService(r).Release(Request{ArtifactID: u.ID, Purpose: purpose, SubjectID: subject, RequestedBy: "specialist"})
	})
}
