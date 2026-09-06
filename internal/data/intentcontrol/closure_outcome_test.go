package intentcontrol

import (
	"context"
	"testing"
)

func TestCloseIdempotentIsExported(t *testing.T) {
	var _ interface {
		CloseIdempotent(context.Context, Executor, Closure) error
	} = ClosureStore{}
}
