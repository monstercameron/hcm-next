package conflictstore_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/conflictstore"
	"github.com/monstercameron/hcm-next/internal/transaction/conflict"
)

func TestStoreRegisterValidatesBeforeUsingCallerTransaction(t *testing.T) {
	err := conflictstore.New().Register(context.Background(), nil, conflict.WriteIntent{ID: "intent"})
	if !errors.Is(err, conflict.ErrInvalidIntent) {
		t.Fatalf("Register error = %v, want invalid intent", err)
	}
}

func TestStoreValidateAtCommitRejectsIncompleteBindingBeforeUsingTransaction(t *testing.T) {
	_, err := conflictstore.New().ValidateAtCommit(context.Background(), nil, conflict.CommitRequest{})
	if !errors.Is(err, conflict.ErrInvalidIntent) {
		t.Fatalf("ValidateAtCommit error = %v, want invalid intent", err)
	}
}
