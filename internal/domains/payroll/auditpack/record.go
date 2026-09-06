package auditpack

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/hashchain"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// LineRequest is what a caller states to record one contributing line. The
// ledger, not this package, allocates the sequence and the digest; a caller
// that could supply either could forge a reconciliation input.
type LineRequest struct {
	Tenant         TenantID
	RunID          string
	Kind           TotalKind
	Amount         values.Decimal
	Currency       string
	ExpectedHead   int64
	OccurredAt     time.Time
	EffectiveAt    time.Time
	CorrelationID  CorrelationID
	IdempotencyKey string
}

// AppendLine records one contributing line as a TRANSACTION_FACT ledger
// event on the run's stream ([StreamKey]) and hash-chains it in the same
// transaction, so it is coverable by a checkpoint the moment the transaction
// commits.
//
// It is a convenience, not the only way to produce a line
// [ResolveFromContent] reads: any caller that appends a payload
// [EncodeLine] produces, under [LineSchemaRef] on the run's stream, resolves
// identically.
func AppendLine(ctx context.Context, tx dbport.Tx, appender *datalogger.Appender, chainer *hashchain.Appender, req LineRequest) (datalogger.AppendReceipt, error) {
	if appender == nil {
		return datalogger.AppendReceipt{}, fmt.Errorf("auditpack: an appender is required")
	}
	if chainer == nil {
		return datalogger.AppendReceipt{}, fmt.Errorf("auditpack: a hash-chain appender is required")
	}
	payload, err := EncodeLine(req.RunID, req.Kind, req.Amount, req.Currency)
	if err != nil {
		return datalogger.AppendReceipt{}, err
	}
	receipt, err := appender.Append(ctx, tx, datalogger.AppendRequest{
		Tenant: req.Tenant, StreamKey: StreamKey(req.RunID), ExpectedHead: req.ExpectedHead,
		AssertionClass: datalogger.TransactionFact,
		SourceRef:      SourceRef, SchemaRef: LineSchemaRef,
		Payload:        payload,
		OccurredAt:     req.OccurredAt,
		EffectiveAt:    req.EffectiveAt,
		CorrelationID:  req.CorrelationID,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return datalogger.AppendReceipt{}, err
	}
	if receipt.Replayed {
		return receipt, nil
	}
	if _, err := chainer.Append(ctx, tx, receipt); err != nil {
		return datalogger.AppendReceipt{}, err
	}
	return receipt, nil
}

// BindRequest is what a caller states to bind a resolved reconciliation.
type BindRequest struct {
	Tenant        TenantID
	RunID         string
	ExpectedHead  int64
	OccurredAt    time.Time
	EffectiveAt   time.Time
	CorrelationID CorrelationID
}

// bindingPayload is the wire projection [Bind] records and [decodeBinding]
// reads back: the resolved totals and the variance decision the ledger
// itself now attests to, keyed by the run's own immutable idempotency key.
type bindingPayload struct {
	RunID          string            `json:"run_id"`
	IdempotencyKey string            `json:"idempotency_key"`
	Totals         map[string]string `json:"totals"`
	Pairs          []pairPayload     `json:"pairs"`
}

type pairPayload struct {
	Name        string `json:"name"`
	LeftAmount  string `json:"left_amount"`
	RightAmount string `json:"right_amount"`
	Difference  string `json:"difference"`
	OK          bool   `json:"ok"`
}

func encodeBinding(runID, idempotencyKey string, totals RunTotals, decision Decision) ([]byte, error) {
	wire := bindingPayload{
		RunID: runID, IdempotencyKey: idempotencyKey,
		Totals: make(map[string]string, len(totals.Totals)),
	}
	for _, k := range Kinds() {
		amount, ok := totals.Total(k)
		if !ok {
			return nil, ErrMissingTotal{RunID: runID, Kind: k}
		}
		text, err := amount.MarshalText()
		if err != nil {
			return nil, fmt.Errorf("auditpack: encode total %s: %w", k, err)
		}
		wire.Totals[string(k)] = string(text)
	}
	for _, p := range decision.Pairs {
		left, err := p.LeftAmount.MarshalText()
		if err != nil {
			return nil, err
		}
		right, err := p.RightAmount.MarshalText()
		if err != nil {
			return nil, err
		}
		diff, err := p.Difference.MarshalText()
		if err != nil {
			return nil, err
		}
		wire.Pairs = append(wire.Pairs, pairPayload{
			Name: p.Name, LeftAmount: string(left), RightAmount: string(right),
			Difference: string(diff), OK: p.OK,
		})
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("auditpack: encode binding: %w", err)
	}
	return body, nil
}

// decodeBinding is encodeBinding's inverse.
func decodeBinding(payload []byte) (bindingPayload, error) {
	var wire bindingPayload
	if err := json.Unmarshal(payload, &wire); err != nil {
		return bindingPayload{}, fmt.Errorf("auditpack: payload is not a reconciliation binding: %w", err)
	}
	return wire, nil
}

// Bind resolves the run's immutable idempotency key ([IdempotencyKey]) and
// records the given totals and variance decision as one TRANSACTION_FACT
// ledger event on the run's stream, hash-chained in the same transaction.
//
// It refuses to record anything when decision carries an unexplained
// variance ([Decision.Err]): a reconciliation that does not reconcile is not
// evidence of release-readiness, so nothing is written. Calling Bind twice
// for the same run with the same totals is an exact replay under
// [IdempotencyKey] (internal/data/ledger's own rule); calling it twice with
// different totals is refused as an idempotency conflict, never a silent
// overwrite of the first attestation.
func Bind(ctx context.Context, tx dbport.Tx, appender *datalogger.Appender, chainer *hashchain.Appender, req BindRequest, totals RunTotals, decision Decision) (datalogger.AppendReceipt, error) {
	if totals.Tenant != req.Tenant || totals.RunID != req.RunID {
		return datalogger.AppendReceipt{}, fmt.Errorf("auditpack: bind request for run %s/%s does not match resolved totals for run %s/%s",
			req.Tenant, req.RunID, totals.Tenant, totals.RunID)
	}
	if err := decision.Err(); err != nil {
		return datalogger.AppendReceipt{}, err
	}
	key := IdempotencyKey(req.Tenant, req.RunID)
	payload, err := encodeBinding(req.RunID, key, totals, decision)
	if err != nil {
		return datalogger.AppendReceipt{}, err
	}
	receipt, err := appender.Append(ctx, tx, datalogger.AppendRequest{
		Tenant: req.Tenant, StreamKey: StreamKey(req.RunID), ExpectedHead: req.ExpectedHead,
		AssertionClass: datalogger.TransactionFact,
		SourceRef:      SourceRef, SchemaRef: BindingSchemaRef,
		Payload:        payload,
		OccurredAt:     req.OccurredAt,
		EffectiveAt:    req.EffectiveAt,
		CorrelationID:  req.CorrelationID,
		IdempotencyKey: key,
	})
	if err != nil {
		return datalogger.AppendReceipt{}, err
	}
	if receipt.Replayed {
		return receipt, nil
	}
	if _, err := chainer.Append(ctx, tx, receipt); err != nil {
		return datalogger.AppendReceipt{}, err
	}
	return receipt, nil
}
