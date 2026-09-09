package blockshared

import (
	"bytes"
	"encoding/json"

	"human-capital-management-suite-executor/internal/executor"
)

// DecodeStrict decodes block input while rejecting unknown fields.
func DecodeStrict[T any](rawInput json.RawMessage, invalidInputMessage string) (T, *executor.ExecutionError) {
	var input T
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return input, executor.InvalidInputError(invalidInputMessage, map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}
