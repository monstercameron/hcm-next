package recover

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrors_SentinelsAndCodesArePresent(t *testing.T) {
	for name, err := range map[string]error{
		"ErrRecover":        ErrRecover,
		"ErrInvalid":        ErrInvalid,
		"ErrLeaseLive":      ErrLeaseLive,
		"ErrNotRecoverable": ErrNotRecoverable,
		"ErrFenceRefused":   ErrFenceRefused,
		"ErrCrashed":        ErrCrashed,
		"ErrEffect":         ErrEffect,
		"ErrStorage":        ErrStorage,
	} {
		if err == nil {
			t.Fatalf("%s is nil", name)
		}
		if err.Error() == "" {
			t.Fatalf("%s has an empty message", name)
		}
	}
	for name, code := range map[string]string{
		"CodeInvalid":        CodeInvalid,
		"CodeLeaseLive":      CodeLeaseLive,
		"CodeNotRecoverable": CodeNotRecoverable,
		"CodeFenceRefused":   CodeFenceRefused,
		"CodeCrashInjected":  CodeCrashInjected,
		"CodeEffectFailed":   CodeEffectFailed,
		"CodeStorageFailed":  CodeStorageFailed,
	} {
		if code == "" {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestErrors_RefusalClassifiesAndCarriesItsCode(t *testing.T) {
	err := refuse(CodeLeaseLive, ErrLeaseLive, "instance-1", "node-a", "holder %s is alive", "worker-1")
	if !errors.Is(err, ErrRecover) {
		t.Fatalf("refusal does not unwrap to ErrRecover: %v", err)
	}
	if !errors.Is(err, ErrLeaseLive) {
		t.Fatalf("refusal does not classify as ErrLeaseLive: %v", err)
	}
	if got := CodeOf(err); got != CodeLeaseLive {
		t.Fatalf("CodeOf = %q, want %q", got, CodeLeaseLive)
	}
	if got := err.Error(); got == "" {
		t.Fatalf("empty message")
	}
}

func TestErrors_WrapKeepsTheUnderlyingCause(t *testing.T) {
	cause := fmt.Errorf("underlying")
	err := wrap(CodeStorageFailed, ErrStorage, "instance-1", "node-a", cause, "read the row")
	if !errors.Is(err, cause) {
		t.Fatalf("wrapped refusal lost its cause: %v", err)
	}
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("wrapped refusal does not classify as ErrStorage: %v", err)
	}
}

func TestErrors_CodeOfAndPhaseOfIgnoreForeignErrors(t *testing.T) {
	if got := CodeOf(errors.New("not ours")); got != "" {
		t.Fatalf("CodeOf of a foreign error = %q, want empty", got)
	}
	if got := PhaseOf(errors.New("not ours")); got != "" {
		t.Fatalf("PhaseOf of a foreign error = %q, want empty", got)
	}
	e := refuse(CodeCrashInjected, ErrCrashed, "instance-1", "node-a", "died")
	e.Phase = PhaseAfterDispatchBeforeResultCommit
	if got := PhaseOf(e); got != PhaseAfterDispatchBeforeResultCommit {
		t.Fatalf("PhaseOf = %q, want %q", got, PhaseAfterDispatchBeforeResultCommit)
	}
}

func TestErrors_InvalidIsRaisedWithoutALocation(t *testing.T) {
	err := invalid("no plan supplied")
	if got := CodeOf(err); got != CodeInvalid {
		t.Fatalf("CodeOf = %q, want %q", got, CodeInvalid)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid does not classify as ErrInvalid: %v", err)
	}
}
