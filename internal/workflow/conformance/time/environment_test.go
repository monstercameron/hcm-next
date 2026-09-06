package time

import "testing"

func TestEnvironment_RegistryPublishesEveryCapability(t *testing.T) {
	registry, err := GoldenEnvironment().Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	for _, key := range CapabilityIDs() {
		if _, ok := registry.Lookup(key); !ok {
			t.Errorf("registry does not publish %s", key)
		}
	}
}
