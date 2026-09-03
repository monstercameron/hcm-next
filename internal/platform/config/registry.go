package config

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ObjectKind identifies the semantic owner of a registered configuration
// object. Objects are immutable; changing one means publishing another
// version.
type ObjectKind string

const (
	ObjectWorkflow  ObjectKind = "WORKFLOW"
	ObjectPolicy    ObjectKind = "POLICY"
	ObjectSchema    ObjectKind = "SCHEMA"
	ObjectRule      ObjectKind = "RULE"
	ObjectConnector ObjectKind = "CONNECTOR"
	ObjectAgent     ObjectKind = "AGENT"
	ObjectReference ObjectKind = "REFERENCE"
)

func (k ObjectKind) Valid() bool {
	switch k {
	case ObjectWorkflow, ObjectPolicy, ObjectSchema, ObjectRule, ObjectConnector, ObjectAgent, ObjectReference:
		return true
	default:
		return false
	}
}

// ConfigObject is one signed, effective-dated configuration definition.
// Content is treated as opaque canonical bytes by this package; its digest is
// nevertheless bound to all of the publication metadata and dependencies.
type ConfigObject struct {
	Kind ObjectKind
	// Type is a compatibility alias for Kind. If both are provided they must
	// agree.
	Type ObjectKind
	ID   string
	// Name is accepted as a readability alias for ID. At least one must be set;
	// when both are set they must agree.
	Name           string
	Version        string
	Owner          string
	Phase          string
	Scope          string
	EffectiveFrom  time.Time
	EffectiveUntil time.Time
	Content        []byte
	Dependencies   []Dependency

	Digest      string
	SignerKeyID string
	PublicKey   []byte
	Signature   []byte
}

// Object is a short compatibility name for ConfigObject.
type Object = ConfigObject

func (o ConfigObject) objectID() string {
	if o.ID != "" {
		return o.ID
	}
	return o.Name
}

func (o ConfigObject) objectKind() ObjectKind {
	if o.Kind != "" {
		return o.Kind
	}
	return o.Type
}

func (o ConfigObject) clone() ConfigObject {
	o.Content = append([]byte(nil), o.Content...)
	o.PublicKey = append([]byte(nil), o.PublicKey...)
	o.Signature = append([]byte(nil), o.Signature...)
	o.Dependencies = append([]Dependency(nil), o.Dependencies...)
	return o
}

func appendRegistryString(dst []byte, s string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(s)))
	return append(dst, s...)
}

func canonicalObjectBytes(o ConfigObject) ([]byte, error) {
	id := o.objectID()
	if o.ID != "" && o.Name != "" && o.ID != o.Name {
		return nil, errors.New("config: object id and name disagree")
	}
	deps := append([]Dependency(nil), o.Dependencies...)
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Kind != deps[j].Kind {
			return deps[i].Kind < deps[j].Kind
		}
		if deps[i].Name != deps[j].Name {
			return deps[i].Name < deps[j].Name
		}
		return deps[i].Version < deps[j].Version
	})
	b := []byte("hcmnext.config.object.v1")
	b = appendRegistryString(b, string(o.objectKind()))
	b = appendRegistryString(b, id)
	b = appendRegistryString(b, o.Version)
	b = appendRegistryString(b, o.Owner)
	b = appendRegistryString(b, o.Phase)
	b = appendRegistryString(b, o.Scope)
	b = appendRegistryString(b, o.SignerKeyID)
	b = binary.BigEndian.AppendUint64(b, uint64(o.EffectiveFrom.UnixNano()))
	b = binary.BigEndian.AppendUint64(b, uint64(o.EffectiveUntil.UnixNano()))
	b = binary.BigEndian.AppendUint32(b, uint32(len(o.Content)))
	b = append(b, o.Content...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(deps)))
	for _, d := range deps {
		b = append(b, byte(d.Kind))
		b = appendRegistryString(b, d.Name)
		b = appendRegistryString(b, d.Version)
		b = appendRegistryString(b, strings.ToLower(d.Digest))
	}
	return b, nil
}

// Digest computes the content address for o, excluding its detached signature.
func (o ConfigObject) DigestValue() (string, error) {
	if err := o.validate(false); err != nil {
		return "", err
	}
	b, err := canonicalObjectBytes(o)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (o ConfigObject) validate(signed bool) error {
	id := o.objectID()
	switch {
	case o.Kind != "" && o.Type != "" && o.Kind != o.Type:
		return newError("ConfigObject.validate", ErrInvalidObject, "kind and type disagree")
	case !o.objectKind().Valid():
		return newError("ConfigObject.validate", ErrInvalidObject, "unknown kind %q", o.objectKind())
	case id == "":
		return newError("ConfigObject.validate", ErrEmptyKey, "object id is empty")
	case o.Version == "":
		return newError("ConfigObject.validate", ErrMissingVersion, "%s has no version", id)
	case strings.TrimSpace(o.Owner) == "":
		return newError("ConfigObject.validate", ErrMissingOwner, "%s has no owner", id)
	case strings.TrimSpace(o.Phase) == "":
		return newError("ConfigObject.validate", ErrMissingPhase, "%s has no phase", id)
	case strings.TrimSpace(o.Scope) == "":
		return newError("ConfigObject.validate", ErrMissingScope, "%s has no scope", id)
	case o.EffectiveFrom.IsZero():
		return newError("ConfigObject.validate", ErrMissingEffectiveTime, "%s has no effective start", id)
	case !o.EffectiveUntil.IsZero() && !o.EffectiveUntil.After(o.EffectiveFrom):
		return newError("ConfigObject.validate", ErrInvalidEffectiveInterval, "%s has an invalid effective interval", id)
	}
	seen := map[string]bool{}
	for _, d := range o.Dependencies {
		if err := d.validate(); err != nil {
			return err
		}
		k := d.Kind.String() + "|" + d.Name
		if seen[k] {
			return newError("ConfigObject.validate", ErrDuplicateDependency, "%s has duplicate dependency %s", id, k)
		}
		seen[k] = true
	}
	if signed {
		if len(o.PublicKey) != ed25519.PublicKeySize || len(o.Signature) != ed25519.SignatureSize {
			return newError("ConfigObject.validate", ErrInvalidSignature, "%s has no valid detached signature", id)
		}
		if strings.TrimSpace(o.SignerKeyID) == "" {
			return newError("ConfigObject.validate", ErrEmptySigner, "%s has no signer key id", id)
		}
		if o.Digest == "" {
			return newError("ConfigObject.validate", ErrMissingDigest, "%s has no digest", id)
		}
	}
	return nil
}

// Sign returns a signed copy of o. The private key is never retained.
func (o ConfigObject) Sign(keyID string, priv ed25519.PrivateKey) (ConfigObject, error) {
	if keyID == "" {
		return ConfigObject{}, ErrEmptySigner
	}
	if len(priv) != ed25519.PrivateKeySize {
		return ConfigObject{}, ErrInvalidSignature
	}
	o = o.clone()
	o.SignerKeyID = keyID
	d, err := o.DigestValue()
	if err != nil {
		return ConfigObject{}, err
	}
	db, _ := hex.DecodeString(d)
	o.Digest, o.PublicKey = d, append([]byte(nil), priv.Public().(ed25519.PublicKey)...)
	o.Signature = ed25519.Sign(priv, db)
	return o, nil
}

// Verify checks metadata, content address and detached signature.
func (o ConfigObject) Verify() error {
	if err := o.validate(true); err != nil {
		return err
	}
	d, err := o.DigestValue()
	if err != nil {
		return err
	}
	if !strings.EqualFold(d, o.Digest) {
		return ErrTamperedManifest
	}
	db, _ := hex.DecodeString(o.Digest)
	if !ed25519.Verify(ed25519.PublicKey(o.PublicKey), db, o.Signature) {
		return ErrInvalidSignature
	}
	return nil
}

// Registry stores immutable configuration object versions and is safe for
// concurrent use. Register never replaces an existing kind/id/version key.
type Registry struct {
	mu      sync.RWMutex
	objects map[string]ConfigObject
}

func NewRegistry() *Registry { return &Registry{objects: make(map[string]ConfigObject)} }

func objectKey(o ConfigObject) string {
	return string(o.objectKind()) + "\x00" + o.objectID() + "\x00" + o.Version
}

func (r *Registry) Register(o ConfigObject) error {
	if err := o.Verify(); err != nil {
		return err
	}
	k := objectKey(o)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.objects[k]; exists {
		return ErrAlreadyRegistered
	}
	r.objects[k] = o.clone()
	return nil
}

func (r *Registry) Lookup(kind ObjectKind, id, version string) (ConfigObject, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.objects[string(kind)+"\x00"+id+"\x00"+version]
	if !ok {
		return ConfigObject{}, false
	}
	return o.clone(), true
}

func (r *Registry) Versions(kind ObjectKind, id string) []ConfigObject {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ConfigObject, 0)
	for _, o := range r.objects {
		if o.objectKind() == kind && o.objectID() == id {
			out = append(out, o.clone())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

func (r *Registry) List() []ConfigObject {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ConfigObject, 0, len(r.objects))
	for _, o := range r.objects {
		out = append(out, o.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].objectKind() != out[j].objectKind() {
			return out[i].objectKind() < out[j].objectKind()
		}
		if out[i].objectID() != out[j].objectID() {
			return out[i].objectID() < out[j].objectID()
		}
		return out[i].Version < out[j].Version
	})
	return out
}
