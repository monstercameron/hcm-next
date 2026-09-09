package bufprotovalidatekit_test

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	kit "github.com/monstercameron/human-capital-management-suite/tools/quality/bufprotovalidatekit"
)

// TestTodo_LIB_019_Property proves deterministic, transport-neutral results
// over boundary lengths and registered versus unregistered dynamic types.
func TestTodo_LIB_019_Property(t *testing.T) {
	validator, descriptor := fixtureValidator(t)
	rng := rand.New(rand.NewSource(20260903))
	for i := 0; i < 256; i++ {
		length := rng.Intn(17)
		note := strings.Repeat("x", length)
		typeURL := "type.googleapis.com/qualification.v1.Attachment"
		if i%3 == 0 {
			typeURL = "type.googleapis.com/unknown.v1.Payload"
		}
		message := requestMessage(descriptor, "worker", note, typeURL, []byte{1})
		first := validator.Validate(message)
		second := validator.Validate(message)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("case %d is nondeterministic: first=%+v second=%+v", i, first, second)
		}
		hasMax := hasViolation(first, "MAX_BYTES")
		if hasMax != (length > 8) {
			t.Fatalf("length %d: MAX_BYTES=%v violations=%+v", length, hasMax, first)
		}
		hasAny := hasViolation(first, "ANY_TYPE_NOT_ALLOWED")
		if hasAny != (i%3 == 0) {
			t.Fatalf("case %d: ANY_TYPE_NOT_ALLOWED=%v violations=%+v", i, hasAny, first)
		}
	}
}

func hasViolation(violations []kit.Violation, code string) bool {
	for _, violation := range violations {
		if violation.Code == code {
			return true
		}
	}
	return false
}
