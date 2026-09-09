package snapshot_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestInputWatermarkFloorValidate(t *testing.T) {
	t.Parallel()

	valid := snapshot.InputWatermarkFloor{InputName: "people.worker_facts", Minimum: mustRevision(t, "watermark.people.worker_facts", 3)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid floor: %v", err)
	}

	t.Run("missing name", func(t *testing.T) {
		f := valid
		f.InputName = ""
		if err := f.Validate(); !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequirementIncomplete", err)
		}
	})

	t.Run("unspecified minimum", func(t *testing.T) {
		f := valid
		f.Minimum = values.RevisionToken{}
		if err := f.Validate(); !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequirementIncomplete", err)
		}
	})
}

func TestConsistencyRequirementValidate(t *testing.T) {
	t.Parallel()
	horizon := fixtureHorizon(t)

	valid := snapshot.ConsistencyRequirement{
		Tenant:         fixtureTenant,
		KnownAtHorizon: horizon,
		MinWatermarks: []snapshot.InputWatermarkFloor{
			{InputName: "a", Minimum: mustRevision(t, "watermark.a", 1)},
			{InputName: "b", Minimum: mustRevision(t, "watermark.b", 1)},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid requirement: %v", err)
	}

	t.Run("missing tenant", func(t *testing.T) {
		r := valid
		r.Tenant = ""
		if err := r.Validate(); !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequirementIncomplete", err)
		}
	})

	t.Run("missing known-at horizon", func(t *testing.T) {
		r := valid
		r.KnownAtHorizon = values.KnownAt{}
		if err := r.Validate(); !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequirementIncomplete", err)
		}
	})

	t.Run("duplicate watermark floor", func(t *testing.T) {
		r := valid
		r.MinWatermarks = []snapshot.InputWatermarkFloor{
			{InputName: "a", Minimum: mustRevision(t, "watermark.a", 1)},
			{InputName: "a", Minimum: mustRevision(t, "watermark.a", 2)},
		}
		if err := r.Validate(); !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequirementIncomplete", err)
		}
	})

	t.Run("invalid watermark floor propagates", func(t *testing.T) {
		r := valid
		r.MinWatermarks = []snapshot.InputWatermarkFloor{{InputName: "", Minimum: mustRevision(t, "x", 1)}}
		if err := r.Validate(); !errors.Is(err, snapshot.ErrRequirementIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequirementIncomplete", err)
		}
	})
}
