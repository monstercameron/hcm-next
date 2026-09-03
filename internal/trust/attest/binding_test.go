package attest

import (
	"errors"
	"testing"
	"time"
)

func validBindingRequest() Request {
	return Request{
		StatementID: "hours-attestation", StatementVersion: "7", StatementDigest: "stmt-digest-v7",
		Facts:       []PresentedArtifact{{Ref: "timecard:123", Version: "42", Kind: "fact", Digest: "sha256:fact"}},
		Attachments: []PresentedArtifact{{Ref: "policy.pdf", Version: "3", Kind: "attachment", Digest: "sha256:policy"}},
		Evidence:    []Evidence{{Ref: "timecard:123", Version: "42", Kind: "timecard", Digest: "sha256:evidence"}},
		Context:     Context{Tenant: "tenant-a", Subject: "worker:123", Purpose: "payroll-certification", EffectiveFrom: time.Unix(100, 0), EffectiveTo: time.Unix(200, 0), Timezone: "America/New_York", TzdbVersion: "2026a", CalendarVersion: "gregorian-1", AuthZDigest: "authz-v4", PolicyDigest: "policy-v9"},
	}
}

// TestTodo_ATTEST_002 is the PRIMARY matrix clause: the receipt is only
// valid for the complete request that was presented.
func TestTodo_ATTEST_002(t *testing.T) {
	r := validBindingRequest()
	b, err := Bind(r)
	if err != nil {
		t.Fatalf("ATTEST_002_REJECTED: valid request was rejected: %v", err)
	}
	if b.Digest == "" {
		t.Fatal("binding has empty digest")
	}
	if err := VerifyBinding(r, b); err != nil {
		t.Fatalf("bound request did not verify: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Request)
	}{
		{"statement", func(r *Request) { r.StatementDigest = "stmt-digest-v8" }},
		{"fact", func(r *Request) { r.Facts[0].Version = "43" }},
		{"attachment", func(r *Request) { r.Attachments[0].Digest = "sha256:changed" }},
		{"evidence", func(r *Request) { r.Evidence[0].Ref = "other-evidence" }},
		{"time context", func(r *Request) { r.Context.EffectiveTo = time.Unix(201, 0) }},
		{"authorization context", func(r *Request) { r.Context.AuthZDigest = "authz-v5" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := r
			tc.mutate(&changed)
			if err := VerifyBinding(changed, b); err == nil {
				t.Fatal("ATTEST_002_REJECTED: changed request verified")
			}
		})
	}
}

// TestTodo_ATTEST_002_Golden pins the exact digest output and proves that
// field framing prevents concatenation ambiguity.
func TestTodo_ATTEST_002_Golden(t *testing.T) {
	d, err := validBindingRequest().Digest()
	if err != nil {
		t.Fatal(err)
	}
	const want = "098cf9e4ba6335bddc9fe4cd05c402f3a0324dc691ed8c9b58425e20bf09ec2c"
	if d != want {
		t.Fatalf("golden digest = %q, want %q", d, want)
	}

	for name, mutate := range map[string]func(*Request){
		"missing artifact digest":   func(r *Request) { r.Attachments[0].Digest = "" },
		"missing context":           func(r *Request) { r.Context.PolicyDigest = "" },
		"missing statement version": func(r *Request) { r.StatementVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			r := validBindingRequest()
			mutate(&r)
			if _, err := Bind(r); err == nil || !errors.Is(err, ErrInvalidBinding) {
				t.Fatalf("Bind error = %v, want ErrInvalidBinding", err)
			}
		})
	}
}
