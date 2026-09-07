package intentcontrol_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/intentcontrol"
)

func TestPlanCompileNamesTheEffectThatFailed(t *testing.T) {
	effects := referencePlanEffects()
	second := effects[0]
	second.EffectID = "effect-notify-benefits"
	second.IdempotencyKey = "benefits-1"
	effects = append(effects, second)
	plan := referencePlan(uuid.New(), uuid.New(), 1, "commit")
	sentinel := errors.New("disk on fire")

	// The plan header is statement 1; effects follow in order.
	for _, tc := range []struct {
		name   string
		failAt int
		want   string
	}{
		{name: "first effect", failAt: 2, want: "record plan effect effect-notify-payroll"},
		{name: "second effect", failAt: 3, want: "record plan effect effect-notify-benefits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := &failingExecutor{failAt: tc.failAt, err: sentinel}
			err := intentcontrol.PlanStore{}.Compile(context.Background(), ex, plan, effects)
			if err == nil {
				t.Fatal("Compile succeeded although an effect insert failed")
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("error %v does not wrap the executor failure", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
			if len(ex.seen) != tc.failAt {
				t.Fatalf("executor saw %d statements after the failure at %d", len(ex.seen), tc.failAt)
			}
			if !strings.Contains(ex.seen[len(ex.seen)-1], "transaction_plan_effect") {
				t.Fatalf("the failing statement was not an effect insert: %s", ex.seen[len(ex.seen)-1])
			}
		})
	}
}
