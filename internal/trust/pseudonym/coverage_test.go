package pseudonym

import (
	"errors"
	"testing"
)

func TestScope_ValidateAndEffects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope Scope
		valid bool
	}{
		{"tenant", Scope{Tenant: "t"}, true}, {"program", Scope{Program: "p"}, true}, {"campaign", Scope{Campaign: "c"}, true}, {"case", Scope{Case: "c"}, true},
		{"empty", Scope{}, false}, {"multiple", Scope{Tenant: "t", Program: "p"}, false}, {"padded only", Scope{Tenant: " "}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.scope.Validate()
			if tc.valid && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
			if !tc.valid && !errors.Is(err, ErrScopeInvalid) {
				t.Fatalf("Validate() = %v, want ErrScopeInvalid", err)
			}
		})
	}
	if !(Effects{}).IsZero() || (Effects{BusinessEvents: 1}).IsZero() {
		t.Fatal("Effects.IsZero does not distinguish zero from non-zero effects")
	}
}

func TestNew_DeriveAndAccept_RejectEveryInvalidInput(t *testing.T) {
	if _, err := New(nil, "v1"); !errors.Is(err, ErrSecretRequired) {
		t.Fatalf("empty secret error = %v", err)
	}
	if _, err := New([]byte("secret"), " "); !errors.Is(err, ErrVersionRequired) {
		t.Fatalf("empty version error = %v", err)
	}
	e, err := New([]byte("secret"), "v1")
	if err != nil || e.Version() != "v1" {
		t.Fatalf("New/Version = %v, %q", err, e.Version())
	}
	if _, err := e.Derive(" ", Scope{Tenant: "t"}); !errors.Is(err, ErrSubjectRequired) {
		t.Fatalf("empty subject error = %v", err)
	}
	if _, err := e.Derive("subject", Scope{}); !errors.Is(err, ErrScopeInvalid) {
		t.Fatalf("invalid scope error = %v", err)
	}
	token, err := e.Derive("subject", Scope{Tenant: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		req          Request
		field, state string
	}{
		{"scope", Request{Subject: "subject", Pseudonym: token, Scope: Scope{Tenant: "t", Program: "p"}, Version: "v1"}, "scope", "INVALID"},
		{"version", Request{Subject: "subject", Pseudonym: token, Scope: Scope{Tenant: "tenant"}, Version: "old"}, "version", "MISMATCH"},
		{"subject", Request{Pseudonym: token, Scope: Scope{Tenant: "tenant"}, Version: "v1"}, "subject", "MISSING"},
		{"pseudonym", Request{Subject: "subject", Scope: Scope{Tenant: "tenant"}, Version: "v1"}, "pseudonym", "MISSING"},
		{"forged", Request{Subject: "subject", Pseudonym: "ps1_forged", Scope: Scope{Tenant: "tenant"}, Version: "v1"}, "pseudonym", "CORRELATES_OUTSIDE_SCOPE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			receipt, effects, err := e.Accept(tc.req)
			if err == nil || !effects.IsZero() {
				t.Fatalf("Accept() receipt=%+v effects=%+v err=%v", receipt, effects, err)
			}
			var rejection *Rejection
			if !errors.As(err, &rejection) || rejection.Code != "ANON_002_REJECTED" || rejection.Field != tc.field || rejection.State != tc.state || rejection.Version != tc.req.Version || rejection.Error() == "" {
				t.Fatalf("rejection = %v, want field=%s state=%s version=%s", err, tc.field, tc.state, tc.req.Version)
			}
		})
	}
	receipt, effects, err := e.Accept(Request{Subject: "subject", Pseudonym: token, Scope: Scope{Tenant: "tenant"}, Version: "v1"})
	if err != nil || receipt.Pseudonym != token || receipt.Version != "v1" || receipt.Scope.Tenant != "tenant" || effects.AuthoritativeRows != 1 {
		t.Fatalf("valid Accept() = receipt=%+v effects=%+v err=%v", receipt, effects, err)
	}
}
