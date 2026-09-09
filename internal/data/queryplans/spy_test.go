package queryplans_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/queryplans"
)

// fakeConn is a minimal dbport.Conn that records nothing itself; Spy is what
// this test proves does the recording, so the wrapped conn only needs to
// answer without erroring.
type fakeConn struct{}

func (fakeConn) Exec(ctx context.Context, sqlText string, args ...any) (int64, error) {
	return 0, nil
}

func (fakeConn) Query(ctx context.Context, sqlText string, args ...any) (dbport.Rows, error) {
	return nil, errors.New("fakeConn: Query is not implemented")
}

func (fakeConn) QueryRow(ctx context.Context, sqlText string, args ...any) dbport.Row {
	return fakeRow{value: "{}"}
}

func TestSpy_QueryRowIsRecordedAndForwarded(t *testing.T) {
	t.Parallel()
	spy := queryplans.NewSpy(fakeConn{})
	row := spy.QueryRow(context.Background(), "SELECT 1 FROM journey_worker WHERE tenant_id = $1", "tenant-a")
	var out string
	if err := row.Scan(&out); err != nil {
		t.Fatalf("Scan on the forwarded row: %v", err)
	}
	if len(spy.Calls) != 1 {
		t.Fatalf("len(Calls) = %d, want 1", len(spy.Calls))
	}
	if spy.Calls[0].SQL != "SELECT 1 FROM journey_worker WHERE tenant_id = $1" {
		t.Errorf("Calls[0].SQL = %q", spy.Calls[0].SQL)
	}
	if len(spy.Calls[0].Args) != 1 || spy.Calls[0].Args[0] != "tenant-a" {
		t.Errorf("Calls[0].Args = %v, want [tenant-a]", spy.Calls[0].Args)
	}
}

func TestSpy_CallAgainstReturnsTheLastMatchingCall(t *testing.T) {
	t.Parallel()
	spy := queryplans.NewSpy(fakeConn{})
	spy.QueryRow(context.Background(), "SELECT 1 FROM ledger_event WHERE tenant_id = $1", "t1")
	spy.QueryRow(context.Background(), "SELECT authority_ref FROM authority_assignment WHERE tenant_id = $1", "t1")
	spy.QueryRow(context.Background(), "SELECT 2 FROM ledger_event WHERE stream_key = $1", "s1")

	call, ok := spy.CallAgainst("ledger_event")
	if !ok {
		t.Fatal("CallAgainst(ledger_event) found nothing")
	}
	if call.SQL != "SELECT 2 FROM ledger_event WHERE stream_key = $1" {
		t.Errorf("CallAgainst returned %q, want the LAST ledger_event call", call.SQL)
	}

	if _, ok := spy.CallAgainst("no_such_table"); ok {
		t.Error("CallAgainst(no_such_table) = found, want not found")
	}
}

func TestSpy_ExecIsForwardedButNotRecorded(t *testing.T) {
	t.Parallel()
	spy := queryplans.NewSpy(fakeConn{})
	if _, err := spy.Exec(context.Background(), "INSERT INTO journey_worker DEFAULT VALUES"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if len(spy.Calls) != 0 {
		t.Errorf("len(Calls) = %d after Exec alone, want 0 (Spy records reads, not writes)", len(spy.Calls))
	}
}
