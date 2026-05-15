package contactinfo

import "hcm-next-executor/internal/executor"

const (
	// PreflightBlockName is the contact-info deterministic validation block name.
	PreflightBlockName = "system.employee_data.contact_info.preflight"
	// PlanTransactionBlockName is the contact-info deterministic transaction planning block name.
	PlanTransactionBlockName = "system.employee_data.contact_info.plan_transaction"
	// BlockVersionV1 is the V0 contact-info block version.
	BlockVersionV1 = "1.0.0"
)

// PostalAddress is the normalized address contract shared by contact-info blocks.
type PostalAddress struct {
	Line1      string  `json:"line1"`
	Line2      *string `json:"line2"`
	City       string  `json:"city"`
	Region     string  `json:"region"`
	PostalCode string  `json:"postalCode"`
	Country    string  `json:"country"`
}

// ContactInfo is the normalized contact-information contract shared by contact-info blocks.
type ContactInfo struct {
	PersonalEmail *string       `json:"personalEmail"`
	MobilePhone   *string       `json:"mobilePhone"`
	HomeAddress   PostalAddress `json:"homeAddress"`
}

// ValidationMessage is the generic block validation message used by contact-info blocks.
type ValidationMessage = executor.ValidationIssue

// InternalWriteSpec is the legacy contact-info output name for generic ledger facts.
type InternalWriteSpec = executor.LedgerFact

// ProjectionPatch is the generic projection mutation emitted by contact-info blocks.
type ProjectionPatch = executor.ProjectionPatch

// RegisterBlocks registers the V0 contact-info deterministic blocks.
func RegisterBlocks(registry *executor.Registry) error {
	if err := registry.Register(executor.BlockReference{Name: PreflightBlockName, Version: BlockVersionV1}, ExecutePreflight); err != nil {
		return err
	}

	return registry.Register(executor.BlockReference{Name: PlanTransactionBlockName, Version: BlockVersionV1}, ExecutePlanTransaction)
}
