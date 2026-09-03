package bufprotovalidatekit_test

import "testing"

// FuzzTodo_LIB_019 exercises hostile local values and dynamic type URLs. An
// unregistered Any must never be accepted and validation must not panic.
func FuzzTodo_LIB_019(f *testing.F) {
	for _, seed := range []struct {
		workerID string
		note     string
		typeURL  string
		payload  []byte
	}{
		{"worker", "ok", "type.googleapis.com/qualification.v1.Attachment", []byte{1}},
		{"", "123456789", "type.googleapis.com/evil.Payload", []byte("secret")},
		{"worker", string([]byte{0xff}), "", nil},
	} {
		f.Add(seed.workerID, seed.note, seed.typeURL, seed.payload)
	}
	validator, descriptor := fixtureValidator(f)
	safeDescriptions := map[string]bool{
		"required field is absent":                 true,
		"field is not valid UTF-8":                 true,
		"field exceeds 8 bytes":                    true,
		"dynamic message type is not registered":   true,
		"dynamic message payload is empty":         true,
		"dynamic message has an invalid structure": true,
	}
	f.Fuzz(func(t *testing.T, workerID, note, typeURL string, payload []byte) {
		violations := validator.Validate(requestMessage(descriptor, workerID, note, typeURL, payload))
		registered := typeURL == "" || typeURL == "type.googleapis.com/qualification.v1.Attachment"
		if !registered && !hasViolation(violations, "ANY_TYPE_NOT_ALLOWED") {
			t.Fatalf("unregistered Any accepted: %q", typeURL)
		}
		for _, violation := range violations {
			if !safeDescriptions[violation.Description] {
				t.Fatalf("violation description is not from the static safe catalog: %+v", violation)
			}
		}
	})
}
