package filing

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/transport"
	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/envelope"
)

type testProvider struct{ key []byte }

func (p *testProvider) Encrypt(ctx custody.Context, object custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil || object.Tenant != ctx.Tenant {
		return custody.Ciphertext{}, custody.Receipt{}, custody.ErrDenied
	}
	mask := sha256.Sum256(p.key)
	sealed := make([]byte, len(plaintext))
	for i := range plaintext {
		sealed[i] = plaintext[i] ^ mask[i%len(mask)]
	}
	return custody.Ciphertext{Handle: object, Algorithm: "test-wrap", Data: sealed}, custody.Receipt{ID: "wrap-receipt", Handle: object, Operation: custody.Encrypt, At: time.Unix(100, 0).UTC()}, nil
}

func (p *testProvider) Decrypt(ctx custody.Context, object custody.Handle, sealed custody.Ciphertext) ([]byte, custody.Receipt, error) {
	opened, receipt, err := p.Encrypt(ctx, object, sealed.Data)
	return opened.Data, receipt, err
}
func (p *testProvider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{}, custody.Receipt{}, errors.New("unused")
}
func (p *testProvider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("unused")
}
func (p *testProvider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}
func (p *testProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("unused")
}
func (p *testProvider) Rotate(ctx custody.Context, object custody.Handle) (custody.Handle, custody.Receipt, error) {
	return object, custody.Receipt{}, errors.New("unused")
}
func (p *testProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("unused")
}

type testTransport struct {
	request transport.Request
	calls   int
}

func (t *testTransport) Call(_ context.Context, request transport.Request) (transport.Response, error) {
	t.request = request
	t.calls++
	return transport.Response{StatusCode: 202, Body: []byte(`{"receipt":"accepted"}`)}, nil
}

func newTestService(t *testing.T) (*Service, *testTransport, *MemoryEvidenceStore, custody.Context) {
	t.Helper()
	profiles, err := NewResearchRegistry()
	if err != nil {
		t.Fatal(err)
	}
	provider := &testProvider{key: []byte("test-key-material")}
	root := custody.Handle{ID: "root", Kind: custody.Key, Version: "v1", Tenant: "root", Region: "us-east"}
	key := custody.Handle{ID: "tenant-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east"}
	manager, err := envelope.New(root, provider)
	if err != nil {
		t.Fatal(err)
	}
	cc := custody.Context{RequestContext: custody.RequestContext{Workload: "filing-test", Tenant: "tenant-a", Region: "us-east", Purpose: "workforce-filing", Destination: "state-exchange"}}
	if err := manager.RegisterTenant(cc, cc.Tenant, key); err != nil {
		t.Fatal(err)
	}
	sender := &testTransport{}
	store := NewMemoryEvidenceStore()
	service, err := NewService(profiles, manager, sender, store, WithClock(func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }))
	if err != nil {
		t.Fatal(err)
	}
	return service, sender, store, cc
}

func wageRequest(id string, cc custody.Context) FilingRequest {
	return FilingRequest{
		SubmissionID: id, Kind: StateWage, Jurisdiction: legal.Jurisdiction{Country: "US", State: "CA"}, AsOf: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		Payload:        []byte(`{"employer_id":"employer-redacted","employee_id":"employee-redacted","reporting_period":"2026-Q3","wages":"1000.00"}`),
		CustodyContext: cc, ObjectID: "payload-" + id,
		Elements: map[string]string{"employer_id": "present", "employee_id": "present", "reporting_period": "2026-Q3", "wages": "1000.00"},
	}
}

// TestTodo_SECARCH_023 proves the complete versioned, validated, encrypted,
// acknowledged, and digested filing contract.
func TestTodo_SECARCH_023(t *testing.T) {
	service, sender, store, cc := newTestService(t)
	request := wageRequest("submission-1", cc)
	submission, err := service.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || len(sender.request.Body) == 0 || strings.Contains(string(sender.request.Body), "employer-redacted") {
		t.Fatalf("transport received unsafe or missing wire body: calls=%d body=%q", sender.calls, sender.request.Body)
	}
	if submission.Profile.SchemaVersion != "CA-UI-WAGE-2026.1" || len(submission.Evidence.Records) != 3 {
		t.Fatalf("submission profile/evidence = %+v", submission)
	}
	if err := service.Verify(request.SubmissionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(request.SubmissionID); err != nil {
		t.Fatal(err)
	}
	explanation, err := submission.Explain()
	if err != nil || explanation.PayloadDigest == "" || explanation.EvidenceRecordCount != 3 {
		t.Fatalf("explanation = %+v, %v", explanation, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", explanation), request.SubmissionID) || strings.Contains(fmt.Sprintf("%+v", explanation), "employer-redacted") {
		t.Fatalf("explanation exposed an identifier or secret: %+v", explanation)
	}
}

func TestTodo_SECARCH_023_Golden(t *testing.T) {
	registry, err := NewResearchRegistry()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	want := map[string]map[ExchangeKind]string{
		"CA": {StateWage: "CA-UI-WAGE-2026.1", NewHire: "CA-NEW-HIRE-2026.1", Withholding: "CA-WITHHOLDING-2026.1"},
		"NY": {StateWage: "NY-UI-WAGE-2026.1", NewHire: "NY-NEW-HIRE-2026.1", Withholding: "NY-WITHHOLDING-2026.1"},
		"WA": {StateWage: "WA-UI-WAGE-2026.1", NewHire: "WA-NEW-HIRE-2026.1", Withholding: "WA-WITHHOLDING-2026.1"},
	}
	for state, kinds := range want {
		for kind, version := range kinds {
			profile, resolveErr := registry.Resolve(kind, legal.Jurisdiction{Country: "US", State: state}, at)
			if resolveErr != nil || profile.SchemaVersion != version || profile.Digest() == "" {
				t.Errorf("%s/%s = version=%q err=%v", state, kind, profile.SchemaVersion, resolveErr)
			}
		}
	}
}

func TestTodo_SECARCH_023_Security(t *testing.T) {
	service, sender, _, cc := newTestService(t)
	request := wageRequest("missing-required", cc)
	delete(request.Elements, "employee_id")
	_, err := service.Submit(context.Background(), request)
	var refusal Refusal
	if !errors.As(err, &refusal) || refusal.Element != "employee_id" || !errors.Is(err, ErrMissingElement) {
		t.Fatalf("error = %v, want typed employee_id refusal", err)
	}
	if sender.calls != 0 {
		t.Fatal("missing required element was transmitted")
	}
}

func TestTodo_SECARCH_023_Integration(t *testing.T) {
	service, sender, _, cc := newTestService(t)
	submission, err := service.Submit(context.Background(), wageRequest("integration-1", cc))
	if err != nil {
		t.Fatal(err)
	}
	if sender.request.Headers["Content-Type"] != "application/json" || sender.request.Headers["X-Filing-Schema-Version"] != submission.Profile.SchemaVersion {
		t.Fatalf("wire headers = %#v", sender.request.Headers)
	}
	if err := submission.Evidence.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_SECARCH_023_Mutation(t *testing.T) {
	service, _, store, cc := newTestService(t)
	first, err := service.Submit(context.Background(), wageRequest("original", cc))
	if err != nil {
		t.Fatal(err)
	}
	correction := wageRequest("correction", cc)
	correction.CorrectionOf = first.ID
	second, err := service.Submit(context.Background(), correction)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Evidence.Records) != 4 || second.Evidence.Records[3].Kind != CorrectionEvidence || second.Evidence.Records[0].CorrectionOfDigest == "" {
		t.Fatalf("correction chain = %+v", second.Evidence.Records)
	}
	if err := store.Verify(second.ID); err != nil {
		t.Fatal(err)
	}
	mutated := second.Evidence.clone()
	mutated.Records[1].Status = "FORGED"
	if err := mutated.Verify(); !errors.Is(err, ErrBrokenChain) {
		t.Fatalf("mutated chain error = %v, want ErrBrokenChain", err)
	}
}
