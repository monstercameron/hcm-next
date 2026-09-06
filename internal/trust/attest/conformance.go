package attest

import (
	"fmt"
	"strings"
)

// AcknowledgementDomain keeps the three conformance slices distinct while
// they share the same statement, binding, response and trusted-time evidence.
type AcknowledgementDomain string

const (
	DomainTime    AcknowledgementDomain = "TIME"
	DomainPayroll AcknowledgementDomain = "PAYROLL"
	DomainLegal   AcknowledgementDomain = "LEGAL"
)

type AcknowledgementClaim string

const (
	ClaimTimecardAccuracy         AcknowledgementClaim = "TIMECARD_ACCURACY"
	ClaimMealBreak                AcknowledgementClaim = "MEAL_BREAK"
	ClaimPayrollInputCompleteness AcknowledgementClaim = "PAYROLL_INPUT_COMPLETENESS"
	ClaimLegalFact                AcknowledgementClaim = "LEGAL_FACTUAL_ACKNOWLEDGEMENT"
)

// ConformanceCase is a domain-specific claim carried by the shared evidence
// contract. It is not an approval, signature or form submission.
type ConformanceCase struct {
	Domain   AcknowledgementDomain
	Claim    AcknowledgementClaim
	Response Response
}

type ConformanceReport struct {
	Valid                 bool
	Cases                 int
	Domains               []AcknowledgementDomain
	SharedEvidence        bool
	DistinctFromApproval  bool
	DistinctFromSignature bool
	DistinctFromForm      bool
	Invalid               string
}

func (c ConformanceCase) validate() error {
	if c.Domain != DomainTime && c.Domain != DomainPayroll && c.Domain != DomainLegal {
		return fmt.Errorf("%w: domain", ErrConformance)
	}
	if strings.TrimSpace(string(c.Claim)) == "" {
		return fmt.Errorf("%w: claim", ErrConformance)
	}
	if !c.Response.Status.Valid() || c.Response.Digest == "" || c.Response.BindingDigest == "" || c.Response.RecordedAt.Validate() != nil {
		return fmt.Errorf("%w: response evidence", ErrConformance)
	}
	return nil
}

// ProveConformance proves that Time, Payroll and Legal acknowledgements use
// the same evidence shape, while retaining their domain claim vocabulary.
func ProveConformance(cases []ConformanceCase) (ConformanceReport, error) {
	r := ConformanceReport{Cases: len(cases), SharedEvidence: true, DistinctFromApproval: true, DistinctFromSignature: true, DistinctFromForm: true}
	seen := map[AcknowledgementDomain]bool{}
	for i, c := range cases {
		if err := c.validate(); err != nil {
			r.Invalid = fmt.Sprintf("cases[%d]", i)
			return r, err
		}
		if seen[c.Domain] {
			r.Invalid = "duplicate domain"
			return r, fmt.Errorf("%w: duplicate domain %s", ErrConformance, c.Domain)
		}
		seen[c.Domain] = true
		r.Domains = append(r.Domains, c.Domain)
	}
	if len(seen) != 3 || !seen[DomainTime] || !seen[DomainPayroll] || !seen[DomainLegal] {
		r.Invalid = "domains"
		return r, fmt.Errorf("%w: Time, Payroll and Legal are all required", ErrConformance)
	}
	r.Valid = true
	return r, nil
}
