package commit_test

import "testing"

// The ledger commit composition is the landed implementation of DATA-007's
// atomic event/projection/outbox contract.
func TestTodo_DATA_007(t *testing.T)          { TestTodo_LEDGER_008(t) }
func TestTodo_DATA_007_Race(t *testing.T)     { TestTodo_LEDGER_008_Race(t) }
func TestTodo_DATA_007_Mutation(t *testing.T) { TestTodo_LEDGER_008_Mutation(t) }
