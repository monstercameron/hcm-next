package paymethod

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/contact"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/sod"
)

type paymethodContactSource struct {
	endpoint contact.ContactEndpointRevision
}

func (s paymethodContactSource) ResolveIndependentContact(string, string, string) (contact.ContactEndpointRevision, error) {
	return s.endpoint, nil
}

type paymethodConfirmationDispatcher struct{ messages []ChangeConfirmation }

func (d *paymethodConfirmationDispatcher) DispatchOutOfBand(message ChangeConfirmation) error {
	d.messages = append(d.messages, message)
	return nil
}

func paymethodContact(t *testing.T) contact.ContactEndpointRevision {
	t.Helper()
	endpoint, err := contact.NewContactEndpointRevision(values.EntityRef{Tenant: values.TenantId("tenant-1"), Kind: values.Kind("worker"), Id: "00000000-0000-0000-0000-000000000001"}, "contact-1", contact.EndpointEmail, "payroll-alerts@example.test", "payment_destination_change_confirmation", 1, "hr-contact-profile")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := endpoint.MarkVerified()
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func paymethodChangeRequest(t *testing.T) BankDetailChangeRequest {
	t.Helper()
	return BankDetailChangeRequest{
		ID: "change-1", TenantID: "tenant-1", DestinationID: "destination-1", WorkerRef: "worker-1",
		BeforeDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		AfterDigest:  "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		RequestedBy:  "requester", Approver: "approver", RequestedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), CoolingOff: 24 * time.Hour,
		Constraints:     sod.Constraints{RuleID: "payment-destination-dual-control", RequesterMayNotApprove: true, OneApprovalPerPrincipal: true},
		DecisionContext: sod.DecisionContext{Requester: sod.Actor{Subject: "requester"}, Approvers: []sod.Actor{{Subject: "approver"}}},
	}
}

func TestTodo_SECARCH_013(t *testing.T) {
	dispatcher := &paymethodConfirmationDispatcher{}
	change, err := StartBankDetailChange(paymethodChangeRequest(t), paymethodContactSource{endpoint: paymethodContact(t)}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if change.Status != ChangeAwaitingConfirmation || len(dispatcher.messages) != 1 {
		t.Fatalf("change=%+v messages=%d", change, len(dispatcher.messages))
	}
	if dispatcher.messages[0].BeforeDigest != change.BeforeDigest || dispatcher.messages[0].AfterDigest != change.AfterDigest {
		t.Fatal("confirmation did not carry the protected before/after digests")
	}
	if err := change.CanReceivePayment(change.RequestedAt.Add(48 * time.Hour)); !errors.Is(err, ErrChangeNotAvailable) {
		t.Fatalf("unconfirmed change release error = %v", err)
	}
	confirmed, err := change.Confirm(change.RequestedAt.Add(time.Hour), "sha256:3333333333333333333333333333333333333333333333333333333333333333")
	if err != nil {
		t.Fatal(err)
	}
	if err := confirmed.CanReceivePayment(confirmed.AvailableAt.Add(time.Nanosecond)); err != nil {
		t.Fatalf("confirmed change should be available after cooling-off: %v", err)
	}
}

func TestTodo_SECARCH_013_Golden(t *testing.T) {
	change, err := StartBankDetailChange(paymethodChangeRequest(t), paymethodContactSource{endpoint: paymethodContact(t)}, &paymethodConfirmationDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(change.CanonicalDigest, "sha256:") || len(change.CanonicalDigest) != len("sha256:")+64 {
		t.Fatalf("canonical digest = %q", change.CanonicalDigest)
	}
}

func TestTodo_SECARCH_013_Security(t *testing.T) {
	change, err := StartBankDetailChange(paymethodChangeRequest(t), paymethodContactSource{endpoint: paymethodContact(t)}, &paymethodConfirmationDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	explanation := change.Explain()
	for _, raw := range []string{"123456789012", "routing-number", "worker-1", "destination-1", "requester", "approver"} {
		if strings.Contains(explanation, raw) {
			t.Fatalf("unsafe explanation contains %q: %s", raw, explanation)
		}
	}
	bad := paymethodChangeRequest(t)
	bad.Approver = "requester"
	_, err = StartBankDetailChange(bad, paymethodContactSource{endpoint: paymethodContact(t)}, &paymethodConfirmationDispatcher{})
	if !errors.Is(err, ErrDistinctApproverRequired) {
		t.Fatalf("same-principal approval error = %v", err)
	}
}

func TestTodo_SECARCH_013_Integration(t *testing.T) {
	// The real contact constructor and real sod evaluator are wired here; only
	// contact lookup and out-of-band delivery are I/O ports.
	request := paymethodChangeRequest(t)
	dispatcher := &paymethodConfirmationDispatcher{}
	change, err := StartBankDetailChange(request, paymethodContactSource{endpoint: paymethodContact(t)}, dispatcher)
	if err != nil || change.ContactRevisionDigest == "" || len(dispatcher.messages) != 1 {
		t.Fatalf("change=%+v messages=%d err=%v", change, len(dispatcher.messages), err)
	}
}

func TestTodo_SECARCH_013_Mutation(t *testing.T) {
	change, err := StartBankDetailChange(paymethodChangeRequest(t), paymethodContactSource{endpoint: paymethodContact(t)}, &paymethodConfirmationDispatcher{})
	if err != nil {
		t.Fatal(err)
	}
	originalDigest := change.CanonicalDigest
	confirmed, err := change.Confirm(change.RequestedAt.Add(time.Hour), "sha256:3333333333333333333333333333333333333333333333333333333333333333")
	if err != nil || change.CanonicalDigest != originalDigest || change.Status != ChangeAwaitingConfirmation || confirmed.Revision != change.Revision+1 {
		t.Fatalf("change mutation: original=%+v confirmed=%+v err=%v", change, confirmed, err)
	}
}

func TestTodo_SECARCH_014(t *testing.T) {
	destination := validDestination(t, "ach-destination")
	account, err := NewProtectedAccount(StorageTokenized, "token:ach-destination", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	validation, err := NewAccountValidationRecord("INSTANT_VERIFICATION", time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), ValidationPassed, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	binding, err := NewReleaseBinding(destination, account, validation, nil)
	if err != nil || binding.CanRelease() != nil {
		t.Fatalf("valid release binding=%+v err=%v release=%v", binding, err, binding.CanRelease())
	}
	pending, _ := NewAccountValidationRecord("MICRO_DEPOSIT", validation.ValidatedAt, ValidationPending, validation.EvidenceDigest)
	blocked, err := NewReleaseBinding(destination, account, pending, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(blocked.CanRelease(), ErrValidationRequired) {
		t.Fatal("pending validation was not blocked")
	}
}

func TestTodo_SECARCH_014_Golden(t *testing.T) {
	validation, err := NewAccountValidationRecord("MICRO_DEPOSIT", time.Unix(100, 0).UTC(), ValidationPassed, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(validation.CanonicalDigest, "sha256:") {
		t.Fatal("validation record is not digested")
	}
}

func TestTodo_SECARCH_014_Security(t *testing.T) {
	if _, err := NewProtectedAccount(StorageTokenized, "123456789012", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, ErrRawBankDetailProhibited) {
		t.Fatalf("raw account reference error = %v", err)
	}
	inspector, err := NewAccountNumberInspector()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectEgress(inspector, []byte(`{"account_number":"123456789012"}`)); !errors.Is(err, ErrAccountNumberEgress) {
		t.Fatalf("DLP egress error = %v", err)
	}
	if _, err := InspectEgress(inspector, []byte(`{"account_token":"token:ach-destination"}`)); err != nil {
		t.Fatalf("tokenized egress was refused: %v", err)
	}
}

func TestTodo_SECARCH_014_Integration(t *testing.T) {
	destination := validDestination(t, "ach-destination")
	account, err := NewProtectedAccount(StorageEnvelopeEncrypted, "envelope:tenant-1:ach-destination:v1", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	validation, err := NewAccountValidationRecord("ACCOUNT_EXISTENCE", time.Unix(100, 0).UTC(), ValidationPending, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	exception := &ValidationException{ApprovedBy: "risk-officer", ApprovedAt: time.Unix(101, 0).UTC(), ReasonDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	binding, err := NewReleaseBinding(destination, account, validation, exception)
	if err != nil || binding.CanRelease() != nil {
		t.Fatalf("approved exception binding=%+v err=%v release=%v", binding, err, binding.CanRelease())
	}
}

func TestTodo_SECARCH_014_Mutation(t *testing.T) {
	record, err := NewAccountValidationRecord("MICRO_DEPOSIT", time.Unix(100, 0).UTC(), ValidationPassed, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	before := record.CanonicalDigest
	record.Result = ValidationFailed
	if err := record.Validate(); err == nil || record.CanonicalDigest != before {
		t.Fatalf("validation record was mutable without a new digest: %+v err=%v", record, err)
	}
}
