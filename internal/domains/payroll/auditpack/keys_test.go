package auditpack_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack"
)

// TestIdempotencyKeyIsAPureFunctionOfTenantAndRun proves the doc's central
// claim about "immutable": the key is recomputed identically every time from
// (tenant, run id) alone, with nothing else - not a clock, not a resolved
// amount - folded in.
func TestIdempotencyKeyIsAPureFunctionOfTenantAndRun(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	a := auditpack.IdempotencyKey(tenant, "run-1")
	b := auditpack.IdempotencyKey(tenant, "run-1")
	if a == "" {
		t.Fatal("IdempotencyKey returned an empty key")
	}
	if a != b {
		t.Fatalf("two calls with identical inputs produced %q and %q", a, b)
	}
}

func TestIdempotencyKeyIsSensitiveToTenantAndRun(t *testing.T) {
	t.Parallel()
	tenantA, tenantB := uuid.New(), uuid.New()
	byTenant := auditpack.IdempotencyKey(tenantA, "run-1")
	if other := auditpack.IdempotencyKey(tenantB, "run-1"); other == byTenant {
		t.Fatal("two different tenants over the same run id produced the same key")
	}
	byRun := auditpack.IdempotencyKey(tenantA, "run-2")
	if byRun == byTenant {
		t.Fatal("two different runs for the same tenant produced the same key")
	}
}

func TestIdempotencyKeyIsHexSHA256Shaped(t *testing.T) {
	t.Parallel()
	key := auditpack.IdempotencyKey(uuid.New(), "run-1")
	if len(key) != 64 {
		t.Fatalf("key length = %d, want 64", len(key))
	}
	for _, r := range key {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("key %q is not lower-case hex", key)
		}
	}
}
