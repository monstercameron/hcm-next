package digest

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sort"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/canonical"
)

// AlgorithmSHA256 is the initial eligible digest algorithm.
const AlgorithmSHA256 = "sha256"

// scopeMagic prefixes scope binding bytes so a scope digest can never collide
// with a canonical payload digest.
const scopeMagic = "hcmnext.scope.v1"

// Scope is the tenant, intent and proposal identity a digest is bound to. It
// stops a digest computed for one subject from being replayed against another.
type Scope struct {
	TenantID           string
	IntentID           string
	ProposalRevisionID string
}

// ScopeSpec tells the registry where a profile's canonical model carries its
// scope identity. An empty path means the model does not carry that component,
// which then contributes the empty string.
type ScopeSpec struct {
	TenantPath           string
	IntentPath           string
	ProposalRevisionPath string
}

// Algorithm is a published digest algorithm. Eligibility is registry-owned.
type Algorithm struct {
	ID  string
	New func() hash.Hash
}

type entry struct {
	plan  *canonical.Plan
	scope ScopeSpec
}

// Registry publishes canonicalization profile versions and digest algorithms,
// and is the only thing that mints or verifies a [Reference].
//
// Profile versions are immutable once registered: registering the same
// (profile id, version) twice fails. That immutability is what lets a
// historical reference keep verifying after a newer version is published.
type Registry struct {
	mu               sync.RWMutex
	profiles         map[Key]entry
	latest           map[string]uint32
	algorithms       map[string]Algorithm
	defaultAlgorithm string
}

// NewRegistry returns an empty registry with sha256 published.
func NewRegistry() *Registry {
	r := &Registry{
		profiles:   map[Key]entry{},
		latest:     map[string]uint32{},
		algorithms: map[string]Algorithm{},
	}
	// sha256 is the initial eligible algorithm; RegisterAlgorithm cannot fail
	// on an empty registry.
	_ = r.RegisterAlgorithm(Algorithm{ID: AlgorithmSHA256, New: func() hash.Hash { return sha256.New() }})
	return r
}

// RegisterAlgorithm publishes a digest algorithm.
func (r *Registry) RegisterAlgorithm(a Algorithm) error {
	if a.ID == "" || a.New == nil {
		return newError("RegisterAlgorithm", ErrUnknownAlgorithm, "algorithm id and constructor are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.algorithms[a.ID]; ok {
		return newError("RegisterAlgorithm", ErrAlreadyRegistered, "algorithm %q", a.ID)
	}
	r.algorithms[a.ID] = a
	if r.defaultAlgorithm == "" {
		r.defaultAlgorithm = a.ID
	}
	return nil
}

// SetDefaultAlgorithm chooses the algorithm [Registry.Compute] uses when the
// caller does not name one. It never affects verification, which always uses
// the algorithm named in the reference being verified.
func (r *Registry) SetDefaultAlgorithm(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.algorithms[id]; !ok {
		return newError("SetDefaultAlgorithm", ErrUnknownAlgorithm, "%q", id)
	}
	r.defaultAlgorithm = id
	return nil
}

// RegisterProfile publishes one profile version. The profile is compiled
// against its canonical model, and — for profile ids the platform constrains —
// checked against the required material floor and the forbidden revalidated
// context list before it is accepted.
func (r *Registry) RegisterProfile(p canonical.Profile, scope ScopeSpec) error {
	if err := checkMateriality(p); err != nil {
		return err
	}
	plan, err := canonical.Compile(p)
	if err != nil {
		return err
	}
	key := Key{ProfileID: p.ID, Version: p.Version}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.profiles[key]; ok {
		return newError("RegisterProfile", ErrAlreadyRegistered, "profile %s", key)
	}
	r.profiles[key] = entry{plan: plan, scope: scope}
	if r.latest[p.ID] < p.Version {
		r.latest[p.ID] = p.Version
	}
	return nil
}

// Profile returns a published profile version.
func (r *Registry) Profile(key Key) (canonical.Profile, error) {
	e, err := r.lookup(key)
	if err != nil {
		return canonical.Profile{}, err
	}
	return e.plan.Profile(), nil
}

// LatestKey returns the highest published version of a profile id.
func (r *Registry) LatestKey(profileID string) (Key, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.latest[profileID]
	if !ok {
		return Key{}, newError("LatestKey", ErrUnknownProfile, "%q has no published version", profileID)
	}
	return Key{ProfileID: profileID, Version: v}, nil
}

// Keys lists every published profile version, sorted.
func (r *Registry) Keys() []Key {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Key, 0, len(r.profiles))
	for k := range r.profiles {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProfileID != out[j].ProfileID {
			return out[i].ProfileID < out[j].ProfileID
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func (r *Registry) lookup(key Key) (entry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.profiles[key]
	if !ok {
		return entry{}, newError("profile", ErrUnknownProfile, "%s is not published", key)
	}
	return e, nil
}

func (r *Registry) algorithm(id string) (Algorithm, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.algorithms[id]
	if !ok {
		return Algorithm{}, newError("algorithm", ErrUnknownAlgorithm, "%q is not published", id)
	}
	return a, nil
}

// Options tunes a single Compute call.
type Options struct {
	// AlgorithmID overrides the registry default.
	AlgorithmID string
	// Scope overrides the scope derived from the message. Supply it when the
	// canonical model does not itself carry every scope component; the same
	// scope must then be supplied to VerifyWithScope.
	Scope *Scope
	// CanonicalBytesArtifactRef records durable storage of the canonical bytes.
	CanonicalBytesArtifactRef *string
	// MaterialProfileRef overrides the default published-profile reference.
	MaterialProfileRef *string
}

// Compute canonicalizes msg under the latest published version of profileID
// and returns the digest reference together with the canonical bytes. The bytes
// are returned rather than stored: they may carry sensitive source data, and
// retention is the caller's classification decision.
func (r *Registry) Compute(msg proto.Message, profileID string) (Reference, []byte, error) {
	key, err := r.LatestKey(profileID)
	if err != nil {
		return Reference{}, nil, err
	}
	return r.ComputeWith(msg, key, Options{})
}

// ComputeWith canonicalizes msg under an explicit profile version.
func (r *Registry) ComputeWith(msg proto.Message, key Key, opts Options) (Reference, []byte, error) {
	e, err := r.lookup(key)
	if err != nil {
		return Reference{}, nil, err
	}
	algID := opts.AlgorithmID
	if algID == "" {
		r.mu.RLock()
		algID = r.defaultAlgorithm
		r.mu.RUnlock()
	}
	alg, err := r.algorithm(algID)
	if err != nil {
		return Reference{}, nil, err
	}
	scope, err := resolveScope(msg, e.scope, opts.Scope)
	if err != nil {
		return Reference{}, nil, err
	}
	canonicalBytes, err := e.plan.Encode(msg)
	if err != nil {
		return Reference{}, nil, err
	}
	p := e.plan.Profile()
	materialRef := opts.MaterialProfileRef
	if materialRef == nil {
		materialRef = Ptr(key.String())
	}
	ref := Reference{
		ProfileID:                 p.ID,
		ProfileVersion:            p.Version,
		SchemaID:                  p.SchemaID,
		SchemaVersion:             p.SchemaVersion,
		AlgorithmID:               alg.ID,
		CanonicalLength:           uint64(len(canonicalBytes)),
		Digest:                    sum(alg, canonicalBytes),
		ScopeBindingDigest:        scopeDigest(alg, scope),
		CanonicalBytesArtifactRef: opts.CanonicalBytesArtifactRef,
		MaterialProfileRef:        materialRef,
	}
	if scope.IntentID != "" {
		ref.IntentID = Ptr(scope.IntentID)
	}
	if scope.ProposalRevisionID != "" {
		ref.ProposalRevisionID = Ptr(scope.ProposalRevisionID)
	}
	return ref, canonicalBytes, nil
}

// Explain reports the material paths that contributed to a digest under the
// latest published version of profileID.
func (r *Registry) Explain(msg proto.Message, profileID string) (canonical.Explanation, error) {
	key, err := r.LatestKey(profileID)
	if err != nil {
		return canonical.Explanation{}, err
	}
	e, err := r.lookup(key)
	if err != nil {
		return canonical.Explanation{}, err
	}
	x, _, err := e.plan.Explain(msg)
	return x, err
}

// Verify recomputes msg under the profile version and algorithm named in ref
// and compares. Because the reference names its own profile version, a
// reference minted under an older version keeps verifying after a newer version
// is published.
func (r *Registry) Verify(msg proto.Message, ref Reference) error {
	return r.VerifyWithScope(msg, ref, nil)
}

// VerifyWithScope verifies against an explicitly supplied scope. Use it when
// Compute was given an explicit scope because the canonical model does not
// carry every scope component.
func (r *Registry) VerifyWithScope(msg proto.Message, ref Reference, scope *Scope) error {
	if err := ref.validate(); err != nil {
		return err
	}
	e, err := r.lookup(ref.Key())
	if err != nil {
		return err
	}
	alg, err := r.algorithm(ref.AlgorithmID)
	if err != nil {
		return err
	}
	p := e.plan.Profile()
	if ref.SchemaID != p.SchemaID || ref.SchemaVersion != p.SchemaVersion {
		return newError("Verify", ErrProfileSubstitution,
			"reference claims schema %s v%d but %s publishes %s v%d",
			ref.SchemaID, ref.SchemaVersion, ref.Key(), p.SchemaID, p.SchemaVersion)
	}
	// Canonicalize first: a message that is not this profile's canonical model
	// at all should say so, rather than failing on a scope path that only
	// exists in some other model.
	canonicalBytes, err := e.plan.Encode(msg)
	if err != nil {
		return err
	}
	resolved, err := resolveScope(msg, e.scope, scope)
	if err != nil {
		return err
	}
	if want := scopeDigest(alg, resolved); want != ref.ScopeBindingDigest {
		return newError("Verify", ErrScopeMismatch,
			"reference is bound to a different tenant, intent or proposal revision")
	}
	if uint64(len(canonicalBytes)) != ref.CanonicalLength {
		return newError("Verify", ErrDigestMismatch,
			"canonical length %d, reference claims %d", len(canonicalBytes), ref.CanonicalLength)
	}
	got, err := hex.DecodeString(sum(alg, canonicalBytes))
	if err != nil {
		return newError("Verify", ErrDigestMismatch, "recomputed digest is unreadable")
	}
	want, err := hex.DecodeString(ref.Digest)
	if err != nil {
		return newError("Verify", ErrInvalidReference, "digest is not lowercase hex")
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return newError("Verify", ErrDigestMismatch, "under %s with %s", ref.Key(), alg.ID)
	}
	return nil
}

// VerifyAgainst verifies ref and additionally asserts that it was minted under
// the expected profile version. Presenting a reference for one profile where
// another is expected is profile substitution and fails before any
// canonicalization work is done.
func (r *Registry) VerifyAgainst(msg proto.Message, ref Reference, expected Key) error {
	if ref.Key() != expected {
		return newError("VerifyAgainst", ErrProfileSubstitution,
			"expected %s, reference is %s", expected, ref.Key())
	}
	return r.Verify(msg, ref)
}

// VerifyDual verifies every reference in a dual-digest set against the same
// source object. It is how an algorithm transition is carried: both the
// outgoing and incoming digests must verify for the whole set to pass.
func (r *Registry) VerifyDual(msg proto.Message, refs []Reference) error {
	if len(refs) == 0 {
		return newError("VerifyDual", ErrInvalidReference, "no references supplied")
	}
	for _, ref := range refs {
		if err := r.Verify(msg, ref); err != nil {
			return err
		}
	}
	return nil
}

func sum(alg Algorithm, b []byte) string {
	h := alg.New()
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// scopeDigest hashes the scope identity under a magic prefix and explicit
// length framing, so no two distinct scopes share bytes.
func scopeDigest(alg Algorithm, s Scope) string {
	var b []byte
	for _, part := range [...]string{scopeMagic, s.TenantID, s.IntentID, s.ProposalRevisionID} {
		b = binary.AppendUvarint(b, uint64(len(part)))
		b = append(b, part...)
	}
	return sum(alg, b)
}

// resolveScope reads the scope components out of the message unless the caller
// supplied them explicitly.
func resolveScope(msg proto.Message, spec ScopeSpec, override *Scope) (Scope, error) {
	if override != nil {
		return *override, nil
	}
	if msg == nil {
		return Scope{}, newError("scope", ErrInvalidReference, "nil message")
	}
	m := msg.ProtoReflect()
	var out Scope
	var err error
	if out.TenantID, err = stringAt(m, spec.TenantPath); err != nil {
		return Scope{}, err
	}
	if out.IntentID, err = stringAt(m, spec.IntentPath); err != nil {
		return Scope{}, err
	}
	if out.ProposalRevisionID, err = stringAt(m, spec.ProposalRevisionPath); err != nil {
		return Scope{}, err
	}
	return out, nil
}

func stringAt(m protoreflect.Message, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	cur := m
	segs := strings.Split(path, ".")
	for i, seg := range segs {
		fd := cur.Descriptor().Fields().ByName(protoreflect.Name(seg))
		if fd == nil {
			return "", newError("scope", ErrInvalidReference,
				"scope path %q: %s has no field %q", path, cur.Descriptor().FullName(), seg)
		}
		if i == len(segs)-1 {
			if fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() {
				return "", newError("scope", ErrInvalidReference,
					"scope path %q does not name a singular string field", path)
			}
			return cur.Get(fd).String(), nil
		}
		if fd.Message() == nil {
			return "", newError("scope", ErrInvalidReference,
				"scope path %q descends into a non-message field", path)
		}
		if !cur.Has(fd) {
			return "", nil
		}
		cur = cur.Get(fd).Message()
	}
	return "", nil
}
