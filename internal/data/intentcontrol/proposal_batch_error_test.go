package intentcontrol_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/intentcontrol"
)

// failingExecutor is a dbport executor with no batch support, so ExecAll
// falls back to one Exec per statement; it fails the statement at failAt
// (1-based) and records every SQL text it saw.
type failingExecutor struct {
	failAt int
	err    error
	seen   []string
}

func (f *failingExecutor) Exec(_ context.Context, sql string, _ ...any) (int64, error) {
	f.seen = append(f.seen, sql)
	if len(f.seen) == f.failAt {
		return 0, f.err
	}
	return 1, nil
}

func (f *failingExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (f *failingExecutor) QueryRow(context.Context, string, ...any) dbport.Row {
	return failingRow{}
}

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("unexpected QueryRow") }

func TestProposalSetRecordNamesTheWriteItemThatFailed(t *testing.T) {
	sets := referenceSets()
	second := sets.Writes[0]
	second.FieldPath += ".second"
	sets.Writes = append(sets.Writes, second)
	if err := sets.Validate(); err != nil {
		t.Fatalf("two-write proposal is invalid: %v", err)
	}
	sentinel := errors.New("disk on fire")
	for _, tc := range []struct {
		name   string
		failAt int
		want   string
	}{
		{name: "first write item", failAt: 1, want: "record write item 1"},
		{name: "second write item", failAt: 2, want: "record write item 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := &failingExecutor{failAt: tc.failAt, err: sentinel}
			err := intentcontrol.ProposalSetStore{}.Record(context.Background(), ex, uuid.New(), uuid.New(), 1, sets)
			if err == nil {
				t.Fatal("Record succeeded although a write item failed")
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("error %v does not wrap the executor failure", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
			if len(ex.seen) != tc.failAt {
				t.Fatalf("executor saw %d statements after the failure at %d; the batch must stop at the failing item", len(ex.seen), tc.failAt)
			}
			if !strings.Contains(ex.seen[len(ex.seen)-1], "proposal_write_item") {
				t.Fatalf("the failing statement was not a write item insert: %s", ex.seen[len(ex.seen)-1])
			}
		})
	}
}
