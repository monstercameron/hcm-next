package artifacts

import (
	"testing"

	"github.com/google/uuid"
)

func TestSubscriptionID_DerivedAndStable(t *testing.T) {
	t.Parallel()
	tenant, instance := uuid.New(), uuid.New()
	a := SubscriptionID(tenant, instance, "node-a", "promotion.finance_ack")
	b := SubscriptionID(tenant, instance, "node-a", "promotion.finance_ack")
	if a != b {
		t.Fatalf("the same tuple derived two identities: %s vs %s", a, b)
	}
	if a == uuid.Nil {
		t.Fatal("derived the nil UUID")
	}
	for _, other := range []uuid.UUID{
		SubscriptionID(tenant, instance, "node-b", "promotion.finance_ack"),
		SubscriptionID(tenant, uuid.New(), "node-a", "promotion.finance_ack"),
		SubscriptionID(uuid.New(), instance, "node-a", "promotion.finance_ack"),
		SubscriptionID(tenant, instance, "node-a", "promotion.other"),
	} {
		if other == a {
			t.Fatalf("a different tuple derived the same identity %s", a)
		}
	}
}

// The correlation key is deliberately outside the identity: the schema allows
// one subscription per (instance, node, signal), so folding the key in would
// mint an id that collides on the constraint rather than on the primary key.
func TestSubscriptionID_IgnoresCorrelationKeyByConstruction(t *testing.T) {
	t.Parallel()
	tenant, instance := uuid.New(), uuid.New()
	if got := SubscriptionID(tenant, instance, "node-a", "sig"); got != SubscriptionID(tenant, instance, "node-a", "sig") {
		t.Fatalf("SubscriptionID is not a pure function of its four arguments")
	}
	if childModeDetach != "DETACH" {
		t.Fatalf("childModeDetach = %q, want DETACH", childModeDetach)
	}
}
