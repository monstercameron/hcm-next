package oidckit_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/oidckit"
)

func root(t *testing.T) string {
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	r, e := oidckit.FindRepoRoot(f)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func valid() oidckit.AdapterPlan {
	return oidckit.AdapterPlan{TestOnly: true, OIDCVersion: "v1.2.3", OAuth2Version: "v0.30.0", Issuer: "https://id.example", Audience: "client", Algorithms: []string{"RS256", "ES256", "EdDSA"}, RequirePKCE: true, RequireState: true, RequireNonce: true, VerifyIssuer: true, RefreshOnKeyMiss: true, NormalizeClaims: true, NoAuthorizationFromClaims: true}
}

func TestOIDCBackendQualification(t *testing.T) {
	q, e := oidckit.LoadQualification(filepath.Join(root(t), "definitions", "architecture", "oidc-oauth2-qualification.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	if q.Version != 1 || q.Todo != "LIB-010" || q.OIDCModule != oidckit.OIDCModule || q.OAuth2Module != oidckit.OAuth2Module || q.Verdict != "REJECT" || !q.RuntimeDependencyGraphUnchanged {
		t.Fatalf("decision drifted: %#v", q)
	}
	if e := oidckit.ValidateAdapterPlan(valid()); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"go.mod", "go.sum"} {
		b, e := os.ReadFile(filepath.Join(root(t), n))
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), oidckit.OIDCModule) || strings.Contains(string(b), oidckit.OAuth2Module) {
			t.Fatalf("rejected candidate in %s", n)
		}
	}
}

func TestTodo_LIB_010_Golden(t *testing.T) {
	q, e := oidckit.LoadQualification(filepath.Join(root(t), "definitions", "architecture", "oidc-oauth2-qualification.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(q.Requirements.Algorithms, []string{"RS256", "ES256", "EdDSA"}) || q.Requirements.PKCE != "S256 PKCE is mandatory" || q.Requirements.StateNonce == "" || q.Requirements.KeyRefresh == "" {
		t.Fatalf("requirements drifted: %#v", q.Requirements)
	}
}

func TestTodo_LIB_010_Security(t *testing.T) {
	mut := []func(*oidckit.AdapterPlan){func(p *oidckit.AdapterPlan) { p.VerifyIssuer = false }, func(p *oidckit.AdapterPlan) { p.RequirePKCE = false }, func(p *oidckit.AdapterPlan) { p.RequireState = false }, func(p *oidckit.AdapterPlan) { p.RequireNonce = false }, func(p *oidckit.AdapterPlan) { p.RefreshOnKeyMiss = false }, func(p *oidckit.AdapterPlan) { p.NoAuthorizationFromClaims = false }, func(p *oidckit.AdapterPlan) { p.Algorithms = []string{"none"} }}
	for i, m := range mut {
		p := valid()
		m(&p)
		if e := oidckit.ValidateAdapterPlan(p); e == nil {
			t.Fatalf("mutation %d accepted", i)
		}
	}
}

func TestTodo_LIB_010_Integration(t *testing.T) {
	g, e := oidckit.ReleaseGraph(root(t), "./cmd/hcmnext", "./cmd/migrate", "./cmd/projector", "./cmd/worker")
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range []string{oidckit.OIDCModule, oidckit.OAuth2Module} {
		for _, x := range g {
			if x == m || strings.HasPrefix(x, m+"/") {
				t.Fatalf("release graph imports rejected %s", x)
			}
		}
	}
}

func TestTodo_LIB_010_Conformance(t *testing.T) {
	q, e := oidckit.LoadQualification(filepath.Join(root(t), "definitions", "architecture", "oidc-oauth2-qualification.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]bool{"TestOIDCBackendQualification": false, "TestTodo_LIB_010_Conformance": false, "TestTodo_LIB_010_Golden": false, "TestTodo_LIB_010_Integration": false, "TestTodo_LIB_010_Security": false}
	for _, r := range q.Evidence {
		if _, ok := want[r.Test]; !ok {
			t.Errorf("unexpected evidence %q", r.Test)
		} else {
			if want[r.Test] {
				t.Errorf("duplicate evidence %q", r.Test)
			}
			want[r.Test] = true
			if r.Package != "tools/quality/oidckit" {
				t.Errorf("evidence package %q", r.Package)
			}
		}
	}
	for n, v := range want {
		if !v {
			t.Errorf("missing evidence %s", n)
		}
	}
}
