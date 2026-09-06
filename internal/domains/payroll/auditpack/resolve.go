package auditpack

import (
	"encoding/json"
	"fmt"
	"sort"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// linePayload is the wire projection of one contributing line's ledger
// payload. Amount crosses as [values.Decimal.MarshalText]'s own format ("text
// /ROUNDING_MODE"), so the declared scale and rounding mode travel with the
// number instead of being reconstructed by a reader's guess.
type linePayload struct {
	RunID    string `json:"run_id"`
	Kind     string `json:"kind"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// EncodeLine renders one contributing fact as the bytes [AppendLine] and
// [ResolveFromContent] agree on. It is exported so a caller that appends a
// line through its own transaction, rather than through [AppendLine], can
// still produce bytes this package resolves correctly.
func EncodeLine(runID string, kind TotalKind, amount values.Decimal, currency string) ([]byte, error) {
	fact := LineFact{Kind: kind, RunID: runID, Amount: amount, Currency: currency}
	if err := fact.Validate(); err != nil {
		return nil, err
	}
	text, err := amount.MarshalText()
	if err != nil {
		return nil, fmt.Errorf("auditpack: encode line amount: %w", err)
	}
	body, err := json.Marshal(linePayload{RunID: runID, Kind: string(kind), Amount: string(text), Currency: currency})
	if err != nil {
		return nil, fmt.Errorf("auditpack: encode line: %w", err)
	}
	return body, nil
}

// decodeLine is EncodeLine's inverse, tolerant of a payload that does not
// parse: a malformed payload under this schema is reported by the caller
// (resolution refuses; verification names a finding), never panicked on.
func decodeLine(payload []byte) (runID string, kind TotalKind, amount values.Decimal, currency string, err error) {
	var wire linePayload
	if unmarshalErr := json.Unmarshal(payload, &wire); unmarshalErr != nil {
		return "", "", values.Decimal{}, "", fmt.Errorf("%w: payload is not a line: %v", ErrInvalidLine, unmarshalErr)
	}
	var d values.Decimal
	if unmarshalErr := d.UnmarshalText([]byte(wire.Amount)); unmarshalErr != nil {
		return "", "", values.Decimal{}, "", fmt.Errorf("%w: amount: %v", ErrInvalidLine, unmarshalErr)
	}
	return wire.RunID, TotalKind(wire.Kind), d, wire.Currency, nil
}

// ResolveFromContent folds every contributing line for runID inside content
// into the four declared totals. content is ordinarily
// evidence.Exporter.Read's own return value, so every amount this function
// sums is already a ledger checkpoint/evidence row, never a domain
// aggregate's own arithmetic.
//
// It refuses rather than guesses: a run missing a contributing line for any
// of the four kinds is [ErrMissingTotal], and two lines for the same kind
// declaring different decimal scales is [ErrScaleMismatch].
func ResolveFromContent(content evidence.Content, tenant TenantID, runID string) (RunTotals, error) {
	if content.Tenant != tenant {
		return RunTotals{}, ErrTenantLeak{Expected: tenant, Found: content.Tenant}
	}

	out := RunTotals{
		Tenant: tenant, RunID: runID,
		CoversFrom: content.CoversFrom, CoversTo: content.CoversTo,
		Totals: make(map[TotalKind]values.Decimal, len(Kinds())),
	}

	for _, stream := range content.Streams {
		for _, event := range stream.Events {
			if event.SchemaRef != LineSchemaRef {
				continue
			}
			if event.Tenant != tenant {
				return RunTotals{}, ErrTenantLeak{Expected: tenant, Found: event.Tenant}
			}
			lineRun, kind, amount, currency, err := decodeLine(event.Payload)
			if err != nil {
				return RunTotals{}, fmt.Errorf("auditpack: line %s@%d: %w", event.StreamKey, event.Sequence, err)
			}
			if lineRun != runID {
				continue
			}
			if !kind.Valid() {
				return RunTotals{}, fmt.Errorf("%w: line %s@%d names kind %q", ErrInvalidLine, event.StreamKey, event.Sequence, kind)
			}
			fact := LineFact{
				Kind: kind, RunID: runID, Amount: amount, Currency: currency,
				Ref: datalogger.EventRef{StreamKey: event.StreamKey, Sequence: event.Sequence},
			}
			if err := fact.Validate(); err != nil {
				return RunTotals{}, err
			}
			out.Lines = append(out.Lines, fact)

			running, ok := out.Totals[kind]
			if !ok {
				out.Totals[kind] = amount
				continue
			}
			if running.Scale() != amount.Scale() {
				return RunTotals{}, ErrScaleMismatch{Kind: kind, Want: running.Scale(), Got: amount.Scale()}
			}
			sum, err := running.Add(amount)
			if err != nil {
				return RunTotals{}, fmt.Errorf("auditpack: sum %s lines: %w", kind, err)
			}
			out.Totals[kind] = sum
		}
	}

	sort.Slice(out.Lines, func(i, j int) bool {
		if out.Lines[i].Kind != out.Lines[j].Kind {
			return out.Lines[i].Kind < out.Lines[j].Kind
		}
		if out.Lines[i].Ref.StreamKey != out.Lines[j].Ref.StreamKey {
			return out.Lines[i].Ref.StreamKey < out.Lines[j].Ref.StreamKey
		}
		return out.Lines[i].Ref.Sequence < out.Lines[j].Ref.Sequence
	})

	for _, k := range Kinds() {
		if _, ok := out.Totals[k]; !ok {
			return RunTotals{}, ErrMissingTotal{RunID: runID, Kind: k}
		}
	}
	return out, nil
}
