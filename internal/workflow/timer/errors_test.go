package timer

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestErrors_EverySentinelAndCodeIsDeclared(t *testing.T) {
	for name, sentinel := range map[string]error{
		"ErrTimer": ErrTimer, "ErrInvalid": ErrInvalid, "ErrReviewRequired": ErrReviewRequired,
		"ErrRequirementDrift": ErrRequirementDrift, "ErrNotFound": ErrNotFound,
		"ErrAlreadySettled": ErrAlreadySettled, "ErrMisfirePolicyRequired": ErrMisfirePolicyRequired,
		"ErrStorage": ErrStorage,
	} {
		if sentinel == nil || sentinel.Error() == "" {
			t.Fatalf("%s is nil or has an empty message", name)
		}
	}
	for name, code := range map[string]string{
		"CodeInvalid": CodeInvalid, "CodeReviewRequired": CodeReviewRequired,
		"CodeRequirementDrift": CodeRequirementDrift, "CodeNotFound": CodeNotFound,
		"CodeAlreadySettled": CodeAlreadySettled, "CodeMisfirePolicyRequired": CodeMisfirePolicyRequired,
		"CodeFenceRefused": CodeFenceRefused, "CodeStorageFailed": CodeStorageFailed,
		"CodeReadyWorkConflict": CodeReadyWorkConflict, "CodeAttemptResolutionError": CodeAttemptResolutionError,
	} {
		if code == "" {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestErrors_RefusalNamesTheTimerItHappenedTo(t *testing.T) {
	timerID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	instanceID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	err := refuse(CodeAlreadySettled, ErrAlreadySettled,
		location{timerID: timerID, instanceID: instanceID, nodeID: "wait.effective_date"},
		"already settled by %s", "another caller")

	if !errors.Is(err, ErrAlreadySettled) || !errors.Is(err, ErrTimer) {
		t.Fatalf("refusal does not classify: %v", err)
	}
	if got := CodeOf(err); got != CodeAlreadySettled {
		t.Fatalf("CodeOf = %q, want %q", got, CodeAlreadySettled)
	}
	msg := err.Error()
	for _, want := range []string{CodeAlreadySettled, timerID.String(), instanceID.String(), "wait.effective_date"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message %q does not name %q", msg, want)
		}
	}
}

func TestErrors_WrappedCauseSurvives(t *testing.T) {
	cause := errors.New("underlying driver failure")
	err := wrapErr(CodeStorageFailed, ErrStorage, location{}, cause, "write the timer row")
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
