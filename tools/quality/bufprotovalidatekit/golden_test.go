package bufprotovalidatekit_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTodo_LIB_019_Golden pins safe owned violation projection. Descriptions
// are static and cannot leak the rejected field value or dynamic type URL.
func TestTodo_LIB_019_Golden(t *testing.T) {
	validator, descriptor := fixtureValidator(t)
	violations := validator.Validate(requestMessage(descriptor, "", "secret-123", "type.googleapis.com/private.Secret", []byte("credential")))
	got, err := json.MarshalIndent(violations, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "quality", "bufprotovalidatekit", "testdata", "violations.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("violation golden changed\ngot:\n%s\nwant:\n%s", got, want)
	}
}
