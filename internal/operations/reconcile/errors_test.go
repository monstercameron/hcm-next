package reconcile

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestErrors_EverySentinelAndCodeIsDeclared(t *testing.T) {
	for name, sentinel := range map[string]error{
		"ErrReconcile": ErrReconcile, "ErrInvalid": ErrInvalid, "ErrNotFound": ErrNotFound,
		"ErrFenceRefused": ErrFenceRefused, "ErrAlreadyTerminal": ErrAlreadyTerminal,
		"ErrVersionConflict": ErrVersionConflict, "ErrObserverFailed": ErrObserverFailed,
		"ErrComparerFailed": ErrComparerFailed, "ErrStorage": ErrStorage,
	} {
		if sentinel == nil || sentinel.Error() == "" {
			t.Fatalf("%s is nil or has an empty message", name)
		}
	}
	for name, code := range map[string]string{
		"CodeInvalid": CodeInvalid, "CodeNotFound": CodeNotFound, "CodeFenceRefused": CodeFenceRefused,
		"CodeAlreadyTerminal": CodeAlreadyTerminal, "CodeVersionConflict": CodeVersionConflict,
		"CodeObserverFailed": CodeObserverFailed, "CodeComparerFailed": CodeComparerFailed,
		"CodeStorageFailed": CodeStorageFailed, "CodeNotMandatory": CodeNotMandatory,
		"CodeUnexpectedStatus": CodeUnexpectedStatus,
	} {
		if code == "" {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestErrors_RefusalNamesTheTenantAndJobItHappenedTo(t *testing.T) {
	tenant := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	job := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	err := refuse(CodeVersionConflict, ErrVersionConflict, tenant, job, "job is at version %d, expected %d", 2, 1)

	if !errors.Is(err, ErrVersionConflict) || !errors.Is(err, ErrReconcile) {
		t.Fatalf("refusal does not classify: %v", err)
	}
	if got := CodeOf(err); got != CodeVersionConflict {
		t.Fatalf("CodeOf = %q, want %q", got, CodeVersionConflict)
	}
	msg := err.Error()
	for _, want := range []string{CodeVersionConflict, tenant.String(), job.String()} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not name %q", msg, want)
		}
	}
}

func TestErrors_WrappedCauseSurvives(t *testing.T) {
	cause := errors.New("underlying driver failure")
	err := wrapErr(CodeStorageFailed, ErrStorage, uuid.Nil, uuid.Nil, cause, "write the job row")
	if !errors.Is(err, cause) || !errors.Is(err, ErrStorage) {
		t.Fatalf("wrap lost something: %v", err)
	}
	if got := CodeOf(err); got != CodeStorageFailed {
		t.Fatalf("CodeOf = %q, want %q", got, CodeStorageFailed)
	}
}

func TestErrors_CodeOfIgnoresForeignErrors(t *testing.T) {
	if got := CodeOf(errors.New("not ours")); got != "" {
		t.Fatalf("CodeOf on a foreign error = %q, want empty", got)
	}
}

func TestErrors_InvalidUsesTheInvalidCode(t *testing.T) {
	err := invalid(uuid.Nil, uuid.Nil, "bad request")
	if CodeOf(err) != CodeInvalid || !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid() did not produce CodeInvalid/ErrInvalid: %v", err)
	}
}
