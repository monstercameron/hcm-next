package outbound_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/lease"
	"github.com/monstercameron/hcm-next/internal/trust/outbound"
)

func testPolicy(t *testing.T) *outbound.Policy {
	t.Helper()
	p, err := outbound.NewPolicy(
		outbound.Destination{
			Name:           "sftp.bank.example",
			TrustBundleRef: "bundle:sftp.bank.example:2026-09",
			Purposes:       []string{"payroll-file-delivery"},
			DataClasses:    []string{"payroll-pii"},
		},
		outbound.Destination{
			Name:           "webhook.vendor.example",
			TrustBundleRef: "bundle:webhook.vendor.example:2026-01",
			Purposes:       []string{"benefits-enrollment-notify"},
			DataClasses:    []string{"benefits-status"},
		},
	)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	return p
}

// TestTodo_TRUST_017 is the PRIMARY test: a request to an allowlisted
// destination, for a cleared purpose and data classification, is allowed
// with its pinned trust bundle reference echoed back; a destination not on
// the allowlist, an allowlisted destination used for an uncleared purpose or
// data classification, and a presented credential lease minted for a
// different destination are all refused.
func TestTodo_TRUST_017(t *testing.T) {
	policy := testPolicy(t)

	t.Run("an allowlisted destination cleared for the purpose and data class is allowed", func(t *testing.T) {
		dec, err := policy.Check(outbound.CheckRequest{
			Destination: "sftp.bank.example", Purpose: "payroll-file-delivery", DataClass: "payroll-pii",
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !dec.Allowed {
			t.Fatalf("decision = %+v, want Allowed", dec)
		}
		if dec.TrustBundleRef != "bundle:sftp.bank.example:2026-09" {
			t.Fatalf("TrustBundleRef = %q, want the pinned bundle ref", dec.TrustBundleRef)
		}
	})

	t.Run("a destination not on the allowlist is refused", func(t *testing.T) {
		dec, err := policy.Check(outbound.CheckRequest{
			Destination: "sftp.unknown.example", Purpose: "payroll-file-delivery", DataClass: "payroll-pii",
		})
		if !errors.Is(err, outbound.ErrNotAllowlisted) {
			t.Fatalf("err = %v, want ErrNotAllowlisted", err)
		}
		if dec.Allowed {
			t.Fatalf("decision = %+v, want not Allowed", dec)
		}
	})

	t.Run("an allowlisted destination used for an uncleared purpose is refused", func(t *testing.T) {
		_, err := policy.Check(outbound.CheckRequest{
			Destination: "sftp.bank.example", Purpose: "recruiting-outreach", DataClass: "payroll-pii",
		})
		if !errors.Is(err, outbound.ErrPurposeNotCleared) {
			t.Fatalf("err = %v, want ErrPurposeNotCleared", err)
		}
	})

	t.Run("an allowlisted destination not cleared for the data class is refused", func(t *testing.T) {
		_, err := policy.Check(outbound.CheckRequest{
			Destination: "sftp.bank.example", Purpose: "payroll-file-delivery", DataClass: "benefits-status",
		})
		if !errors.Is(err, outbound.ErrDataClassNotCleared) {
			t.Fatalf("err = %v, want ErrDataClassNotCleared", err)
		}
	})

	t.Run("a credential lease minted for a different destination is refused", func(t *testing.T) {
		leased := &lease.CredentialLease{Destination: "webhook.vendor.example"}
		_, err := policy.Check(outbound.CheckRequest{
			Destination: "sftp.bank.example", Purpose: "payroll-file-delivery", DataClass: "payroll-pii", Lease: leased,
		})
		if !errors.Is(err, outbound.ErrLeaseDestinationMismatch) {
			t.Fatalf("err = %v, want ErrLeaseDestinationMismatch", err)
		}
	})

	t.Run("a credential lease minted for the matching destination is not itself a reason to refuse", func(t *testing.T) {
		leased := &lease.CredentialLease{Destination: "sftp.bank.example"}
		dec, err := policy.Check(outbound.CheckRequest{
			Destination: "sftp.bank.example", Purpose: "payroll-file-delivery", DataClass: "payroll-pii", Lease: leased,
		})
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
		if !dec.Allowed {
			t.Fatalf("decision = %+v, want Allowed", dec)
		}
	})
}

// FuzzTodo_TRUST_017 is the FUZZ matrix test: Check, over arbitrary
// destination/purpose/data-class strings against a fixed policy, only ever
// allows the one combination the policy actually clears, and never panics.
func FuzzTodo_TRUST_017(f *testing.F) {
	f.Add("sftp.bank.example", "payroll-file-delivery", "payroll-pii")
	f.Add("sftp.bank.example", "payroll-file-delivery", "benefits-status")
	f.Add("sftp.unknown.example", "payroll-file-delivery", "payroll-pii")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, destination, purpose, dataClass string) {
		policy := testPolicy(t)
		dec, err := policy.Check(outbound.CheckRequest{Destination: destination, Purpose: purpose, DataClass: dataClass})
		wantAllow := destination == "sftp.bank.example" && purpose == "payroll-file-delivery" && dataClass == "payroll-pii"
		if wantAllow {
			if err != nil || !dec.Allowed {
				t.Fatalf("Check(%q,%q,%q): err=%v dec=%+v, want allowed", destination, purpose, dataClass, err, dec)
			}
		} else if err == nil {
			t.Fatalf("Check(%q,%q,%q) succeeded, want a refusal", destination, purpose, dataClass)
		}
	})
}

// TestTodo_TRUST_017_Security proves the policy fails closed against
// adversarial and malformed input: an empty allowlist authorizes nothing, a
// nil policy is refused rather than panicking, an empty destination/purpose/
// data-class request is refused as invalid rather than silently matching
// nothing, and a destination name that only differs by case or embedded
// whitespace from an allowlisted one is not conflated with it.
func TestTodo_TRUST_017_Security(t *testing.T) {
	t.Run("an empty allowlist authorizes nothing", func(t *testing.T) {
		empty, err := outbound.NewPolicy()
		if err != nil {
			t.Fatalf("NewPolicy(): %v", err)
		}
		if _, err := empty.Check(outbound.CheckRequest{Destination: "anything", Purpose: "p", DataClass: "c"}); !errors.Is(err, outbound.ErrNotAllowlisted) {
			t.Fatalf("err = %v, want ErrNotAllowlisted", err)
		}
	})

	t.Run("a nil policy is refused, not panicked on", func(t *testing.T) {
		var p *outbound.Policy
		if _, err := p.Check(outbound.CheckRequest{Destination: "d", Purpose: "p", DataClass: "c"}); !errors.Is(err, outbound.ErrInvalidPolicy) {
			t.Fatalf("err = %v, want ErrInvalidPolicy", err)
		}
	})

	t.Run("an empty destination, purpose or data class is refused as an invalid request", func(t *testing.T) {
		policy := testPolicy(t)
		cases := []outbound.CheckRequest{
			{Destination: "", Purpose: "payroll-file-delivery", DataClass: "payroll-pii"},
			{Destination: "sftp.bank.example", Purpose: "", DataClass: "payroll-pii"},
			{Destination: "sftp.bank.example", Purpose: "payroll-file-delivery", DataClass: ""},
		}
		for _, req := range cases {
			if _, err := policy.Check(req); !errors.Is(err, outbound.ErrInvalidRequest) {
				t.Fatalf("Check(%+v) err = %v, want ErrInvalidRequest", req, err)
			}
		}
	})

	t.Run("a case-distinct or padded destination name is not conflated with the allowlisted one", func(t *testing.T) {
		policy := testPolicy(t)
		for _, name := range []string{"SFTP.BANK.EXAMPLE", " sftp.bank.example", "sftp.bank.example "} {
			if _, err := policy.Check(outbound.CheckRequest{Destination: name, Purpose: "payroll-file-delivery", DataClass: "payroll-pii"}); !errors.Is(err, outbound.ErrNotAllowlisted) {
				t.Fatalf("Check(%q) err = %v, want ErrNotAllowlisted (must not conflate with sftp.bank.example)", name, err)
			}
		}
	})

	t.Run("a destination declared with no purposes or no data classes is refused at construction", func(t *testing.T) {
		if _, err := outbound.NewPolicy(outbound.Destination{Name: "d", TrustBundleRef: "bundle:d", DataClasses: []string{"c"}}); !errors.Is(err, outbound.ErrInvalidPolicy) {
			t.Fatalf("err = %v, want ErrInvalidPolicy (no purposes)", err)
		}
		if _, err := outbound.NewPolicy(outbound.Destination{Name: "d", TrustBundleRef: "bundle:d", Purposes: []string{"p"}}); !errors.Is(err, outbound.ErrInvalidPolicy) {
			t.Fatalf("err = %v, want ErrInvalidPolicy (no data classes)", err)
		}
	})

	t.Run("a destination with no pinned trust bundle reference is refused at construction", func(t *testing.T) {
		if _, err := outbound.NewPolicy(outbound.Destination{Name: "d", Purposes: []string{"p"}, DataClasses: []string{"c"}}); !errors.Is(err, outbound.ErrInvalidPolicy) {
			t.Fatalf("err = %v, want ErrInvalidPolicy", err)
		}
	})

	t.Run("a duplicate destination name is refused at construction", func(t *testing.T) {
		d := outbound.Destination{Name: "d", TrustBundleRef: "bundle:d", Purposes: []string{"p"}, DataClasses: []string{"c"}}
		if _, err := outbound.NewPolicy(d, d); !errors.Is(err, outbound.ErrInvalidPolicy) {
			t.Fatalf("err = %v, want ErrInvalidPolicy", err)
		}
	})
}

// TestTodo_TRUST_017_Mutation proves every refusal branch in Check is
// load-bearing: a scenario engineered to trip exactly one check, with every
// other dimension satisfied, is refused with that check's specific error - a
// mutant that deletes or short-circuits one branch would let at least one of
// these through as allowed.
func TestTodo_TRUST_017_Mutation(t *testing.T) {
	base := outbound.CheckRequest{Destination: "sftp.bank.example", Purpose: "payroll-file-delivery", DataClass: "payroll-pii"}

	cases := []struct {
		name    string
		mutate  func(outbound.CheckRequest) outbound.CheckRequest
		wantErr error
	}{
		{
			name:    "allowlist membership check",
			mutate:  func(r outbound.CheckRequest) outbound.CheckRequest { r.Destination = "sftp.unknown.example"; return r },
			wantErr: outbound.ErrNotAllowlisted,
		},
		{
			name:    "purpose check",
			mutate:  func(r outbound.CheckRequest) outbound.CheckRequest { r.Purpose = "recruiting-outreach"; return r },
			wantErr: outbound.ErrPurposeNotCleared,
		},
		{
			name:    "data class check",
			mutate:  func(r outbound.CheckRequest) outbound.CheckRequest { r.DataClass = "benefits-status"; return r },
			wantErr: outbound.ErrDataClassNotCleared,
		},
		{
			name: "lease destination binding check",
			mutate: func(r outbound.CheckRequest) outbound.CheckRequest {
				r.Lease = &lease.CredentialLease{Destination: "webhook.vendor.example"}
				return r
			},
			wantErr: outbound.ErrLeaseDestinationMismatch,
		},
	}

	policy := testPolicy(t)
	// The baseline request itself must be allowed - otherwise a mutation
	// case proves nothing about which specific check fired.
	if dec, err := policy.Check(base); err != nil || !dec.Allowed {
		t.Fatalf("baseline request: dec=%+v err=%v, want allowed", dec, err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := policy.Check(tc.mutate(base)); !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
