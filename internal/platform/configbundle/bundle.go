// Package configbundle compiles immutable, hermetic configuration bundles.
// It consumes only the platform configuration registry contract; storage,
// databases, clocks, and network clients are deliberately outside this
// package.
package configbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	platformconfig "github.com/monstercameron/hcm-next/internal/platform/configregistry"
)

// Version is the configuration-bundle manifest contract version.
const manifestVersion = 1

// Version reports this package's canonical manifest version.
func Version() int { return manifestVersion }

// Explain describes the package's hermeticity boundary.
func Explain() string {
	return "configuration bundles v1: exact active revisions, closed dependency closure, and order-independent sha256 digest"
}

// Store is the immutable publication/activation registry consumed by Compile.
type Store = platformconfig.Store

// Scope and ObjectRef are aliases so callers do not need a translation layer
// when moving from the publication registry to the bundle compiler.
type Scope = platformconfig.Scope
type ObjectRef = platformconfig.ObjectRef
type Kind = platformconfig.Kind

// Configuration-kind aliases keep bundle call sites concise while retaining
// the platform registry's closed vocabulary.
const (
	KindWorkflow   = platformconfig.KindWorkflow
	KindPolicy     = platformconfig.KindPolicy
	KindSchema     = platformconfig.KindSchema
	KindRule       = platformconfig.KindRule
	KindConnector  = platformconfig.KindConnector
	KindAgent      = platformconfig.KindAgent
	KindReference  = platformconfig.KindReference
	KindMapping    = platformconfig.KindMapping
	KindCapability = platformconfig.KindCapability
)

// CompileOptions describes a bundle compilation. Roots and Refs are
// compatibility spellings; Roots is preferred. TargetScope may be a Scope or
// a tenant string. A scope and runtime floor are required when options are
// supplied, because they are part of the bundle's immutable identity.
type CompileOptions struct {
	BundleID              string
	Roots                 []ObjectRef
	Refs                  []ObjectRef
	TargetScope           any
	Scope                 any
	MinimumRuntimeVersion string
	RuntimeVersion        string
}

// Options is the concise spelling for CompileOptions.
type Options = CompileOptions

// DependencyRef is one reference declared by an object's JSON body. Scope
// may be omitted in the body, in which case the declaring object's scope is
// inherited. Revision is never inferred: zero is an unpinned reference.
type DependencyRef struct {
	Ref    ObjectRef `json:"ref"`
	Digest string    `json:"digest,omitempty"`
}

// Dependency is a descriptive alias for DependencyRef.
type Dependency = DependencyRef

// IncludedObject is the content identity recorded in a compiled bundle.
// Digest is the registry record digest, so it covers the complete immutable
// object record, not merely the opaque body.
type IncludedObject struct {
	Ref        ObjectRef `json:"ref"`
	Digest     string    `json:"digest"`
	BodyDigest string    `json:"body_digest"`
}

// Object is a concise spelling for IncludedObject.
type Object = IncludedObject

// Bundle is an immutable compilation result by convention: Compile copies
// every slice it stores. Callers can use Verify after retaining or serializing
// a value to detect mutation of an exported copy.
type Bundle struct {
	BundleID        string `json:"bundle_id"`
	ManifestVersion int    `json:"manifest_version"`
	TargetScope     Scope  `json:"target_scope"`
	// Scope is retained as a compatibility spelling for consumers that call
	// the target simply Scope. Compile sets both fields to the same value.
	Scope                 Scope            `json:"scope"`
	MinimumRuntimeVersion string           `json:"minimum_runtime_version"`
	Roots                 []ObjectRef      `json:"roots"`
	Objects               []IncludedObject `json:"objects"`
	// Dependencies is a read-friendly compatibility spelling. It is populated
	// with the same values as Objects by Compile.
	Dependencies []IncludedObject `json:"dependencies"`
	Digest       string           `json:"digest"`
}

// Error is a typed compilation refusal. Code is stable for callers; Detail
// is for diagnostics and must not be used as a programmatic discriminator.
type Error struct {
	Code   string
	Ref    string
	Detail string
	Cause  error
}

func (e *Error) Error() string {
	where := ""
	if e.Ref != "" {
		where = " [" + e.Ref + "]"
	}
	msg := fmt.Sprintf("configbundle: %s%s: %s", e.Code, where, e.Detail)
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrCompile, e.Cause}
	}
	return []error{ErrCompile}
}

// Stable refusal causes.
var (
	ErrCompile               = errors.New("configbundle: compilation refused")
	ErrInvalidRequest        = errors.New("configbundle: invalid compile request")
	ErrUnpinnedReference     = errors.New("configbundle: reference is not pinned")
	ErrUnpublishedObject     = errors.New("configbundle: object is unpublished")
	ErrSupersededObject      = errors.New("configbundle: object is superseded")
	ErrDependencyCycle       = errors.New("configbundle: dependency cycle")
	ErrMalformedDependencies = errors.New("configbundle: malformed dependency declaration")
	ErrScopeMismatch         = errors.New("configbundle: object scope differs from bundle scope")
	ErrDigestMismatch        = errors.New("configbundle: declared digest differs from registry")
	ErrRegistryMutation      = errors.New("configbundle: registry object failed verification")
	ErrDuplicateReference    = errors.New("configbundle: duplicate declared reference")
	ErrMissingRuntimeVersion = errors.New("configbundle: minimum runtime version is missing")
	ErrMissingTargetScope    = errors.New("configbundle: target scope is missing")
)

// CodeOf returns the stable refusal code carried by err.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code, ref string, cause error, format string, args ...any) error {
	return &Error{Code: code, Ref: ref, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}

// Compile accepts either CompileOptions (preferred) or []ObjectRef for a
// small call-site convenience. The slice form infers the scope from its first
// root and uses an explicit "unspecified" runtime marker; option-based calls
// must declare both values.
func Compile(store Store, request any) (Bundle, error) {
	if store == nil {
		return Bundle{}, refuse("NO_STORE", "", ErrInvalidRequest, "publication registry is nil")
	}
	opts, optionForm, err := normalizeOptions(request)
	if err != nil {
		return Bundle{}, err
	}
	if len(opts.Roots) == 0 {
		return Bundle{}, refuse("NO_ROOTS", "", ErrInvalidRequest, "at least one root reference is required")
	}
	target, ok := optionScope(opts.TargetScope)
	if !ok {
		if optionForm {
			return Bundle{}, refuse("MISSING_TARGET_SCOPE", "", ErrMissingTargetScope, "target scope must be declared")
		}
		target = opts.Roots[0].Scope
	}
	if target.TenantID == "" {
		return Bundle{}, refuse("MISSING_TARGET_SCOPE", "", ErrMissingTargetScope, "target scope must include a tenant")
	}
	runtime := strings.TrimSpace(opts.MinimumRuntimeVersion)
	if runtime == "" {
		runtime = strings.TrimSpace(opts.RuntimeVersion)
	}
	if runtime == "" {
		if optionForm {
			return Bundle{}, refuse("MISSING_RUNTIME_VERSION", "", ErrMissingRuntimeVersion, "minimum runtime version must be declared")
		}
		runtime = "unspecified"
	}

	seen := make(map[string]bool)
	visiting := make(map[string]bool)
	objects := make([]IncludedObject, 0, len(opts.Roots))
	var visit func(ObjectRef, string) error
	visit = func(ref ObjectRef, parentScope string) error {
		if ref.Scope.TenantID == "" {
			if parentScope == "" {
				return refuse("UNPINNED_REFERENCE", refKey(ref), ErrUnpinnedReference, "tenant scope is missing")
			}
			ref.Scope = target
		}
		if ref.Revision == 0 || !ref.Kind.Valid() || strings.TrimSpace(ref.ID) == "" {
			return refuse("UNPINNED_REFERENCE", refKey(ref), ErrUnpinnedReference, "kind, id, tenant scope, and positive revision are required")
		}
		if ref.Scope != target {
			return refuse("SCOPE_MISMATCH", refKey(ref), ErrScopeMismatch, "object scope is not the bundle target scope")
		}
		key := refKey(ref)
		if visiting[key] {
			return refuse("DEPENDENCY_CYCLE", key, ErrDependencyCycle, "reference occurs again while its dependencies are being visited")
		}
		if seen[key] {
			return nil
		}
		obj, err := resolveActive(store, ref)
		if err != nil {
			return err
		}
		visiting[key] = true
		deps, err := ExtractDependencies(obj.Body)
		if err != nil {
			return refuse("MALFORMED_DEPENDENCIES", key, ErrMalformedDependencies, "%v", err)
		}
		declared := make(map[string]bool, len(deps))
		for _, dep := range deps {
			depRef := dep.Ref
			if depRef.Scope.TenantID == "" {
				depRef.Scope = ref.Scope
			}
			depKey := refKey(depRef)
			if declared[depKey] {
				return refuse("DUPLICATE_REFERENCE", depKey, ErrDuplicateReference, "object declares the same dependency twice")
			}
			declared[depKey] = true
			if dep.Digest != "" {
				// The digest is checked after resolving the exact object so a
				// digest can never cause an ambient lookup.
				depRefDigest := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(dep.Digest)), "sha256:")
				resolved, resolveErr := resolveActive(store, depRef)
				if resolveErr != nil {
					return resolveErr
				}
				if !strings.EqualFold(depRefDigest, resolved.Digest()) {
					return refuse("DIGEST_MISMATCH", depKey, ErrDigestMismatch, "declared %q, registry has %q", dep.Digest, resolved.Digest())
				}
			}
			if err := visit(depRef, ref.Scope.TenantID); err != nil {
				return err
			}
		}
		delete(visiting, key)
		seen[key] = true
		objects = append(objects, IncludedObject{Ref: ref, Digest: obj.Digest(), BodyDigest: obj.CanonicalBodyDigest})
		return nil
	}

	roots := append([]ObjectRef(nil), opts.Roots...)
	sort.Slice(roots, func(i, j int) bool { return refKey(roots[i]) < refKey(roots[j]) })
	for _, root := range roots {
		if err := visit(root, ""); err != nil {
			return Bundle{}, err
		}
	}
	sort.Slice(objects, func(i, j int) bool { return refKey(objects[i].Ref) < refKey(objects[j].Ref) })
	b := Bundle{
		BundleID: opts.BundleID, ManifestVersion: manifestVersion,
		TargetScope: target, Scope: target, MinimumRuntimeVersion: runtime,
		Roots: append([]ObjectRef(nil), roots...), Objects: append([]IncludedObject(nil), objects...),
	}
	if b.BundleID == "" {
		b.BundleID = "configuration-bundle"
	}
	b.Dependencies = append([]IncludedObject(nil), b.Objects...)
	digest, err := BundleDigest(b)
	if err != nil {
		return Bundle{}, err
	}
	b.Digest = digest
	return b, nil
}

// CompileBundle is the descriptive alias for Compile.
func CompileBundle(store Store, request any) (Bundle, error) { return Compile(store, request) }

func normalizeOptions(request any) (CompileOptions, bool, error) {
	switch value := request.(type) {
	case CompileOptions:
		if value.Roots == nil {
			value.Roots = append([]ObjectRef(nil), value.Refs...)
		}
		return value, true, nil
	case *CompileOptions:
		if value == nil {
			return CompileOptions{}, true, refuse("INVALID_REQUEST", "", ErrInvalidRequest, "compile options are nil")
		}
		return normalizeOptions(*value)
	case []ObjectRef:
		return CompileOptions{Roots: append([]ObjectRef(nil), value...)}, false, nil
	default:
		return CompileOptions{}, true, refuse("INVALID_REQUEST", "", ErrInvalidRequest, "want CompileOptions or []ObjectRef, got %T", request)
	}
}

func optionScope(value any) (Scope, bool) {
	switch value := value.(type) {
	case Scope:
		return value, value.TenantID != ""
	case *Scope:
		if value == nil {
			return Scope{}, false
		}
		return *value, value.TenantID != ""
	case string:
		return Scope{TenantID: strings.TrimSpace(value)}, strings.TrimSpace(value) != ""
	default:
		return Scope{}, false
	}
}

func resolveActive(store Store, ref ObjectRef) (platformconfig.ConfigurationObject, error) {
	if ref.Revision == 0 || !ref.Kind.Valid() || strings.TrimSpace(ref.ID) == "" || ref.Scope.TenantID == "" {
		return platformconfig.ConfigurationObject{}, refuse("UNPINNED_REFERENCE", refKey(ref), ErrUnpinnedReference, "kind, id, tenant scope, and positive revision are required")
	}
	obj, found, err := store.GetObject(ref)
	if err != nil {
		return platformconfig.ConfigurationObject{}, refuse("REGISTRY_ERROR", refKey(ref), ErrCompile, "%v", err)
	}
	if !found {
		return platformconfig.ConfigurationObject{}, refuse("UNPUBLISHED_OBJECT", refKey(ref), ErrUnpublishedObject, "exact revision is not published")
	}
	if err := obj.Verify(); err != nil {
		return platformconfig.ConfigurationObject{}, refuse("REGISTRY_MUTATION", refKey(ref), ErrRegistryMutation, "%v", err)
	}
	active, found, err := store.GetLatestActivation(ref.Scope, ref.Kind, ref.ID)
	if err != nil {
		return platformconfig.ConfigurationObject{}, refuse("REGISTRY_ERROR", refKey(ref), ErrCompile, "%v", err)
	}
	if !found {
		return platformconfig.ConfigurationObject{}, refuse("UNPUBLISHED_OBJECT", refKey(ref), ErrUnpublishedObject, "exact revision has not been activated")
	}
	if active.Ref() != ref {
		return platformconfig.ConfigurationObject{}, refuse("SUPERSEDED_OBJECT", refKey(ref), ErrSupersededObject, "active revision is %d", active.Revision)
	}
	if active.ObjectDigest != "" && active.ObjectDigest != obj.Digest() {
		return platformconfig.ConfigurationObject{}, refuse("REGISTRY_MUTATION", refKey(ref), ErrRegistryMutation, "activation digest does not match object digest")
	}
	return obj, nil
}

func refKey(ref ObjectRef) string {
	return ref.Scope.TenantID + "/" + ref.Scope.CellID + "/" + string(ref.Kind) + "/" + ref.ID + "@" + strconv.FormatUint(uint64(ref.Revision), 10)
}

// ExtractDependencies reads the stable JSON dependency declaration accepted by
// the compiler. The body may use dependencies, dependency_refs, or refs; all
// three are aliases for the same ordered-free set. A non-JSON opaque body has
// no declared dependencies and is valid for leaf objects.
func ExtractDependencies(body []byte) ([]DependencyRef, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || (trimmed[0] != '{' && trimmed[0] != '[') {
		return nil, nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, err
	}
	var out []DependencyRef
	for _, name := range []string{"dependencies", "dependency_refs", "refs"} {
		raw, ok := document[name]
		if !ok {
			continue
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("%s must be an array: %w", name, err)
		}
		for _, entry := range entries {
			dep, err := decodeDependency(entry)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			out = append(out, dep)
		}
	}
	return out, nil
}

func decodeDependency(raw json.RawMessage) (DependencyRef, error) {
	var wire struct {
		Ref      *ObjectRef `json:"ref"`
		Scope    Scope      `json:"scope"`
		Kind     Kind       `json:"kind"`
		ID       string     `json:"id"`
		Revision uint32     `json:"revision"`
		Version  any        `json:"version"`
		Digest   string     `json:"digest"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return DependencyRef{}, err
	}
	ref := ObjectRef{Scope: wire.Scope, Kind: wire.Kind, ID: wire.ID, Revision: wire.Revision}
	if wire.Ref != nil {
		ref = *wire.Ref
	}
	if ref.Revision == 0 && wire.Version != nil {
		switch version := wire.Version.(type) {
		case float64:
			if version >= 1 && version == float64(uint32(version)) {
				ref.Revision = uint32(version)
			}
		case string:
			n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(version), "v"), 10, 32)
			if err == nil {
				ref.Revision = uint32(n)
			}
		}
	}
	if wire.Ref != nil && wire.Digest != "" {
		return DependencyRef{}, errors.New("digest may be declared only beside a direct ref")
	}
	return DependencyRef{Ref: ref, Digest: strings.TrimSpace(wire.Digest)}, nil
}

// OrderedObjects returns a defensive, canonical-key ordered copy.
func (b Bundle) OrderedObjects() []IncludedObject {
	objects := append([]IncludedObject(nil), b.Objects...)
	sort.Slice(objects, func(i, j int) bool { return refKey(objects[i].Ref) < refKey(objects[j].Ref) })
	return objects
}

// ObjectsCopy returns a defensive copy of the included object identities.
func (b Bundle) ObjectsCopy() []IncludedObject { return append([]IncludedObject(nil), b.Objects...) }

// RootsCopy returns a defensive copy of the root set.
func (b Bundle) RootsCopy() []ObjectRef { return append([]ObjectRef(nil), b.Roots...) }

type canonicalBundle struct {
	Profile               string           `json:"profile"`
	BundleID              string           `json:"bundle_id"`
	ManifestVersion       int              `json:"manifest_version"`
	TargetScope           Scope            `json:"target_scope"`
	MinimumRuntimeVersion string           `json:"minimum_runtime_version"`
	Roots                 []ObjectRef      `json:"roots"`
	Objects               []IncludedObject `json:"objects"`
}

func (b Bundle) canonical() (canonicalBundle, error) {
	if b.BundleID == "" || b.ManifestVersion != manifestVersion {
		return canonicalBundle{}, refuse("INVALID_BUNDLE", "", ErrInvalidRequest, "bundle id and manifest version are required")
	}
	target := b.TargetScope
	if target.TenantID == "" {
		target = b.Scope
	}
	if target.TenantID == "" || strings.TrimSpace(b.MinimumRuntimeVersion) == "" {
		return canonicalBundle{}, refuse("INVALID_BUNDLE", "", ErrInvalidRequest, "bundle scope and runtime floor are required")
	}
	objects := b.OrderedObjects()
	if len(objects) == 0 && len(b.Dependencies) != 0 {
		objects = append([]IncludedObject(nil), b.Dependencies...)
		sort.Slice(objects, func(i, j int) bool { return refKey(objects[i].Ref) < refKey(objects[j].Ref) })
	}
	for _, object := range objects {
		if object.Ref.Revision == 0 || object.Digest == "" || object.Ref.Scope != target {
			return canonicalBundle{}, refuse("INVALID_BUNDLE", refKey(object.Ref), ErrInvalidRequest, "included objects must be exact, digested, and in target scope")
		}
	}
	roots := append([]ObjectRef(nil), b.Roots...)
	sort.Slice(roots, func(i, j int) bool { return refKey(roots[i]) < refKey(roots[j]) })
	return canonicalBundle{Profile: "hcmnext.platform.configbundle.Bundle/v1", BundleID: b.BundleID, ManifestVersion: b.ManifestVersion, TargetScope: target, MinimumRuntimeVersion: b.MinimumRuntimeVersion, Roots: roots, Objects: objects}, nil
}

// Canonical returns the deterministic manifest bytes, or nil for an invalid
// bundle.
func (b Bundle) Canonical() []byte {
	canonical, err := b.canonical()
	if err != nil {
		return nil
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil
	}
	return encoded
}

// BundleDigest computes the canonical digest over every included object
// digest and the immutable bundle metadata.
func BundleDigest(b Bundle) (string, error) {
	encoded := b.Canonical()
	if encoded == nil {
		_, err := b.canonical()
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// DigestValue recomputes the bundle digest, independent of its recorded
// Digest field.
func (b Bundle) DigestValue() (string, error) { return BundleDigest(b) }

// Verify checks the recorded digest and the canonical identity.
func (b Bundle) Verify() error {
	got, err := BundleDigest(b)
	if err != nil {
		return err
	}
	if got != b.Digest {
		return refuse("BUNDLE_MUTATED", "", ErrCompile, "recorded digest %q, canonical digest %q", b.Digest, got)
	}
	return nil
}

// Explain describes this particular immutable bundle.
func (b Bundle) Explain() string {
	return fmt.Sprintf("configuration bundle %s with %d pinned objects for %s (%s)", b.BundleID, len(b.Objects), b.TargetScope.TenantID, b.Digest)
}

// ChangeKind names one object-level bundle difference.
type ChangeKind string

const (
	ChangeAdded       ChangeKind = "ADDED"
	ChangeRemoved     ChangeKind = "REMOVED"
	ChangeReversioned ChangeKind = "REVERSIONED"
)

// DiffEntry names an added, removed, or re-versioned logical object.
type DiffEntry struct {
	Key          string     `json:"key"`
	Kind         ChangeKind `json:"kind"`
	Before       ObjectRef  `json:"before"`
	After        ObjectRef  `json:"after"`
	BeforeDigest string     `json:"before_digest,omitempty"`
	AfterDigest  string     `json:"after_digest,omitempty"`
}

// DiffResult is the deterministic comparison of two bundles.
type DiffResult struct {
	Added       []DiffEntry `json:"added"`
	Removed     []DiffEntry `json:"removed"`
	Reversioned []DiffEntry `json:"reversioned"`
	// Changed is a compatibility spelling for Reversioned.
	Changed []DiffEntry `json:"changed"`
	Digest  string      `json:"digest"`
}

// Diff names logical objects present in only one bundle and logical objects
// whose pinned revision or content digest changed.
func Diff(before, after Bundle) DiffResult {
	left := make(map[string]IncludedObject)
	right := make(map[string]IncludedObject)
	for _, object := range before.OrderedObjects() {
		left[logicalKey(object.Ref)] = object
	}
	for _, object := range after.OrderedObjects() {
		right[logicalKey(object.Ref)] = object
	}
	keys := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for key := range left {
		seen[key] = true
	}
	for key := range right {
		seen[key] = true
	}
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result DiffResult
	for _, key := range keys {
		old, hasOld := left[key]
		newObject, hasNew := right[key]
		switch {
		case !hasOld:
			result.Added = append(result.Added, DiffEntry{Key: key, Kind: ChangeAdded, After: newObject.Ref, AfterDigest: newObject.Digest})
		case !hasNew:
			result.Removed = append(result.Removed, DiffEntry{Key: key, Kind: ChangeRemoved, Before: old.Ref, BeforeDigest: old.Digest})
		case old.Ref != newObject.Ref || old.Digest != newObject.Digest:
			result.Reversioned = append(result.Reversioned, DiffEntry{Key: key, Kind: ChangeReversioned, Before: old.Ref, After: newObject.Ref, BeforeDigest: old.Digest, AfterDigest: newObject.Digest})
		}
	}
	result.Changed = append([]DiffEntry(nil), result.Reversioned...)
	data, _ := json.Marshal(struct {
		Added, Removed, Reversioned []DiffEntry
	}{result.Added, result.Removed, result.Reversioned})
	sum := sha256.Sum256(append([]byte("hcmnext.platform.configbundle.Diff/v1\x00"), data...))
	result.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return result
}

func logicalKey(ref ObjectRef) string {
	return ref.Scope.TenantID + "/" + ref.Scope.CellID + "/" + string(ref.Kind) + "/" + ref.ID
}
