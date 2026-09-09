package attestationstore

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustattest "github.com/monstercameron/human-capital-management-suite/internal/trust/attest"
)

type responseRow struct{ values []any }

func (r responseRow) Scan(dest ...any) error {
	if len(dest) != len(r.values) {
		return errors.New("unexpected scan shape")
	}
	for i, value := range r.values {
		switch target := dest[i].(type) {
		case *uuid.UUID:
			*target = value.(uuid.UUID)
		case *string:
			if value == nil {
				*target = ""
			} else {
				*target = value.(string)
			}
		case **string:
			*target = value.(*string)
		case *int64:
			*target = value.(int64)
		case *[]byte:
			*target = value.([]byte)
		case *time.Time:
			*target = value.(time.Time)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func TestAttestationStore_ScanResponseValidatesTenantJSONAndDigest(t *testing.T) {
	tenantID := uuid.New()
	tenant := values.TenantId(tenantID.String())
	tm := trustattest.TrustedTime{At: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Source: "clock", EvidenceID: "ev-1", Health: "HEALTHY"}
	base := trustattest.Response{
		Tenant: tenant, ResponseID: "response-1", Revision: 1, StatementID: "statement-1", StatementVersion: 1,
		StatementDigest: "sha256:" + strings.Repeat("1", 64), BindingDigest: "sha256:" + strings.Repeat("2", 64),
		Status: trustattest.ResponseAccepted, Kind: trustattest.AssertionResponse, EvidenceReceipt: "sha256:" + strings.Repeat("3", 64),
		IdempotencyKey: "idem-1", RecordedAt: tm, RequestDigest: strings.Repeat("4", 64),
	}
	base.Digest = trustattest.DigestResponse(base)
	timeEvidence, err := json.Marshal(tm)
	if err != nil {
		t.Fatal(err)
	}
	valuesFor := func(storedTenant uuid.UUID, digest string, evidence []byte, obligations []byte) responseRow {
		return responseRow{values: []any{storedTenant, base.ResponseID, int64(base.Revision), base.StatementID, int64(base.StatementVersion), strings.TrimPrefix(base.StatementDigest, "sha256:"), strings.TrimPrefix(base.BindingDigest, "sha256:"), string(base.Status), string(base.Kind), base.Reason, strings.TrimPrefix(base.EvidenceReceipt, "sha256:"), base.IdempotencyKey, (*string)(nil), (*string)(nil), obligations, (*string)(nil), tm.At, evidence, digest, base.RequestDigest}}
	}
	var got trustattest.Response
	if err := scanResponse(valuesFor(tenantID, strings.TrimPrefix(base.Digest, "sha256:"), timeEvidence, []byte("[]")), tenant, &got); err != nil {
		t.Fatalf("scan valid response: %v", err)
	}
	if got.Digest != base.Digest || got.Tenant != tenant || got.RecordedAt.EvidenceID != tm.EvidenceID {
		t.Fatalf("rehydrated response = %+v", got)
	}
	if err := scanResponse(valuesFor(uuid.New(), strings.TrimPrefix(base.Digest, "sha256:"), timeEvidence, []byte("[]")), tenant, &got); err == nil {
		t.Fatal("tenant mismatch was accepted")
	}
	if err := scanResponse(valuesFor(tenantID, strings.TrimPrefix(base.Digest, "sha256:"), []byte("{"), []byte("[]")), tenant, &got); err == nil {
		t.Fatal("malformed trusted-time JSON was accepted")
	}
	if err := scanResponse(valuesFor(tenantID, strings.Repeat("9", 64), timeEvidence, []byte("[]")), tenant, &got); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
	if nullableString("") != nil || nullableString("value") != "value" {
		t.Fatal("nullableString did not preserve optional column semantics")
	}
}
