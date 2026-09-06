package timeauth

import (
	"errors"
	"strings"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestErrors_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestErrors_ErrorFormatsAndUnwrapsCause(t *testing.T) {
	cause := ErrInvalidSample
	err := newError("Observe", cause, "field %s", "wall")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "Observe") || !strings.Contains(err.Error(), "field wall") {
		t.Fatalf("error = %q, want op/detail/cause", err)
	}
	if (&Error{Cause: cause}).Error() != cause.Error() {
		t.Fatalf("empty op/detail formatting changed")
	}
	if (&Error{Op: "op", Cause: cause}).Error() != "op: "+cause.Error() {
		t.Fatalf("op-only formatting changed")
	}
	if (&Error{Detail: "detail", Cause: cause}).Error() != cause.Error()+": detail" {
		t.Fatalf("detail-only formatting changed")
	}
	if !errors.Is(err, err.Unwrap()) {
		t.Fatalf("Unwrap did not return cause")
	}
}
