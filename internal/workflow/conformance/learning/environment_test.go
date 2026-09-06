package learning

import "testing"

// TestEnvironment_RegistryPublishesEveryCapability is a smoke check that
// [Environment.Registry] publishes every capability id [CapabilityIDs]
// declares. The substantive behavior of each handler is exercised end to
// end by conformance_test.go's TestTodo_CONF_013 matrix.
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

// TestEnvironment_EvidenceExpiryParsesTheDeclaredDate is a smoke check that
// the fixture's declared evidence expiry text is a well-formed local date.
func TestEnvironment_EvidenceExpiryParsesTheDeclaredDate(t *testing.T) {
	for name, env := range map[string]*Environment{
		"golden":   GoldenEnvironment(),
		"expired":  ExpiredEnvironment(),
		"expiring": ExpiringEnvironment(),
	} {
		if _, err := env.EvidenceExpiry(); err != nil {
			t.Errorf("%s: EvidenceExpiry: %v", name, err)
		}
	}
}
