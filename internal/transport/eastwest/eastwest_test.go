package eastwest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func repoManifest(t *testing.T) *Manifest {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root")
		}
		dir = parent
	}
	path := filepath.Join(dir, "definitions", "operations", "service-dependencies.yaml")
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return m
}

func TestTodo_EDGE_006(t *testing.T) {
	m := repoManifest(t)
	p, err := Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		caller string
		target string
		method string
		cell   string
		at     time.Time
		allow  bool
	}{
		{"declared hcmnext to postgres READ", "hcmnext", "postgres-store", "READ", "cell-local", now, true},
		{"declared worker to postgres WRITE", "worker", "postgres-store", "WRITE", "cell-local", now, true},
		{"unknown workload", "evil", "postgres-store", "READ", "cell-local", now, false},
		{"wrong cell", "hcmnext", "postgres-store", "READ", "other-cell", now, false},
		{"stale endpoint expired", "hcmnext", "postgres-store", "READ", "cell-local", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"stale before verified", "hcmnext", "postgres-store", "READ", "cell-local", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false},
		{"unauthorized method", "hcmnext", "postgres-store", "DELETE", "cell-local", now, false},
		{"unknown target", "hcmnext", "unknown-service", "READ", "cell-local", now, false},
	}
	for _, tc := range cases {
		err := p.Allows(tc.caller, tc.target, tc.method, tc.cell, tc.at)
		allowed := err == nil
		if allowed != tc.allow {
			t.Errorf("%s: allowed=%v err=%v want allow=%v", tc.name, allowed, err, tc.allow)
		}
	}
	t.Run("digest deterministic", func(t *testing.T) {
		p2, _ := Compile(m)
		if p.Digest() != p2.Digest() {
			t.Fatalf("digest not deterministic %s vs %s", p.Digest(), p2.Digest())
		}
		if len(p.Digest()) != 64 {
			t.Fatalf("digest len %d", len(p.Digest()))
		}
	})
	t.Run("mTLS signing verifies", func(t *testing.T) {
		sig := SignRequest("hcmnext->postgres-store", "secret")
		if !VerifyRequest("hcmnext->postgres-store", sig, "secret") {
			t.Fatal("verify failed")
		}
		if VerifyRequest("hcmnext->postgres-store", sig, "other") {
			t.Fatal("verify should fail with wrong secret")
		}
		if VerifyRequest("evil->postgres-store", sig, "secret") {
			t.Fatal("payload mismatch should fail")
		}
	})
}

func TestTodo_EDGE_006_Golden(t *testing.T) {
	m := repoManifest(t)
	p, err := Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if p.Digest() == "" {
		t.Fatal("empty digest")
	}
	if len(m.Dependencies) != 6 {
		t.Fatalf("dependency count %d want 6", len(m.Dependencies))
	}
	for _, d := range m.Dependencies {
		if d.EvidenceRef == "" || !strings.HasPrefix(d.EvidenceRef, "runbook://") {
			t.Fatalf("missing evidence ref for %s->%s", d.Consumer, d.Dependency)
		}
	}
}

func FuzzTodo_EDGE_006(f *testing.F) {
	m := repoManifest(&testing.T{})
	p, _ := Compile(m)
	now := time.Now().UTC()
	f.Add("hcmnext", "postgres-store", "READ", "cell-local")
	f.Add("evil", "unknown", "DELETE", "")
	f.Fuzz(func(t *testing.T, caller, target, method, cell string) {
		_ = p.Allows(caller, target, method, cell, now)
		sig := SignRequest(caller+target, "s")
		_ = VerifyRequest(caller+target, sig, "s")
	})
}

func TestTodo_EDGE_006_Security(t *testing.T) {
	m := repoManifest(t)
	p, _ := Compile(m)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	if err := p.Allows("", "postgres-store", "READ", "cell-local", now); err == nil {
		t.Fatal("empty caller should deny")
	}
	if err := p.Allows("hcmnext", "", "READ", "cell-local", now); err == nil {
		t.Fatal("empty target should deny")
	}
	if err := p.Allows("hcmnext", "postgres-store", "", "cell-local", now); err == nil {
		t.Fatal("empty method should deny")
	}
}
