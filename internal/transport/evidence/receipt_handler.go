package evidence

import (
	"context"
	"errors"
	"strings"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GetExecutionReceipt serves one already-immutable, purpose- and
// tenant-scoped receipt by id. It never assembles or recomputes anything:
// every field returned came verbatim from the [ReceiptStore] record, which
// is itself never touched by this method (a pure read has no write path to
// fall back to even by accident).
//
// Visibility is non-disclosing: a receipt in another tenant, authorized for
// a different purpose than the one this caller declared, or owned by a
// different subject than an owner-scoped record names, is refused with the
// same NOT_FOUND this method uses for a receipt id that does not exist at
// all. A caller can never learn "it exists but I can't see it" from this
// method's response.
func (s *server) GetExecutionReceipt(ctx context.Context, req *evidencev1.GetExecutionReceiptRequest) (*evidencev1.GetExecutionReceiptResponse, error) {
	p, inv, ownedErr := trustedContext(ctx)
	if ownedErr != nil {
		return nil, ownedErr
	}
	receiptID := strings.TrimSpace(req.GetReceiptId())
	if receiptID == "" {
		return nil, requireField("receipt_id")
	}
	tenant := p.Tenant().String()
	purpose := strings.TrimSpace(req.GetScope().GetPurpose())
	if purpose == "" {
		purpose = p.DefaultPurpose()
	}
	if purpose == "" || !p.AuthorizesPurpose(purpose) {
		return nil, notFound(inv)
	}

	record, err := s.deps.Receipts.Get(ctx, tenant, receiptID)
	if err != nil {
		if errors.Is(err, ErrReceiptNotFound) {
			return nil, notFound(inv)
		}
		return nil, unavailable(inv, err)
	}
	if !receiptVisible(record, tenant, purpose, p.Subject()) {
		return nil, notFound(inv)
	}

	return &evidencev1.GetExecutionReceiptResponse{ExecutionReceipt: &evidencev1.ExecutionReceipt{
		ReceiptId: record.ReceiptID,
		Receipt:   zeroEffectReceiptProto(record),
		IssuedAt:  timestamppb.New(record.IssuedAt.UTC()),
	}}, nil
}

// receiptVisible applies GetExecutionReceipt's non-disclosing boundary:
// tenant match is mandatory, purpose match is mandatory, and an
// owner-scoped record additionally requires the exact subject.
func receiptVisible(record Receipt, tenant, purpose, subject string) bool {
	if strings.TrimSpace(record.TenantID) != tenant {
		return false
	}
	if strings.TrimSpace(record.Purpose) != purpose {
		return false
	}
	if owner := strings.TrimSpace(record.Owner); owner != "" && owner != subject {
		return false
	}
	return true
}

func zeroEffectReceiptProto(record Receipt) *evidencev1.ZeroEffectReceipt {
	mode := evidencev1.ReceiptMode_RECEIPT_MODE_UNSPECIFIED
	switch record.Mode {
	case "PREFLIGHT":
		mode = evidencev1.ReceiptMode_RECEIPT_MODE_PREFLIGHT
	case "SIMULATE":
		mode = evidencev1.ReceiptMode_RECEIPT_MODE_SIMULATE
	}
	controls := make([]*evidencev1.ControlVersion, 0, len(record.Controls))
	for _, c := range record.Controls {
		controls = append(controls, &evidencev1.ControlVersion{Name: c.Name, Version: c.Version})
	}
	return &evidencev1.ZeroEffectReceipt{
		IntentType:     record.IntentType,
		IntentVersion:  record.IntentVersion,
		Mode:           mode,
		RequestState:   record.RequestState,
		ExecutionState: record.ExecutionState,
		Controls:       controls,
		InputsDigest:   record.InputsDigest,
		ResultDigest:   record.ResultDigest,
		Counters:       &evidencev1.EffectCounters{},
	}
}
