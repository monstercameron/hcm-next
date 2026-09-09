package oidc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// parseSubjectKind maps the wire spelling of a subject kind to
// [trust.SubjectKind]. This duplicates internal/trust/federation's own
// unexported parseSubjectKind for the same reason that package's doc
// comment on parseAssurance gives: it keeps the wire vocabulary this
// package's claim mapping accepts identical to the platform's other
// federation adapter without exporting a parser from package trust.
func parseSubjectKind(s string) (trust.SubjectKind, bool) {
	switch s {
	case "human":
		return trust.SubjectKindHuman, true
	case "service":
		return trust.SubjectKindService, true
	case "agent":
		return trust.SubjectKindAgent, true
	case "integration":
		return trust.SubjectKindIntegration, true
	default:
		return trust.SubjectKindUnspecified, false
	}
}

// parseAssurance maps the wire spelling of an assurance level to
// [trust.Assurance]. See [parseSubjectKind]'s comment.
func parseAssurance(s string) (trust.Assurance, bool) {
	switch s {
	case "low":
		return trust.AssuranceLow, true
	case "substantial":
		return trust.AssuranceSubstantial, true
	case "high":
		return trust.AssuranceHigh, true
	default:
		return trust.AssuranceUnspecified, false
	}
}

// decodeClaimString decodes raw as a single JSON string. Only a JSON string
// is accepted: a boolean or number claim smuggled at a mapped source claim
// is refused rather than coerced, so a claim mapping can never be tricked
// into treating a type-confused value as text.
func decodeClaimString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("value is not a JSON string")
	}
	return s, nil
}

// decodeClaimStringSlice decodes raw as either a JSON array of strings or a
// single JSON string, in which case it is split on whitespace -- the same
// space-delimited convention OAuth's own "scope" claim uses -- into
// multiple entries.
func decodeClaimStringSlice(raw json.RawMessage) ([]string, error) {
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.Fields(s), nil
	}
	return nil, fmt.Errorf("value is neither a JSON array of strings nor a string")
}

// mapClaims applies issuer's governed [issuerregistry.ClaimMapping] list to
// raw (the verified ID token's decoded claim map) and produces the
// [trust.PrincipalSpec] [trust.NewPrincipal] will validate.
//
// Every field defaults to its least-privilege zero value and is populated
// only when the issuer's own configuration explicitly maps a source claim
// to it: an unmapped claim never leaks ambient authority into the
// principal, matching [issuerregistry.ClaimMapping]'s own doc comment that
// this is "governance metadata describing the tenant's issuer
// configuration", not a claim the raw token gets to assert on its own.
//
// Subject defaults to the ID token's own verified "sub" claim (the one
// claim OIDC Core itself guarantees identifies the end user) and is
// overridden only if the issuer's claim mappings explicitly target
// [issuerregistry.PrincipalFieldSubject] with a different source claim.
// SubjectKind defaults to [trust.SubjectKindHuman] -- this flow is
// definitionally an interactive, browser-redirect authentication, not a
// workload or service credential path -- and Assurance defaults to
// [trust.AssuranceLow], the lowest concrete level, when the issuer declares
// no richer assurance signal: this package never assumes a stronger
// assurance than the issuer affirmatively states.
func mapClaims(issuer issuerregistry.Issuer, raw map[string]json.RawMessage, std stdIDTokenClaims, credentialDigest, sessionRef string) (trust.PrincipalSpec, error) {
	spec := trust.PrincipalSpec{
		Tenant:               issuer.Tenant,
		Subject:              std.Subject,
		SubjectKind:          trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceLow,
		SessionRef:           sessionRef,
		IssuedAt:             std.IssuedAt,
		ExpiresAt:            std.ExpiresAt,
		CredentialDigest:     credentialDigest,
	}

	for _, m := range issuer.ClaimMappings {
		value, present := raw[m.SourceClaim]
		if !present {
			continue
		}
		if err := applyClaimMapping(&spec, m.Target, value); err != nil {
			return trust.PrincipalSpec{}, fmt.Errorf("%w: claim %q -> %q: %v", ErrClaimMapping, m.SourceClaim, m.Target, err)
		}
	}

	if strings.TrimSpace(spec.Subject) == "" {
		return trust.PrincipalSpec{}, fmt.Errorf("%w: no subject resolved", ErrClaimMapping)
	}
	return spec, nil
}

// applyClaimMapping assigns one decoded claim value onto spec's field named
// by target.
func applyClaimMapping(spec *trust.PrincipalSpec, target issuerregistry.PrincipalField, value json.RawMessage) error {
	switch target {
	case issuerregistry.PrincipalFieldSubject:
		s, err := decodeClaimString(value)
		if err != nil {
			return err
		}
		spec.Subject = s
	case issuerregistry.PrincipalFieldSubjectKind:
		s, err := decodeClaimString(value)
		if err != nil {
			return err
		}
		kind, ok := parseSubjectKind(s)
		if !ok {
			return fmt.Errorf("sub_kind %q is not human/service/agent/integration", s)
		}
		spec.SubjectKind = kind
	case issuerregistry.PrincipalFieldOrganizationScopeID:
		s, err := decodeClaimString(value)
		if err != nil {
			return err
		}
		spec.OrganizationScopeID = s
	case issuerregistry.PrincipalFieldRoles:
		roles, err := decodeClaimStringSlice(value)
		if err != nil {
			return err
		}
		spec.Roles = roles
	case issuerregistry.PrincipalFieldAuthorityRefs:
		refs, err := decodeClaimStringSlice(value)
		if err != nil {
			return err
		}
		spec.AuthorityRefs = refs
	case issuerregistry.PrincipalFieldPurposes:
		purposes, err := decodeClaimStringSlice(value)
		if err != nil {
			return err
		}
		spec.Purposes = purposes
	case issuerregistry.PrincipalFieldAssurance:
		s, err := decodeClaimString(value)
		if err != nil {
			return err
		}
		assurance, ok := parseAssurance(s)
		if !ok {
			return fmt.Errorf("assurance %q is not low/substantial/high", s)
		}
		spec.Assurance = assurance
	case issuerregistry.PrincipalFieldDelegationRefs:
		refs, err := decodeClaimStringSlice(value)
		if err != nil {
			return err
		}
		spec.DelegationRefs = refs
	default:
		return fmt.Errorf("unrecognized principal field %q", target)
	}
	return nil
}
