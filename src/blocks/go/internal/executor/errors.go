package executor

// InvalidRequestError returns a typed request contract validation error.
func InvalidRequestError(message string, details map[string]any) *ExecutionError {
	return &ExecutionError{
		Code:        ErrorCodeInvalidRequest,
		Message:     message,
		SafeMessage: "The executor request is invalid.",
		Details:     details,
	}
}

// InvalidJSONError returns a typed malformed JSON error.
func InvalidJSONError(message string) *ExecutionError {
	return &ExecutionError{
		Code:        ErrorCodeInvalidJSON,
		Message:     message,
		SafeMessage: "The executor request body must be valid JSON.",
	}
}

// UnknownBlockError returns a typed unknown block error.
func UnknownBlockError(block BlockReference) *ExecutionError {
	return &ExecutionError{
		Code:        ErrorCodeUnknownBlock,
		Message:     "No registered executor block matches the requested name and version.",
		SafeMessage: "The requested executor block is not available.",
		Details: map[string]any{
			"blockName":    block.Name,
			"blockVersion": block.Version,
		},
	}
}

// InvalidInputError returns a typed block input validation error.
func InvalidInputError(message string, details map[string]any) *ExecutionError {
	return &ExecutionError{
		Code:        ErrorCodeInvalidInput,
		Message:     message,
		SafeMessage: "The executor block input is invalid.",
		Details:     details,
	}
}

// ExecutionFailedError returns a typed unexpected execution failure.
func ExecutionFailedError(message string, details map[string]any) *ExecutionError {
	return &ExecutionError{
		Code:        ErrorCodeExecutionFailed,
		Message:     message,
		SafeMessage: "The executor block failed.",
		Details:     details,
	}
}

// MethodNotAllowedError returns a typed HTTP method error.
func MethodNotAllowedError(method string) *ExecutionError {
	return &ExecutionError{
		Code:        ErrorCodeMethodNotAllowed,
		Message:     "Unsupported HTTP method.",
		SafeMessage: "The requested HTTP method is not allowed.",
		Details: map[string]any{
			"method": method,
		},
	}
}
