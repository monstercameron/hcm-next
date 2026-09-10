package anomaly002_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/abuse/anomaly002"
)

func TestVersionAndExplain(t *testing.T) {
	version := anomaly002.Version()
	if version <= 0 {
		t.Fatalf("Version() = %d, want stable positive version", version)
	}
	if again := anomaly002.Version(); again != version {
		t.Fatalf("Version() = %d then %d, want stable version", version, again)
	}
	exp := anomaly002.Explain(anomaly002.Activity{}, anomaly002.Definition{})
	if exp.Applicable || exp.Reason != "DEFINITION_INVALID" || len(exp.Inputs) == 0 {
		t.Fatalf("Explain() = %+v", exp)
	}
}
