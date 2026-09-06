package rebuild_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/projection"
	"github.com/monstercameron/hcm-next/internal/data/rebuild"
)

func TestTodo_LEDGER_013(t *testing.T) {
	if rebuild.NewPromotionOutcomeRebuilder(nil) == nil {
		t.Fatal("nil reader did not produce a usable replay service")
	}
	tenant := uuid.New()
	report, err := projection.RebuildPromotionOutcome([]ledger.EventRecord{{
		Tenant: tenant, StreamKey: "workflow:promotion", Sequence: 1, EventID: uuid.New(),
		SchemaRef: projection.PromotionOutcomeSchemaRef, Digest: strings.Repeat("a", 64), DigestAlgorithm: ledger.Algorithm,
	}}, tenant, "workflow:promotion")
	if err != nil {
		t.Fatal(err)
	}
	if err := rebuild.ComparePromotionOutcome(report, report); err != nil {
		t.Fatalf("wrapper compare: %v", err)
	}
}
