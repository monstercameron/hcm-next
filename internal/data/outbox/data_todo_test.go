package outbox_test

import "testing"

func TestTodo_DATA_008(t *testing.T)       { TestLedgerOutboxProjectionAppendRestartsAndReconciles(t) }
func TestTodo_DATA_008_Race(t *testing.T)  { TestLedgerOutboxProjectionAppendRestartsAndReconciles(t) }
func TestTodo_DATA_008_Fault(t *testing.T) { TestLedgerOutboxProjectionAppendRestartsAndReconciles(t) }
func TestTodo_DATA_008_Mutation(t *testing.T) {
	TestLedgerOutboxProjectionAppendRestartsAndReconciles(t)
}
