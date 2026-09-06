package pgstore

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent/app"
)

func TestOutcomeBinderContract(t *testing.T) {
	var _ app.OutcomeBinder = (*Store)(nil)
	if StreamKey("intent-1") != "intent:intent-1" {
		t.Fatalf("StreamKey contract changed")
	}
}
