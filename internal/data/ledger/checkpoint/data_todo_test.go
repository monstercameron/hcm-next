package checkpoint_test

import "testing"

func TestTodo_DATA_013(t *testing.T)          { TestTodo_LEDGER_010(t) }
func TestTodo_DATA_013_Golden(t *testing.T)   { TestTodo_LEDGER_010_Golden(t) }
func TestTodo_DATA_013_Security(t *testing.T) { TestTodo_LEDGER_010_Security(t) }
func TestTodo_DATA_013_Recovery(t *testing.T) { TestTodo_LEDGER_010(t) }
