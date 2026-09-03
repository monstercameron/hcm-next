package capability

import "testing"

func TestErrors_CodesNonEmpty(t *testing.T) {
	codes := []string{CodeFieldRequired, CodeInvalidEffectClass, CodeImplementationUnbound, CodeAlreadyRegistered, CodeUnknownCapability, CodeCapabilityDisabled, CodeUnauthorized, CodeWriteEffectRefusedP1A, CodeHandlerFailed}
	for _, c := range codes {
		if c == "" {
			t.Fatal("empty code")
		}
	}
}

func TestErrors_ErrDefinitionInvalid(t *testing.T) {
	e := ErrDefinitionInvalid{ID: "test", Version: 1, Field: "OwnerDomain", Reason: "is required"}
	if e.Code() != CodeFieldRequired {
		t.Fatalf("code %s", e.Code())
	}
	if e.Error() == "" {
		t.Fatal("empty error")
	}
}

func TestErrors_ErrAlreadyRegistered(t *testing.T) {
	e := ErrAlreadyRegistered{ID: "a", Version: 1}
	if e.Code() != CodeAlreadyRegistered {
		t.Fatalf("code %s", e.Code())
	}
	if e.Error() == "" {
		t.Fatal("empty")
	}
}

func TestErrors_GatewayError(t *testing.T) {
	e := &GatewayError{Code: CodeUnauthorized, Capability: "x", Version: 1, Reason: "nope", EvidenceID: "ev1"}
	if e.Error() == "" {
		t.Fatal("empty")
	}
}

func TestErrors_ErrImplementationUnbound(t *testing.T) {
	e := ErrImplementationUnbound{ID: "x", Version: 1}
	if e.Code() != CodeImplementationUnbound {
		t.Fatalf("code %s", e.Code())
	}
}
