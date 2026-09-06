package app

import (
	"context"
	"testing"
)

func TestTodo_PROMO_EXEC_TIMERDISPATCH_WithResumeTenant(t *testing.T) {
	ctx := WithResumeTenant(context.Background(), "tenant-a")
	if tenant, ok := resumeTenant(ctx); !ok || tenant != "tenant-a" {
		t.Fatalf("resume tenant = %q, %t; want tenant-a, true", tenant, ok)
	}
}
