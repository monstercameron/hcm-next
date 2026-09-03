package signal

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrSubscriptionIdentityRequired == nil {
		t.Fatalf("ErrSubscriptionIdentityRequired is nil")
	}
	if ErrSubscriptionCorrelationRequired == nil {
		t.Fatalf("ErrSubscriptionCorrelationRequired is nil")
	}
	if ErrSubscriptionSchemaRequired == nil {
		t.Fatalf("ErrSubscriptionSchemaRequired is nil")
	}
	if ErrSubscriptionSourcesRequired == nil {
		t.Fatalf("ErrSubscriptionSourcesRequired is nil")
	}
	if ErrSubscriptionOrderingInvalid == nil {
		t.Fatalf("ErrSubscriptionOrderingInvalid is nil")
	}
	if ErrVerifierRequired == nil {
		t.Fatalf("ErrVerifierRequired is nil")
	}
	if ErrNowRequired == nil {
		t.Fatalf("ErrNowRequired is nil")
	}
	if ErrPriorEntryWrongSubscription == nil {
		t.Fatalf("ErrPriorEntryWrongSubscription is nil")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrSubscriptionIdentityRequired, ErrSubscriptionIdentityRequired) {
		t.Fatalf("errors.Is failed for ErrSubscriptionIdentityRequired")
	}
	if ErrSubscriptionIdentityRequired.Error() == "" {
		t.Fatalf("ErrSubscriptionIdentityRequired Error empty")
	}
	if !errors.Is(ErrSubscriptionCorrelationRequired, ErrSubscriptionCorrelationRequired) {
		t.Fatalf("errors.Is failed for ErrSubscriptionCorrelationRequired")
	}
	if ErrSubscriptionCorrelationRequired.Error() == "" {
		t.Fatalf("ErrSubscriptionCorrelationRequired Error empty")
	}
	if !errors.Is(ErrSubscriptionSchemaRequired, ErrSubscriptionSchemaRequired) {
		t.Fatalf("errors.Is failed for ErrSubscriptionSchemaRequired")
	}
	if ErrSubscriptionSchemaRequired.Error() == "" {
		t.Fatalf("ErrSubscriptionSchemaRequired Error empty")
	}
	if !errors.Is(ErrSubscriptionSourcesRequired, ErrSubscriptionSourcesRequired) {
		t.Fatalf("errors.Is failed for ErrSubscriptionSourcesRequired")
	}
	if ErrSubscriptionSourcesRequired.Error() == "" {
		t.Fatalf("ErrSubscriptionSourcesRequired Error empty")
	}
	if !errors.Is(ErrSubscriptionOrderingInvalid, ErrSubscriptionOrderingInvalid) {
		t.Fatalf("errors.Is failed for ErrSubscriptionOrderingInvalid")
	}
	if ErrSubscriptionOrderingInvalid.Error() == "" {
		t.Fatalf("ErrSubscriptionOrderingInvalid Error empty")
	}
	if !errors.Is(ErrVerifierRequired, ErrVerifierRequired) {
		t.Fatalf("errors.Is failed for ErrVerifierRequired")
	}
	if ErrVerifierRequired.Error() == "" {
		t.Fatalf("ErrVerifierRequired Error empty")
	}
	if !errors.Is(ErrNowRequired, ErrNowRequired) {
		t.Fatalf("errors.Is failed for ErrNowRequired")
	}
	if ErrNowRequired.Error() == "" {
		t.Fatalf("ErrNowRequired Error empty")
	}
	if !errors.Is(ErrPriorEntryWrongSubscription, ErrPriorEntryWrongSubscription) {
		t.Fatalf("errors.Is failed for ErrPriorEntryWrongSubscription")
	}
	if ErrPriorEntryWrongSubscription.Error() == "" {
		t.Fatalf("ErrPriorEntryWrongSubscription Error empty")
	}
}
