package fixture

import "testing"

func TestFixturePattern(t *testing.T) {
	if FunctionPattern(`fixture`) == nil {
		t.Fatal("fixture pattern is nil")
	}
}
