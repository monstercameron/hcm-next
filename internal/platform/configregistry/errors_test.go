package configregistry

import (
	"errors"
	"testing"
)

func TestRefuseProducesClassifiableError(t *testing.T) {
	t.Parallel()
	err := refuse(CodeInvalidKind, "obj-1", "kind %q is bad", "BOGUS")
	if !errors.Is(err, ErrConfigRegistry) {
		t.Fatal("refuse()'d error does not unwrap to ErrConfigRegistry")
	}
	if CodeOf(err) != CodeInvalidKind {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeInvalidKind)
	}
	if err.Error() == "" {
		t.Fatal("Error() is empty")
	}
}

func TestWrapCarriesUnderlyingCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying store failure")
	err := wrap(CodeUnknownRevision, "obj-1", cause, "could not resolve")
	if !errors.Is(err, ErrConfigRegistry) {
		t.Fatal("wrap()'d error does not unwrap to ErrConfigRegistry")
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrap()'d error does not unwrap to its cause")
	}
	if CodeOf(err) != CodeUnknownRevision {
		t.Fatalf("CodeOf(err) = %q, want %q", CodeOf(err), CodeUnknownRevision)
	}
}

func TestCodeOfReturnsEmptyForForeignError(t *testing.T) {
	t.Parallel()
	if got := CodeOf(errors.New("not from this package")); got != "" {
		t.Fatalf("CodeOf(foreign error) = %q, want empty", got)
	}
}
