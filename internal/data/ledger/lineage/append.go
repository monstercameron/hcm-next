package lineage

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
)

// Append validates a CORRECTION request's target - refusing a dangling or
// cross-tenant reference before anything is written - and then forwards to
// internal/data/ledger.Append unchanged. Every other assertion class passes
// through untouched: lineage only has an opinion about corrections.
//
// internal/data/ledger.Append (LEDGER-004) already refuses a CORRECTION
// request with no target at all, or a non-CORRECTION request that carries
// one; Append here adds the one check that requires a database round trip -
// that the named target actually exists for this tenant - which the
// in-memory validation in append.go cannot perform on its own.
//
// Append writes nothing itself and exposes no update or delete: the only
// effect of a successful call is the single INSERT
// internal/data/ledger.Append performs.
func Append(ctx context.Context, tx pgx.Tx, tenant uuid.UUID, req datalogger.AppendRequest) (datalogger.AppendReceipt, error) {
	if req.AssertionClass == datalogger.Correction && req.Corrects != nil {
		if _, err := ValidateCorrectionTarget(ctx, tx, tenant, *req.Corrects); err != nil {
			return datalogger.AppendReceipt{}, err
		}
	}
	return datalogger.Append(ctx, tx, req)
}
