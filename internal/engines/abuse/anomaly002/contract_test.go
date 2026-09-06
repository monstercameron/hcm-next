package anomaly002_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/abuse/anomaly002"
)

func TestVersionAndExplain(t *testing.T) {
	if anomaly002.Version() <= 0 || anomaly002.Version() != anomaly002.Version() {
		t.Fatalf("Version() = %d, want stable positive version", anomaly002.Version())
	}
	exp := anomaly002.Explain(anomaly002.Activity{}, anomaly002.Definition{})
	if exp.Applicable || exp.Reason != "DEFINITION_INVALID" || len(exp.Inputs) == 0 {
		t.Fatalf("Explain() = %+v", exp)
	}
}
