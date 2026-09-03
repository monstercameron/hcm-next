package capability

// validate enforces CAP-001's RED clause: publication rejects a definition
// missing owner, schema, risk, effect, idempotency, AuthZ, legal,
// entitlement, SLO or test references. It does not check the implementation
// binding; Registry.Register checks that separately because the binding is a
// registry-time argument, not a Definition field.
func validate(d Definition) error {
	if d.ID == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "ID", Reason: "is required"}
	}
	if d.Version < 1 {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "Version", Reason: "must be >= 1"}
	}
	if d.OwnerDomain == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "OwnerDomain", Reason: "is required"}
	}
	for name, ref := range map[string]SchemaRef{
		"RequestSchema":  d.RequestSchema,
		"ResponseSchema": d.ResponseSchema,
		"ErrorSchema":    d.ErrorSchema,
	} {
		if !ref.Valid() {
			return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: name, Reason: "must name a schema id, version and protobuf full name"}
		}
	}
	if !d.EffectClass.Valid() {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "EffectClass", Reason: "must be one of the five declared effect classes"}
	}
	if d.RiskClass == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "RiskClass", Reason: "is required"}
	}
	if d.IdempotencyPolicyRef == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "IdempotencyPolicyRef", Reason: "is required"}
	}
	if d.AuthZScopeRef == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "AuthZScopeRef", Reason: "is required"}
	}
	if d.LegalBasisRef == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "LegalBasisRef", Reason: "is required"}
	}
	if d.EntitlementRef == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "EntitlementRef", Reason: "is required"}
	}
	if d.SLOClassRef == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "SLOClassRef", Reason: "is required"}
	}
	if d.TestRef == "" {
		return ErrDefinitionInvalid{ID: d.ID, Version: d.Version, Field: "TestRef", Reason: "is required"}
	}
	return nil
}
