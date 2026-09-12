package approverclass_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
)

func TestDeriveDistinct(t *testing.T) {
	t.Run("fails closed with no base", func(t *testing.T) {
		if _, err := approverclass.DeriveDistinct("", approverclass.FinancePartner); !errors.Is(err, approverclass.ErrNoBase) {
			t.Fatalf("DeriveDistinct(\"\", FinancePartner) = %v, want ErrNoBase", err)
		}
	})

	t.Run("fails closed with no class", func(t *testing.T) {
		if _, err := approverclass.DeriveDistinct("principal:base", approverclass.ClassUnspecified); !errors.Is(err, approverclass.ErrNoClass) {
			t.Fatalf("DeriveDistinct with ClassUnspecified = %v, want ErrNoClass", err)
		}
	})

	t.Run("the same base derives different principals for different classes", func(t *testing.T) {
		finance, err := approverclass.DeriveDistinct("principal:base", approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct finance: %v", err)
		}
		manager, err := approverclass.DeriveDistinct("principal:base", approverclass.CurrentManager)
		if err != nil {
			t.Fatalf("DeriveDistinct manager: %v", err)
		}
		if finance == manager {
			t.Fatalf("finance %q and manager %q must differ", finance, manager)
		}
		if finance == "" || manager == "" {
			t.Fatal("a derived principal must never be the empty string")
		}
	})

	t.Run("the same base and class always derive the same principal", func(t *testing.T) {
		a, err := approverclass.DeriveDistinct("principal:base", approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct: %v", err)
		}
		b, err := approverclass.DeriveDistinct("principal:base", approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct: %v", err)
		}
		if a != b {
			t.Fatalf("derivation is not deterministic: %q vs %q", a, b)
		}
	})

	t.Run("different bases never collide for the same class", func(t *testing.T) {
		a, err := approverclass.DeriveDistinct("principal:alice", approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct: %v", err)
		}
		b, err := approverclass.DeriveDistinct("principal:bob", approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct: %v", err)
		}
		if a == b {
			t.Fatalf("two different bases derived to the same principal: %q", a)
		}
	})
}

func TestRequireDistinct(t *testing.T) {
	t.Run("accepts two distinct, resolved principals", func(t *testing.T) {
		if err := approverclass.RequireDistinct("principal:finance", "principal:manager"); err != nil {
			t.Fatalf("RequireDistinct on distinct principals: %v, want nil", err)
		}
	})

	t.Run("refuses a shared owner", func(t *testing.T) {
		if err := approverclass.RequireDistinct("principal:shared", "principal:shared"); !errors.Is(err, approverclass.ErrSharedOwner) {
			t.Fatalf("RequireDistinct on a shared owner = %v, want ErrSharedOwner", err)
		}
	})

	t.Run("refuses when the finance principal is unresolved", func(t *testing.T) {
		if err := approverclass.RequireDistinct("", "principal:manager"); !errors.Is(err, approverclass.ErrUnresolved) {
			t.Fatalf("RequireDistinct with no finance approver = %v, want ErrUnresolved", err)
		}
	})

	t.Run("refuses when the manager principal is unresolved", func(t *testing.T) {
		if err := approverclass.RequireDistinct("principal:finance", ""); !errors.Is(err, approverclass.ErrUnresolved) {
			t.Fatalf("RequireDistinct with no manager approver = %v, want ErrUnresolved", err)
		}
	})

	t.Run("refuses when both are unresolved, never treating two empties as distinct", func(t *testing.T) {
		if err := approverclass.RequireDistinct("", ""); !errors.Is(err, approverclass.ErrUnresolved) {
			t.Fatalf("RequireDistinct(\"\", \"\") = %v, want ErrUnresolved (not ErrSharedOwner and not nil)", err)
		}
	})
}

// TestDeriveDistinctThenRequireDistinct proves the two functions compose the
// way PROMOUX-003's callers rely on: deriving both classes from any single
// base approver always satisfies RequireDistinct, which is what makes "the
// work items share an undifferentiated owner" unrepresentable for a caller
// that always derives before compiling or routing.
func TestDeriveDistinctThenRequireDistinct(t *testing.T) {
	for _, base := range []string{"principal:promotion-approver", "principal:someone-else", "x"} {
		finance, err := approverclass.DeriveDistinct(base, approverclass.FinancePartner)
		if err != nil {
			t.Fatalf("DeriveDistinct(%q, FinancePartner): %v", base, err)
		}
		manager, err := approverclass.DeriveDistinct(base, approverclass.CurrentManager)
		if err != nil {
			t.Fatalf("DeriveDistinct(%q, CurrentManager): %v", base, err)
		}
		if err := approverclass.RequireDistinct(finance, manager); err != nil {
			t.Fatalf("base %q: RequireDistinct(%q, %q) = %v, want nil", base, finance, manager, err)
		}
	}
}
