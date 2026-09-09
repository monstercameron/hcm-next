package workload_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_TRUST_006 is the TRUST-006 primary test.
//
// GREEN: each of the four P1A processes authenticates as a unique,
// Ed25519-signed, short-lived workload identity bound to its declared
// process role and cell.
//
// RED: a service call bearing only node/network identity (never reaches
// [workload.Verifier.Verify] as a credential at all — there is no such
// argument), an expired credential, an unrecognized role and a credential
// issued for a different cell are all denied. Each is a separate subtest so
// a regression names itself.
func TestTodo_TRUST_006(t *testing.T) {
	authority := newTestAuthority(t, baseTime)
	verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
	ctx := context.Background()

	t.Run("every P1A process role authenticates through the same path", func(t *testing.T) {
		for _, role := range []workload.ProcessRole{workload.RoleHCMNext, workload.RoleWorker, workload.RoleProjector, workload.RoleMigrate} {
			token, err := authority.issuer.Issue(validSpec(role))
			if err != nil {
				t.Fatalf("Issue(%s): %v", role, err)
			}
			id, err := verifier.Verify(ctx, token)
			if err != nil {
				t.Fatalf("Verify(%s): %v", role, err)
			}
			if id.Role() != role {
				t.Errorf("Role() = %v, want %v", id.Role(), role)
			}
			if id.Cell() != cellPrimary {
				t.Errorf("Cell() = %q, want %q", id.Cell(), cellPrimary)
			}
			if id.KeyID() != "key-1" {
				t.Errorf("KeyID() = %q, want key-1", id.KeyID())
			}
			if !id.ValidAt(baseTime) {
				t.Error("identity should be valid at issuance time")
			}
		}
	})

	t.Run("RED: an unsigned bare identity cannot be constructed at all", func(t *testing.T) {
		// There is no exported way to build a workload.Identity except
		// through Verify. This subtest documents that a "node/network
		// identity only" call has literally nothing to present: the zero
		// value of workload.Identity is what Authorize (TRUST-007) sees for
		// such a caller, and it denies it (proven in the TRUST-007 tests).
		var zero workload.Identity
		if zero.Role() != "" || zero.Subject() != "" {
			t.Fatal("the zero Identity must carry no role or subject")
		}
	})

	t.Run("RED: expired credential is rejected", func(t *testing.T) {
		token, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		later := newVerifier(t, authority.source, cellPrimary, baseTime.Add(6*time.Minute))
		if _, err := later.Verify(ctx, token); !errors.Is(err, workload.ErrCredentialExpired) {
			t.Fatalf("Verify(expired) = %v, want ErrCredentialExpired", err)
		}
	})

	t.Run("RED: wrong cell is denied", func(t *testing.T) {
		token, err := authority.issuer.Issue(validSpec(workload.RoleWorker))
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		wrongCell := newVerifier(t, authority.source, cellOther, baseTime)
		if _, err := wrongCell.Verify(ctx, token); !errors.Is(err, workload.ErrWrongCell) {
			t.Fatalf("Verify(wrong cell) = %v, want ErrWrongCell", err)
		}
	})

	t.Run("RED: an unrecognized process role selector is denied", func(t *testing.T) {
		if _, err := authority.issuer.Issue(workload.IssueSpec{
			Subject: "instance-x", Role: "not_a_real_role", Cell: cellPrimary, Lifetime: time.Minute,
		}); err == nil {
			t.Fatal("Issue(unrecognized role) succeeded, want a failure")
		}
	})

	t.Run("Issue refuses a lifetime longer than 15 minutes", func(t *testing.T) {
		_, err := authority.issuer.Issue(workload.IssueSpec{
			Subject: "instance-x", Role: workload.RoleWorker, Cell: cellPrimary, Lifetime: 16 * time.Minute,
		})
		if !errors.Is(err, workload.ErrLifetimeTooLong) {
			t.Fatalf("Issue(16m) = %v, want ErrLifetimeTooLong", err)
		}
		if _, err := authority.issuer.Issue(workload.IssueSpec{
			Subject: "instance-x", Role: workload.RoleWorker, Cell: cellPrimary, Lifetime: workload.MaxIdentityLifetime,
		}); err != nil {
			t.Fatalf("Issue(exactly 15m) = %v, want success", err)
		}
	})
}
