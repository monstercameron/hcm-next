package mobility

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

func TestEnvironment_ScenariosRetainDatedLegsAndPayrollSides(t *testing.T) {
	env := GoldenEnvironment()
	if env.LegStartText == "" || env.LegEndText == "" || env.HomePayrollGroup == "" || env.HostPayrollGroup == "" {
		t.Fatal("golden environment omitted dated leg or home/host payroll evidence")
	}
	if env.HomePayrollGroup == env.HostPayrollGroup {
		t.Fatal("home and host payroll groups must remain distinct")
	}
}
