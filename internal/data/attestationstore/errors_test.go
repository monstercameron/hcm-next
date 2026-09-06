package attestationstore

import (
	"errors"
	"fmt"
	"testing"
)

func TestAttestationStore_ErrorCodesAndWrapping(t *testing.T) {
	cause := errors.New("cause")
	tests := []struct {
		name string
		code Code
		want string
	}{
		{name: "invalid", code: CodeInvalid, want: "INVALID table key: detail"},
		{name: "not found", code: CodeNotFound, want: "NOT_FOUND"},
		{name: "duplicate revision", code: CodeDuplicateRevision, want: "DUPLICATE_REVISION table key: cause"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.name == "invalid" {
				err = invalid("table", "key", "detail")
			} else if tt.name == "not found" {
				err = failure(tt.code, "", "", nil)
			} else {
				err = failure(tt.code, "table", "key", cause)
			}
			if got := CodeOf(err); got != tt.code {
				t.Fatalf("CodeOf = %q, want %q", got, tt.code)
			}
			if got := err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
			if tt.name == "duplicate revision" && !errors.Is(err, cause) {
				t.Fatal("typed error did not unwrap its cause")
			}
		})
	}
	if got := CodeOf(fmt.Errorf("wrapped: %w", failure(CodeDatabase, "t", "k", cause))); got != CodeDatabase {
		t.Fatalf("CodeOf wrapped = %q", got)
	}
	if got := CodeOf(errors.New("plain")); got != "" {
		t.Fatalf("CodeOf plain = %q", got)
	}
	var nilError *Error
	if nilError != nil {
		t.Fatal("nil typed error unexpectedly non-nil")
	}
}
