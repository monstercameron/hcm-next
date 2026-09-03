package config

import (
	"fmt"
	"sort"
	"strings"
)

// ObjectRef identifies one immutable registry object. Digest is mandatory:
// resolving by name and version alone would make a build depend on mutable
// registry state.
type ObjectRef struct {
	Kind    ObjectKind
	ID      string
	Version string
	Digest  string
}

// CompileOptions controls a hermetic bundle build. Roots are the authored
// entry points; every transitive dependency is discovered from registry
// objects and emitted into the resulting bundle.
type CompileOptions struct {
	BundleID              string
	Roots                 []ObjectRef
	TargetScope           string
	MinimumRuntimeVersion string
	CompatibilityRange    string
	Signer                string
	Provenance            string
	CredentialRefs        []string
}

func dependencyObjectKind(k DependencyKind) (ObjectKind, bool) {
	switch k {
	case DependencySchema:
		return ObjectSchema, true
	case DependencyRule:
		return ObjectRule, true
	case DependencyWorkflow:
		return ObjectWorkflow, true
	case DependencyMapping:
		return ObjectMapping, true
	case DependencyCapability:
		return ObjectCapability, true
	case DependencyConnector:
		return ObjectConnector, true
	case DependencyAgent:
		return ObjectAgent, true
	case DependencyReference:
		return ObjectReference, true
	case DependencyPolicy:
		return ObjectPolicy, true
	default:
		return "", false
	}
}

func objectDependency(o ConfigObject) (Dependency, error) {
	k := o.objectKind()
	var dk DependencyKind
	switch k {
	case ObjectSchema:
		dk = DependencySchema
	case ObjectRule:
		dk = DependencyRule
	case ObjectPolicy:
		dk = DependencyPolicy
	case ObjectWorkflow:
		dk = DependencyWorkflow
	case ObjectMapping:
		dk = DependencyMapping
	case ObjectCapability:
		dk = DependencyCapability
	case ObjectConnector:
		dk = DependencyConnector
	case ObjectAgent:
		dk = DependencyAgent
	case ObjectReference:
		dk = DependencyReference
	default:
		return Dependency{}, newError("Compile", ErrInvalidObject, "unsupported root kind %q", k)
	}
	d, err := o.DigestValue()
	if err != nil {
		return Dependency{}, err
	}
	return Dependency{Kind: dk, Name: o.objectID(), Version: o.Version, Digest: d}, nil
}

// Compile resolves and canonicalizes a complete dependency closure. It does
// no network or ambient-version lookup and never mutates the registry.
func Compile(reg *Registry, opts CompileOptions) (Bundle, error) {
	if reg == nil {
		return Bundle{}, newError("Compile", ErrUnresolvedDependency, "registry is nil")
	}
	if strings.TrimSpace(opts.TargetScope) == "" {
		return Bundle{}, newError("Compile", ErrMissingTargetScope, "")
	}
	if strings.TrimSpace(opts.MinimumRuntimeVersion) == "" {
		return Bundle{}, newError("Compile", ErrMissingRuntimeVersion, "")
	}
	if len(opts.Roots) == 0 {
		return Bundle{}, newError("Compile", ErrUnresolvedDependency, "no roots")
	}
	seen, visiting := map[string]bool{}, map[string]bool{}
	deps := make([]Dependency, 0)
	var visit func(ObjectRef) error
	visit = func(ref ObjectRef) error {
		if !ref.Kind.Valid() || ref.ID == "" || ref.Version == "" || ref.Digest == "" {
			return newError("Compile", ErrUnresolvedDependency, "invalid root/dependency reference")
		}
		key := string(ref.Kind) + "\x00" + ref.ID + "\x00" + ref.Version
		if visiting[key] {
			return newError("Compile", ErrDependencyCycle, "%s", ref.ID)
		}
		if seen[key] {
			return nil
		}
		o, ok := reg.Lookup(ref.Kind, ref.ID, ref.Version)
		if !ok {
			return newError("Compile", ErrUnresolvedDependency, "%s %s %s", ref.Kind, ref.ID, ref.Version)
		}
		digest, err := o.DigestValue()
		if err != nil {
			return err
		}
		if !strings.EqualFold(digest, ref.Digest) {
			return newError("Compile", ErrDependencyDigestMismatch, "%s %s %s", ref.Kind, ref.ID, ref.Version)
		}
		if o.Scope != opts.TargetScope {
			return newError("Compile", ErrScopeMismatch, "%s", ref.ID)
		}
		visiting[key] = true
		for _, d := range o.Dependencies {
			okind, ok := dependencyObjectKind(d.Kind)
			if !ok {
				return newError("Compile", ErrUnresolvedDependency, "%s", d.Name)
			}
			if err := visit(ObjectRef{Kind: okind, ID: d.Name, Version: d.Version, Digest: d.Digest}); err != nil {
				return err
			}
		}
		delete(visiting, key)
		seen[key] = true
		od, err := objectDependency(o)
		if err != nil {
			return err
		}
		deps = append(deps, od)
		return nil
	}
	roots := append([]ObjectRef(nil), opts.Roots...)
	sort.Slice(roots, func(i, j int) bool {
		a, b := roots[i], roots[j]
		return fmt.Sprintf("%s|%s|%s", a.Kind, a.ID, a.Version) < fmt.Sprintf("%s|%s|%s", b.Kind, b.ID, b.Version)
	})
	for _, root := range roots {
		if err := visit(root); err != nil {
			return Bundle{}, err
		}
	}
	b := Bundle{BundleID: opts.BundleID, ManifestVersion: 1, Dependencies: deps, CompatibilityRange: opts.CompatibilityRange, Signer: opts.Signer, Provenance: opts.Provenance, CredentialRefs: append([]string(nil), opts.CredentialRefs...), TargetScope: opts.TargetScope, MinimumRuntimeVersion: opts.MinimumRuntimeVersion}
	if _, err := BundleDigest(b); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

// CompileBundle is the descriptive alias used by publication callers.
var CompileBundle = Compile
