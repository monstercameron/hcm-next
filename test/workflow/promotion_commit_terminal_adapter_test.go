package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/execute"
	"github.com/monstercameron/hcm-next/internal/workflow/execute/effects"
)

// TestTodo_TX_004_PromotionTerminalAdapter proves the additive adapter can be
// installed at the existing TerminalWriter port without changing Promotion's
// terminal continuation: the end-to-end fixture still records exactly one
// governed terminal fact.
func TestTodo_TX_004_PromotionTerminalAdapter(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	f := runPromotionToCompleteWithTerminal(t, "tx004-terminal-adapter", at,
		func(_ *testing.T, _ *pgtest.DB, _ uuid.UUID, _ intent.ProposalRevision, _ *workflow.CompiledWorkflow, base *effects.LedgerTerminalWriter) execute.TerminalWriter {
			return &effects.CommitTerminalWriter{Next: base}
		})
	var facts int
	if err := f.db.Conn.QueryRow(context.Background(), `
		SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`, f.tenantID, f.streamKey).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if facts != 1 {
		t.Fatalf("promotion terminal facts = %d, want exactly one", facts)
	}
}
