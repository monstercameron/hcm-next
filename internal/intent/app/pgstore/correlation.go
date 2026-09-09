package pgstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
)

// LoadIntentByCorrelation resolves the intent a workflow instance was started
// for from the instance's correlation id.
//
// The source is the intent's own ledger stream: [Store.Append] records every
// intent's creation as a ledger_event on stream "intent:<id>" carrying
// correlationUUID(correlation), and ledger_event_correlation indexes exactly
// that pair. It is the one durable, append-only place the correlation is
// written today (intent_instance_context exists in the schema but nothing in
// this cell records it yet), so a timer-parked instance with no work item
// still maps back to the intent that started it. A correlation that names
// several intents resolves to the earliest recorded one; the journey creates
// one intent per request, so that is the only one in practice.
func (s *Store) LoadIntentByCorrelation(ctx context.Context, tenant, correlation string) (app.IntentRecord, error) {
	if correlation == "" {
		return app.IntentRecord{}, fmt.Errorf("pgstore: %w: empty correlation", app.ErrIntentNotFound)
	}
	tenantID := TenantID(tenant)
	var streamKey string
	err := s.db.QueryRow(ctx, `
		SELECT stream_key
		FROM ledger_event
		WHERE tenant_id = $1 AND correlation_id = $2 AND stream_key LIKE 'intent:%'
		ORDER BY recorded_at, stream_key
		LIMIT 1`, tenantID, correlationUUID(correlation)).Scan(&streamKey)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return app.IntentRecord{}, fmt.Errorf("pgstore: %w: no intent recorded under correlation %q", app.ErrIntentNotFound, correlation)
		}
		return app.IntentRecord{}, fmt.Errorf("pgstore: resolve correlation %q: %w", correlation, err)
	}
	intentID := strings.TrimPrefix(streamKey, "intent:")
	rec, err := s.LoadIntent(ctx, tenant, intentID)
	if err != nil {
		return app.IntentRecord{}, err
	}
	rec.CorrelationID = correlation
	return rec, nil
}
