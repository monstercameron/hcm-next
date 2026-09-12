package promotioncommit_test

// PROMOUX-005: "Include reporting-line and organization impact in
// management promotions."
//
// TestTodo_PROMOUX_005_Mutation proves the refusal path writes nothing it
// should not, at the one place in this tree where a promotion's manager
// relationship is actually committed: promotioncommit.Writer.Write. Before
// PROMOUX-005, domaincommit.Command.ManagerAncestorWorkerIDs -- the only
// input its own cycle check (domaincommit.ErrManagerCycle) has ever had --
// was never populated by any real graph resolution anywhere in this
// codebase; every production caller and every test fixture either left it
// empty or filled it with an unrelated filler id, which made the check a
// dead letter against real data. internal/domains/org.DetectManagerCycle and
// internal/data/orgfacts (this todo's new real org.WorkerFacts adapter, see
// orgfacts's own TestTodo_PROMOUX_005_Integration) are what a correct caller
// now has to compute that list for real; this test proves that once it is
// computed correctly, the write boundary that has always declared it would
// honor a cycle refusal actually does -- and does so before a single SQL
// statement reaches the database.
import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/promotioncommit"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
)

// mutatingStatement and countingExecutor mirror the pattern
// internal/humanwork/workitem/fixtures_test.go established: wrap a real
// dbport.Tx, count every mutating statement by the table it targets, and
// leave every read to pass straight through unmodified.
var mutatingStatement = regexp.MustCompile(`(?is)^\s*(INSERT INTO|UPDATE|DELETE FROM)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

type countingExecutor struct {
	dbport.Tx
	mu     sync.Mutex
	writes map[string]int
}

func (c *countingExecutor) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	c.record(sql)
	return c.Tx.Exec(ctx, sql, args...)
}

func (c *countingExecutor) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	c.record(sql)
	return c.Tx.QueryRow(ctx, sql, args...)
}

func (c *countingExecutor) record(sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writes == nil {
		c.writes = map[string]int{}
	}
	if m := mutatingStatement.FindStringSubmatch(sql); m != nil {
		c.writes[m[2]]++
	}
}

func TestTodo_PROMOUX_005_Mutation(t *testing.T) {
	f := newFixture(t)
	writer := promotioncommit.Writer{}

	// This is exactly the shape a caller who ran org.DetectManagerCycle (or
	// org.ResolveManagerRelationships directly) upstream would produce: the
	// promoted worker (WorkerID) appears in the proposed manager's own
	// resolved ancestor chain, which is the real multi-hop condition
	// domaincommit.Command.Validate checks for with
	// slices.Contains(ManagerAncestorWorkerIDs, WorkerID) -- not a single-hop
	// "is the manager the worker" comparison, and reachable at any chain
	// depth the upstream resolution walked.
	cmd := f.command(t)
	cmd.ManagerAncestorWorkerIDs = []string{cmd.WorkerID}

	ctx := context.Background()
	tx, err := f.db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	counting := &countingExecutor{Tx: tx}

	_, err = writer.Write(ctx, counting, cmd)
	if !errors.Is(err, domaincommit.ErrManagerCycle) {
		t.Fatalf("Write error = %v, want ErrManagerCycle", err)
	}

	// The zero-mutation proof: Command.Validate runs before Write parses a
	// single identifier or opens the assignment advisory lock, so a cycle
	// refusal must leave the counting executor having recorded no mutating
	// statement against any table at all -- not merely the four Promotion
	// participants, literally none.
	if len(counting.writes) != 0 {
		t.Fatalf("cycle refusal issued mutating statements %v, want none", counting.writes)
	}
}

// TestTodo_PROMOUX_005_MutationAdmitsANonCyclingCommand is the negative half:
// a command whose ancestor list is real but does not contain the worker --
// the same shape a legitimate deep, non-cycling chain produces -- is not
// refused by the cycle check, and the writer proceeds to commit its normal
// four participants. This is what proves the mutation test above is
// exercising the cycle rule specifically, not some unrelated validation
// failure that would refuse any command.
func TestTodo_PROMOUX_005_MutationAdmitsANonCyclingCommand(t *testing.T) {
	f := newFixture(t)
	writer := promotioncommit.Writer{}

	cmd := f.command(t)
	// A deep, unrelated ancestor chain that never reaches the worker.
	cmd.ManagerAncestorWorkerIDs = []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}

	receipt, err := commitCommand(t, f, writer, cmd)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if receipt.AssignmentRowID == "" || receipt.OccupancyRowID == "" || receipt.BasePayRowID == "" || receipt.BudgetRowID == "" {
		t.Fatalf("receipt = %+v, want every participant row id populated", receipt)
	}
}
