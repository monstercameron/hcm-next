// Package toxiproxykit defines the provider-independent contract used to
// qualify network fault injectors. It deliberately has no Toxiproxy client
// dependency: an integration runner may translate this contract to any
// injector, while these tests prove schedule and oracle semantics locally.
package toxiproxykit

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type FaultKind string

const (
	Latency   FaultKind = "latency"
	Timeout   FaultKind = "timeout"
	Reset     FaultKind = "reset"
	HalfClose FaultKind = "half_close"
	Truncate  FaultKind = "truncation"
	Bandwidth FaultKind = "bandwidth"
)

type Fault struct {
	Kind  FaultKind `yaml:"kind"`
	AtMS  int       `yaml:"at_ms"`
	Value int       `yaml:"value,omitempty"`
}

type Schedule struct {
	Seed   uint64  `yaml:"seed"`
	Faults []Fault `yaml:"faults"`
}

func (s Schedule) Validate() error {
	if s.Seed == 0 {
		return fmt.Errorf("schedule seed must be non-zero")
	}
	last := -1
	for i, f := range s.Faults {
		if f.AtMS < 0 || f.AtMS < last {
			return fmt.Errorf("fault %d: at_ms must be non-negative and sorted", i)
		}
		last = f.AtMS
		switch f.Kind {
		case Latency, Timeout, Reset, HalfClose, Truncate, Bandwidth:
		default:
			return fmt.Errorf("fault %d: unsupported kind %q", i, f.Kind)
		}
		if (f.Kind == Latency || f.Kind == Bandwidth || f.Kind == Truncate) && f.Value <= 0 {
			return fmt.Errorf("fault %d: %s requires positive value", i, f.Kind)
		}
	}
	return nil
}

type Outcome string

const (
	Accepted         Outcome = "accepted"
	RetryableFailure Outcome = "retryable_failure"
	UnknownCommit    Outcome = "unknown_commit"
	Rejected         Outcome = "rejected"
)

type Run struct {
	Outcome         Outcome
	DurableWrites   int
	AcceptedEffects int
	Timeline        []Fault
}

func (r Run) Validate(s Schedule) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if len(r.Timeline) != len(s.Faults) {
		return fmt.Errorf("timeline has %d faults, want %d", len(r.Timeline), len(s.Faults))
	}
	for i := range s.Faults {
		if r.Timeline[i] != s.Faults[i] {
			return fmt.Errorf("timeline fault %d differs from declared schedule", i)
		}
	}
	if r.DurableWrites < 0 || r.AcceptedEffects < 0 {
		return fmt.Errorf("negative oracle count")
	}
	if r.AcceptedEffects > 1 {
		return fmt.Errorf("duplicate accepted effect: %d", r.AcceptedEffects)
	}
	if r.Outcome == Accepted && (r.DurableWrites != 1 || r.AcceptedEffects != 1) {
		return fmt.Errorf("accepted outcome requires exactly one durable write and effect")
	}
	if r.Outcome != Accepted && r.AcceptedEffects != 0 {
		return fmt.Errorf("non-accepted outcome has accepted effect")
	}
	return nil
}

type Qualification struct {
	Version int    `yaml:"version"`
	Tool    string `yaml:"tool"`
	Verdict string `yaml:"verdict"`
	Scope   struct {
		Covers   []string `yaml:"covers"`
		Excludes []string `yaml:"excludes"`
	} `yaml:"scope"`
	Routing  map[string]string `yaml:"routing"`
	Evidence []struct {
		Test    string `yaml:"test"`
		Package string `yaml:"package"`
	} `yaml:"evidence"`
	Command string `yaml:"command"`
}

func LoadQualification(path string) (Qualification, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Qualification{}, fmt.Errorf("toxiproxykit: reading manifest: %w", err)
	}
	var q Qualification
	if err := yaml.Unmarshal(b, &q); err != nil {
		return Qualification{}, fmt.Errorf("toxiproxykit: parsing manifest: %w", err)
	}
	return q, nil
}
