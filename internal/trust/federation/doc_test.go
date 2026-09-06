package federation

import (
	"errors"
	"testing"
)

func TestDoc_Smoke(t *testing.T) {
	if NewStaticKeySource() == nil {
		t.Fatal("NewStaticKeySource returned nil")
	}
}

func TestDoc_NoPanic(t *testing.T) {
	if _, err := NewValidator(Config{}); !errors.Is(err, ErrValidatorAudience) {
		t.Fatalf("empty package configuration error = %v, want ErrValidatorAudience", err)
	}
}
