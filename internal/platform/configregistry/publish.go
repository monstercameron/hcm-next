package configregistry

// Publish mints an immutable [ConfigurationObject] and records it in store.
//
// It refuses:
//   - an object with an invalid Kind, no ID, a zero Revision, no tenant
//     scope, no SchemaRef, no PublisherPrincipal, a zero PublishedAt or an
//     empty Body;
//   - a caller-supplied CanonicalBodyDigest that does not match recomputing
//     it from Body;
//   - a different Body published under a (scope, kind, id, revision) key
//     that already carries one — a revision's content is immutable once
//     published.
//
// Publishing the exact same body under a key already published is
// idempotent: Publish returns the existing record rather than minting (or
// rejecting) a second one for identical content.
func Publish(store Store, obj ConfigurationObject) (ConfigurationObject, error) {
	if store == nil {
		return ConfigurationObject{}, refuse(CodeNoStore, obj.ID, "no store supplied")
	}
	if !obj.Kind.Valid() {
		return ConfigurationObject{}, refuse(CodeInvalidKind, obj.ID, "kind %q is not in the closed vocabulary", obj.Kind)
	}
	if obj.ID == "" {
		return ConfigurationObject{}, refuse(CodeMissingID, "", "configuration object has no id")
	}
	if obj.Revision == 0 {
		return ConfigurationObject{}, refuse(CodeInvalidRevision, obj.ID, "revision must be >= 1")
	}
	if !obj.Scope.valid() {
		return ConfigurationObject{}, refuse(CodeMissingScope, obj.ID, "configuration object has no tenant scope")
	}
	if obj.SchemaRef == "" {
		return ConfigurationObject{}, refuse(CodeMissingSchemaRef, obj.ID, "configuration object has no schema ref")
	}
	if obj.PublisherPrincipal == "" {
		return ConfigurationObject{}, refuse(CodeMissingPublisher, obj.ID, "configuration object has no publisher principal")
	}
	if obj.PublishedAt.IsZero() {
		return ConfigurationObject{}, refuse(CodeMissingPublishedAt, obj.ID, "configuration object has no published-at time")
	}
	if len(obj.Body) == 0 {
		return ConfigurationObject{}, refuse(CodeEmptyBody, obj.ID, "configuration object has no body")
	}

	bodyDigest := computeBodyDigest(obj.Body)
	if obj.CanonicalBodyDigest != "" && obj.CanonicalBodyDigest != bodyDigest {
		return ConfigurationObject{}, refuse(CodeBodyDigestMismatch, obj.ID,
			"supplied canonical body digest %s does not match the body's own digest %s", obj.CanonicalBodyDigest, bodyDigest)
	}

	ref := obj.Ref()
	if existing, found, err := store.GetObject(ref); err != nil {
		return ConfigurationObject{}, err
	} else if found {
		if existing.CanonicalBodyDigest == bodyDigest {
			return existing, nil
		}
		return ConfigurationObject{}, refuse(CodeRevisionConflict, obj.ID,
			"revision %d of %s/%s is already published with different content", obj.Revision, obj.Kind, obj.ID)
	}

	minted := obj.clone()
	minted.CanonicalBodyDigest = bodyDigest
	minted.digest = computeRecordDigest(minted)

	if err := store.PutObject(minted); err != nil {
		return ConfigurationObject{}, err
	}
	return minted, nil
}

// Activate appends an [ActivationRecord] naming ref as the newly active
// revision for its (scope, kind, id).
//
// It refuses:
//   - evidence with no ActivatedBy (an unattributed activation);
//   - evidence with a zero ActivatedAt;
//   - a ref naming a revision [Publish] never recorded — only published
//     revisions may be activated.
//
// Activating a different revision for a (scope, kind, id) that already has
// an active revision does not delete or edit the earlier [ActivationRecord]:
// it is superseded by recency, and its evidence remains in
// [Store.ListActivations]'s history.
func Activate(store Store, ref ObjectRef, evidence ActivationEvidence) (ActivationRecord, error) {
	if store == nil {
		return ActivationRecord{}, refuse(CodeNoStore, ref.ID, "no store supplied")
	}
	if evidence.ActivatedBy == "" {
		return ActivationRecord{}, refuse(CodeUnauthorizedActivation, ref.ID,
			"activation of %s/%s@%d presented no activating principal", ref.Kind, ref.ID, ref.Revision)
	}
	if evidence.ActivatedAt.IsZero() {
		return ActivationRecord{}, refuse(CodeMissingActivationTime, ref.ID,
			"activation of %s/%s@%d presented no activation time", ref.Kind, ref.ID, ref.Revision)
	}

	obj, found, err := store.GetObject(ref)
	if err != nil {
		return ActivationRecord{}, err
	}
	if !found {
		return ActivationRecord{}, refuse(CodeUnknownRevision, ref.ID,
			"no published revision %d of %s/%s; only published revisions may be activated", ref.Revision, ref.Kind, ref.ID)
	}

	rec := ActivationRecord{
		Scope:        ref.Scope,
		Kind:         ref.Kind,
		ID:           ref.ID,
		Revision:     ref.Revision,
		ActivatedBy:  evidence.ActivatedBy,
		Authority:    evidence.Authority,
		Reason:       evidence.Reason,
		ActivatedAt:  evidence.ActivatedAt,
		ObjectDigest: obj.Digest(),
	}
	if err := store.PutActivation(rec); err != nil {
		return ActivationRecord{}, err
	}
	return rec, nil
}

// Resolve returns the [ConfigurationObject] currently active for
// (scope, kind, id). It never falls back to "latest published" or any other
// guess: a (scope, kind, id) that has never been activated is refused.
func Resolve(store Store, scope Scope, kind Kind, id string) (ConfigurationObject, error) {
	if store == nil {
		return ConfigurationObject{}, refuse(CodeNoStore, id, "no store supplied")
	}
	latest, found, err := store.GetLatestActivation(scope, kind, id)
	if err != nil {
		return ConfigurationObject{}, err
	}
	if !found {
		return ConfigurationObject{}, refuse(CodeNoActiveRevision, id,
			"no revision of %s/%s has ever been activated for this scope", kind, id)
	}

	obj, found, err := store.GetObject(latest.Ref())
	if err != nil {
		return ConfigurationObject{}, err
	}
	if !found {
		return ConfigurationObject{}, refuse(CodeUnknownRevision, id,
			"active revision %d of %s/%s is recorded but no longer published", latest.Revision, kind, id)
	}
	return obj, nil
}
