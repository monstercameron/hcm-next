package artifacts

import (
	"testing"
)

// A hole in the port set would migrate zero artifacts of that kind in
// silence, which is exactly the loss this ticket exists to prevent. Every
// port must therefore be required, one by one.
func TestPorts_ValidateRefusesEveryHole(t *testing.T) {
	t.Parallel()
	full := newMemoryPorts().ports()
	if err := full.validate(); err != nil {
		t.Fatalf("a complete port set was refused: %v", err)
	}

	holes := map[string]func(p *Ports){
		"Leases":        func(p *Ports) { p.Leases = nil },
		"Timers":        func(p *Ports) { p.Timers = nil },
		"Signals":       func(p *Ports) { p.Signals = nil },
		"ReadyWork":     func(p *Ports) { p.ReadyWork = nil },
		"Approvals":     func(p *Ports) { p.Approvals = nil },
		"Children":      func(p *Ports) { p.Children = nil },
		"Continuations": func(p *Ports) { p.Continuations = nil },
	}
	for name, punch := range holes {
		ports := full
		punch(&ports)
		err := ports.validate()
		if err == nil {
			t.Fatalf("a port set missing %s was accepted", name)
		}
		if got := CodeOf(err); got != CodeMissingPort {
			t.Fatalf("missing %s refused with %q, want %q", name, got, CodeMissingPort)
		}
		if !contains(err.Error(), name) {
			t.Fatalf("missing %s produced %q, which does not name the port", name, err.Error())
		}
	}
}

func TestChildRow_OwedExcludesDetachedChildren(t *testing.T) {
	t.Parallel()
	if !(ChildRow{Mode: "AWAIT"}).Owed() {
		t.Fatal("an AWAIT child is not owed")
	}
	if !(ChildRow{Mode: "COMPENSATE_ON_FAILURE"}).Owed() {
		t.Fatal("a COMPENSATE_ON_FAILURE child is not owed; the parent still hears about its failure")
	}
	if (ChildRow{Mode: "DETACH"}).Owed() {
		t.Fatal("a DETACH child is owed; nothing reports back")
	}
}

// The default set is the durable one, and it must satisfy its own validation
// rather than relying on a caller noticing a nil.
func TestDefaultPorts_IsComplete(t *testing.T) {
	t.Parallel()
	if err := DefaultPorts().validate(); err != nil {
		t.Fatalf("DefaultPorts() is incomplete: %v", err)
	}
}
