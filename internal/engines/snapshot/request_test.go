package snapshot_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/snapshot"
)

func TestInputRequestValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid unconstrained", func(t *testing.T) {
		r := snapshot.InputRequest{Name: "people.worker_facts"}
		if err := r.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})

	t.Run("valid pinned authority", func(t *testing.T) {
		r := snapshot.InputRequest{Name: "people.worker_facts", RequiredAuthority: snapshot.AuthorityNativeState}
		if err := r.Validate(); err != nil {
			t.Fatalf("Validate() = %v, want nil", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		r := snapshot.InputRequest{}
		if err := r.Validate(); !errors.Is(err, snapshot.ErrRequestIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequestIncomplete", err)
		}
	})

	t.Run("unknown authority class", func(t *testing.T) {
		r := snapshot.InputRequest{Name: "x", RequiredAuthority: snapshot.AuthorityClass("BOGUS")}
		if err := r.Validate(); !errors.Is(err, snapshot.ErrRequestIncomplete) {
			t.Fatalf("Validate() = %v, want ErrRequestIncomplete", err)
		}
	})
}
