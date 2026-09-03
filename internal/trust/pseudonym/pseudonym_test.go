package pseudonym_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/pseudonym"
)

func engine(t *testing.T) *pseudonym.Engine {
	t.Helper()
	e, err := pseudonym.New([]byte("test-only high entropy secret material"), "anon-002.v1")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestTodo_ANON_002(t *testing.T) {
	e := engine(t)
	scopes := []pseudonym.Scope{{Tenant: "t1"}, {Program: "p1"}, {Campaign: "c1"}, {Case: "case1"}}
	tokens := make(map[string]string)
	for _, scope := range scopes {
		token, err := e.Derive("opaque-subject", scope)
		if err != nil {
			t.Fatalf("derive %v: %v", scope, err)
		}
		if !strings.HasPrefix(token, "ps1_") {
			t.Fatalf("token has no versioned prefix: %q", token)
		}
		tokens[scopeLabel(scope)] = token
		_, effects, err := e.Accept(pseudonym.Request{Subject: "opaque-subject", Pseudonym: token, Scope: scope, Version: e.Version()})
		if err != nil {
			t.Fatalf("accept %s: %v", scopeLabel(scope), err)
		}
		if effects.AuthoritativeRows != 1 || effects.BusinessEvents != 0 || effects.OutboxEntries != 0 || effects.HumanWork != 0 || effects.ProviderRequests != 0 {
			t.Fatalf("unexpected successful effects: %+v", effects)
		}
	}
	if tokens["tenant"] == tokens["program"] || tokens["tenant"] == tokens["campaign"] || tokens["tenant"] == tokens["case"] {
		t.Fatal("pseudonym correlated across scopes")
	}
}

func TestTodo_ANON_002_Security(t *testing.T) {
	e := engine(t)
	ten, prog := pseudonym.Scope{Tenant: "tenant-a"}, pseudonym.Scope{Program: "program-a"}
	token, err := e.Derive("subject-a", ten)
	if err != nil {
		t.Fatal(err)
	}
	_, effects, err := e.Accept(pseudonym.Request{Subject: "subject-a", Pseudonym: token, Scope: prog, Version: e.Version()})
	if err == nil {
		t.Fatal("tenant token accepted in program scope")
	}
	if !effects.IsZero() {
		t.Fatalf("rejection emitted effects: %+v", effects)
	}
	var rej *pseudonym.Rejection
	if !errors.As(err, &rej) || rej.Code != "ANON_002_REJECTED" || rej.Field != "pseudonym" || rej.State != "CORRELATES_OUTSIDE_SCOPE" || rej.Version != e.Version() {
		t.Fatalf("rejection evidence = %v", err)
	}
	// A token is not a raw subject and cannot be generated with the public
	// scope alone; changing the subject changes the token.
	other, _ := e.Derive("subject-b", ten)
	if other == token {
		t.Fatal("different subjects yielded the same token")
	}
}

func TestTodo_ANON_002_Mutation(t *testing.T) {
	e := engine(t)
	scope := pseudonym.Scope{Tenant: "tenant-a"}
	token, err := e.Derive("subject-a", scope)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name         string
		req          pseudonym.Request
		field, state string
	}{
		{"scope", pseudonym.Request{Subject: "subject-a", Pseudonym: token, Scope: pseudonym.Scope{Tenant: "t", Program: "p"}, Version: e.Version()}, "scope", "INVALID"},
		{"version", pseudonym.Request{Subject: "subject-a", Pseudonym: token, Scope: scope, Version: "anon-002.old"}, "version", "MISMATCH"},
		{"subject", pseudonym.Request{Pseudonym: token, Scope: scope, Version: e.Version()}, "subject", "MISSING"},
		{"pseudonym", pseudonym.Request{Subject: "subject-a", Pseudonym: "ps1_forged", Scope: scope, Version: e.Version()}, "pseudonym", "CORRELATES_OUTSIDE_SCOPE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, effects, err := e.Accept(tc.req)
			if err == nil || !effects.IsZero() {
				t.Fatalf("err=%v effects=%+v", err, effects)
			}
			var rej *pseudonym.Rejection
			if !errors.As(err, &rej) || rej.Field != tc.field || rej.State != tc.state || rej.Code != "ANON_002_REJECTED" {
				t.Fatalf("evidence=%v", err)
			}
		})
	}
}

func scopeLabel(s pseudonym.Scope) string {
	if s.Tenant != "" {
		return "tenant"
	}
	if s.Program != "" {
		return "program"
	}
	if s.Campaign != "" {
		return "campaign"
	}
	return "case"
}
