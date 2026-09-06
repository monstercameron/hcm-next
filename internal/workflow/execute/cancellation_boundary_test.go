package execute

import (
	"context"
	"testing"

	transactioncancel "github.com/monstercameron/hcm-next/internal/transaction/cancel"
)

func TestGovernedCancellationRequiresDatabase(t *testing.T) {
	var d *Driver
	if _, err := d.CancelGoverned(context.Background(), transactioncancel.Request{}, nil); err == nil {
		t.Fatal("nil driver accepted governed cancellation")
	}
}
