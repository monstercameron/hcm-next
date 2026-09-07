package pgtest_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

// Every parallel test on one embedded server migrates its own schema from
// zero, so migrations that touch cluster-wide objects (the hcmnext_app role
// in 00008) run concurrently across sessions. Before 00008 serialised its
// role creation, a second session could swallow the duplicate CREATE ROLE
// while the first was still uncommitted and then fail on COMMENT ON ROLE
// with "role does not exist". Eight schemas migrating at once keep that
// window exercised.
func TestMigrationsApplyConcurrentlyFromZero(t *testing.T) {
	for i := range 8 {
		t.Run("schema-"+strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			db := pgtest.NewEmpty(t)
			results, err := db.Provider(t).Up(context.Background())
			if err != nil {
				t.Fatalf("migrate up from zero concurrently: %v", err)
			}
			if len(results) == 0 {
				t.Fatal("no migrations were applied")
			}
		})
	}
}
