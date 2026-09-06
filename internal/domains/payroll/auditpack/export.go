package auditpack

import (
	"context"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/ledger/evidence"
)

// Querier is the minimal database capability an export needs. It is
// evidence.Querier itself (internal/data/ledger.Querier), so whatever handle
// a caller already reads the ledger through satisfies this one unchanged.
type Querier = evidence.Querier

// ExportRequest is what a caller states about an auditor package export.
// Everything the package attests to - the resolved totals, the variance
// decision, every digest - is derived from what the ledger and its signed
// checkpoints already hold for [From, To), never accepted from the caller.
type ExportRequest struct {
	Tenant TenantID
	RunID  string
	// From and To are the half-open recorded-time window to export. It must
	// cover every contributing line the run recorded and be wholly covered by
	// a signed checkpoint epoch, exactly as evidence.Request requires.
	From time.Time
	To   time.Time
	// Schema binds the package to the physical schema the ledger was read
	// under, exactly as evidence.Request requires.
	Schema evidence.SchemaRelease
}

// Exporter assembles auditor packages from a live ledger. It holds no state;
// the value exists so the method set can be swapped at a composition root.
type Exporter struct {
	inner *evidence.Exporter
}

// NewExporter returns an Exporter.
func NewExporter() *Exporter { return &Exporter{inner: evidence.NewExporter()} }

// Export reads the covered slice of the ledger, resolves the run's four
// totals and its variance decision purely from that slice (never from a
// domain aggregate), and builds the auditor package.
//
// It is read-only: no statement it runs writes anything. It refuses rather
// than degrades, at three points in order: the wrapped evidence read refuses
// a window no signed epoch covers or a row naming another tenant
// ([evidence.ErrIncompleteEpochCoverage], [evidence.ErrTenantLeak]); a run
// missing a contributing line for one of the four kinds refuses resolution
// ([ErrMissingTotal]); and a run whose totals do not reconcile exactly
// refuses to become a package at all ([ErrVarianceUnexplained]) - an auditor
// package attests that a run reconciled, and one built over a run that did
// not would misrepresent exactly that.
func (e *Exporter) Export(ctx context.Context, q Querier, req ExportRequest) (Package, RunTotals, Decision, error) {
	content, totals, decision, err := e.Resolve(ctx, q, req)
	if err != nil {
		return Package{}, totals, decision, err
	}
	pkg, err := Build(content, totals, decision)
	if err != nil {
		return Package{}, totals, decision, err
	}
	return pkg, totals, decision, nil
}

// Resolve reads the covered slice of the ledger and resolves the run's
// totals and variance decision without building package bytes. A caller
// that wants to inspect what a release would rest on - the totals, whether
// they reconcile - before deciding whether to export a package at all calls
// this and never touches [Build].
func (e *Exporter) Resolve(ctx context.Context, q Querier, req ExportRequest) (evidence.Content, RunTotals, Decision, error) {
	content, err := e.inner.Read(ctx, q, evidence.Request{
		Tenant: req.Tenant, From: req.From, To: req.To, Schema: req.Schema,
	})
	if err != nil {
		return evidence.Content{}, RunTotals{}, Decision{}, err
	}
	totals, err := ResolveFromContent(content, req.Tenant, req.RunID)
	if err != nil {
		return content, RunTotals{}, Decision{}, err
	}
	decision, err := Reconcile(totals)
	if err != nil {
		return content, totals, Decision{}, err
	}
	return content, totals, decision, nil
}
