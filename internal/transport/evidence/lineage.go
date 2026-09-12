package evidence

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/evidence"
)

// assembleReceipt seals a BusinessExecutionReceipt from exactly the frozen
// facts snap carries -- it touches nothing else, so the receipt can only
// ever describe what was true at the instant snap was recorded.
// evidence.Assemble itself refuses a snapshot that omits any of the fifteen
// declared dimensions, so a missing dimension fails here, not later.
func assembleReceipt(snap LineageSnapshot) (evidence.BusinessExecutionReceipt, error) {
	inputs := make([]evidence.DimensionInput, 0, len(snap.Dimensions))
	for _, d := range snap.Dimensions {
		inputs = append(inputs, evidence.DimensionInput{Name: d.Name, Status: d.Status, Digest: d.Digest, Note: d.Note})
	}
	receipt, err := evidence.Assemble(snap.Tenant, snap.IntentRef, snap.LineageDigest, snap.AuthorityLineage, inputs)
	if err != nil {
		return evidence.BusinessExecutionReceipt{}, fmt.Errorf("evidence: assemble receipt: %w", err)
	}
	return receipt, nil
}

// requireLineageHops proves, by name, that every one of RequiredLineageHops
// is PRESENT in receipt -- not merely named, not merely "not absent", but
// PRESENT with a real digest. A five-of-six lineage (or fewer) fails here
// rather than exporting a package that looks complete but is not.
func requireLineageHops(receipt evidence.BusinessExecutionReceipt) error {
	byName := make(map[string]evidence.Dimension, len(receipt.Dimensions))
	for _, d := range receipt.Dimensions {
		byName[d.Name] = d
	}
	for _, hop := range RequiredLineageHops {
		d, ok := byName[hop]
		if !ok || d.Status != evidence.StatusPresent || d.Digest == "" {
			return fmt.Errorf("%w: %s", ErrLineageIncomplete, hop)
		}
	}
	return nil
}
