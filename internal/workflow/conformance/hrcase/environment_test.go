package hrcase

import "testing"

func TestEnvironment_RegistryPublishesEveryCapability(t *testing.T) {
	registry, err := GoldenEnvironment().Registry()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range CapabilityIDs() {
		if _, ok := registry.Lookup(key); !ok {
			t.Errorf("registry does not publish %s", key)
		}
	}
}
