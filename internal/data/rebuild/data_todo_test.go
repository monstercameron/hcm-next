package rebuild_test

import "testing"

func TestTodo_DATA_010(t *testing.T)          { TestRebuildPromotesOnExactMatch(t) }
func TestTodo_DATA_010_Golden(t *testing.T)   { TestRebuildPromotesOnExactMatch(t) }
func TestTodo_DATA_010_Recovery(t *testing.T) { TestRebuildMidRunFailureLeavesNoTrace(t) }
