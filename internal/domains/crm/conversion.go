package crm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var ErrCRM005Rejected = errors.New("CRM_005_REJECTED")

const (
	CRM005IntentType    = "hcm.recruiting.create_candidate"
	CRM005IntentVersion = "v1"
)

type ConversionRejection struct {
	Field, State string
	Version      values.RevisionToken
}

// CandidateConversionProposal is a zero-write preflight artifact. It is not
// evidence that identity was resolved or that recruiting created either role;
// those outcomes must come from their semantic owners.
type CandidateConversionProposal struct {
	Prospect             ProspectRevision
	ProposedCandidate    values.EntityRef
	ProposedApplication  values.EntityRef
	ProposedIdentityLink values.EntityRef
	IntentType           string
	IntentVersion        string
	At                   values.Instant
	Source               ProspectSourceAttribution
	Consent              ProspectConsent
}

func PrepareProspectConversion(p ProspectRevision, candidate, application, identityLink values.EntityRef, intentType, intentVersion string, at values.Instant) (CandidateConversionProposal, *ConversionRejection, error) {
	reject := func(field, state string) (CandidateConversionProposal, *ConversionRejection, error) {
		rejection := &ConversionRejection{Field: field, State: state, Version: p.Revision}
		return CandidateConversionProposal{}, rejection, fmt.Errorf("%w: field=%s state=%s version=%s", ErrCRM005Rejected, field, state, p.Revision.String())
	}
	if err := at.Validate(); err != nil {
		return reject("at", "INVALID")
	}
	if err := p.Validate(); err != nil {
		return reject("prospect", "INVALID")
	}
	if strings.TrimSpace(p.Attribution.Campaign) == "" {
		return reject("source.campaign", "UNBOUND")
	}
	if d := p.Consent.OutreachAt(at); !d.Allowed {
		return reject("consent", string(d.Status))
	}
	if err := candidate.Validate(); err != nil || candidate.Tenant != p.ProspectID.Tenant || candidate.Kind != "candidate" {
		return reject("candidate", "INVALID")
	}
	if err := application.Validate(); err != nil || application.Tenant != p.ProspectID.Tenant || application.Kind != "application" {
		return reject("application", "INVALID")
	}
	if err := identityLink.Validate(); err != nil || identityLink.Tenant != p.ProspectID.Tenant || identityLink.Kind != "identity_link" {
		return reject("identity_link", "INVALID")
	}
	if intentType != CRM005IntentType || intentVersion != CRM005IntentVersion {
		return reject("intent", "UNBOUND")
	}
	return CandidateConversionProposal{Prospect: p, ProposedCandidate: candidate, ProposedApplication: application, ProposedIdentityLink: identityLink, IntentType: intentType, IntentVersion: intentVersion, At: at, Source: p.Attribution, Consent: p.Consent}, nil, nil
}
