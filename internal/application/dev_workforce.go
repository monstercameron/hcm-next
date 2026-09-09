package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/demoworkforce"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// bootstrapLocalDevWorkforce makes the local browser identities genuine
// members of the same durable tenant population the Journey service reads.
// Both seeders are replay-safe, and one transaction prevents a login page
// from advertising an account before its worker and organization exist.
func bootstrapLocalDevWorkforce(ctx context.Context, pool *pgxadapter.Pool, tenant string) (demoworkforce.Summary, demoworkforce.OrganizationSummary, error) {
	if pool == nil || tenant != demoworkforce.CompanyKey {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("begin local development workforce seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantID := pgstore.TenantID(tenant)
	organization, err := demoworkforce.SeedOrganization(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed local development organization: %w", err)
	}
	workforce, err := demoworkforce.Seed(ctx, tx, tenantID)
	if err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("seed local development workforce: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return demoworkforce.Summary{}, demoworkforce.OrganizationSummary{}, fmt.Errorf("commit local development workforce seed: %w", err)
	}
	return workforce, organization, nil
}
