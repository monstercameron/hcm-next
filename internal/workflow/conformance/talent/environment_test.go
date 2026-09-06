package talent

import "testing"

// TestEnvironment_RegistryPublishesEveryCapability is a smoke check that
// [Environment.Registry] publishes every capability id [CapabilityIDs]
// declares. The substantive behavior of each handler is exercised end to end
// by conformance_test.go's TestTodo_CONF_012 matrix.
func TestEnvironment_RegistryPublishesEveryCapability(t *testing.T) {
	env := GoldenEnvironment()
	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	for _, key := range CapabilityIDs() {
		if _, ok := registry.Lookup(key); !ok {
			t.Errorf("registry does not publish %s", key)
		}
	}
}
