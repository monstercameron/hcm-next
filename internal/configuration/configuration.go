// Package configuration owns the configuration-semantics contracts
// (ARCH-GO-024): definition, snapshot, bundle, dependency, diff,
// validation, approval, activation, rollback and registry. Authored
// definitions are inputs; snapshots and bundles are immutable
// content-addressed values; activation and rollback bind an exact bundle
// digest to a sealed approval so customer configuration can never bypass
// publication and approval. Configuration selects behaviour: this package
// performs no domain transaction, owns no persistence and depends on no
// domain, workflow or store package.
package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Definition references one authored input (a file beneath definitions/).
type Definition struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Snapshot is one immutable content-addressed definition capture.
type Snapshot struct {
	Definition Definition `json:"definition"`
	Digest     string     `json:"digest"`
}

// Dependency pins one prerequisite bundle digest.
type Dependency struct {
	Bundle string `json:"bundle"`
}

// Bundle is one immutable ordered set of snapshots plus dependencies.
type Bundle struct {
	Name      string       `json:"name"`
	Snapshots []Snapshot   `json:"snapshots"`
	Deps      []Dependency `json:"deps"`
	Digest    string       `json:"digest"`
}

// Approval seals one bundle digest to one approver and expiry.
type Approval struct {
	Bundle   string `json:"bundle"`
	Approver string `json:"approver"`
	Expiry   string `json:"expiry"`
	Seal     string `json:"seal"`
}

// ActivationRecord binds one activation or rollback to its bundle digest.
type ActivationRecord struct {
	Bundle   string   `json:"bundle"`
	Approval Approval `json:"approval"`
	Rollback bool     `json:"rollback"`
}

// Change is one deterministic bundle diff entry.
type Change string

// Finding is one exact bundle validation failure.
type Finding struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

func (f Finding) String() string { return f.Code + "|" + f.Field + "|" + f.Detail }

// Finding codes.
const (
	UnknownBundle         = "UNKNOWN_BUNDLE"
	UnresolvedDependency  = "UNRESOLVED_DEPENDENCY"
	MissingApproval       = "MISSING_APPROVAL"
	ForeignApproval       = "FOREIGN_APPROVAL"
	BrokenSeal            = "BROKEN_SEAL"
	UnknownActivation     = "UNKNOWN_ACTIVATION"
	UnknownRollbackTarget = "UNKNOWN_ROLLBACK_TARGET"
)

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// SnapshotDefinition captures one authored definition immutably.
func SnapshotDefinition(def Definition, content []byte) (Snapshot, error) {
	if strings.TrimSpace(def.Kind) == "" || strings.TrimSpace(def.Name) == "" {
		return Snapshot{}, fmt.Errorf("definition kind and name are required")
	}
	if len(content) == 0 {
		return Snapshot{}, fmt.Errorf("definition content is required")
	}
	return Snapshot{
		Definition: def,
		Digest:     digest("snapshot", def.Kind, def.Name, string(content)),
	}, nil
}

// Registry holds bundles, approvals and the activation log.
type Registry struct {
	bundles map[string]Bundle
	log     []ActivationRecord
}

// NewRegistry returns an empty registry.
func NewRegistry() Registry {
	return Registry{bundles: make(map[string]Bundle)}
}

// Assemble freezes one bundle. Every dependency digest must already
// resolve in the registry; rollout targets these immutable digests.
func (r *Registry) Assemble(name string, snaps []Snapshot, deps []Dependency) (Bundle, error) {
	if strings.TrimSpace(name) == "" {
		return Bundle{}, fmt.Errorf("bundle name is required")
	}
	if len(snaps) == 0 {
		return Bundle{}, fmt.Errorf("at least one snapshot is required")
	}
	for _, dep := range deps {
		if _, ok := r.bundles[dep.Bundle]; !ok {
			return Bundle{}, fmt.Errorf("unresolved dependency %q", dep.Bundle)
		}
	}
	ordered := append([]Snapshot(nil), snaps...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Definition.Kind != ordered[j].Definition.Kind {
			return ordered[i].Definition.Kind < ordered[j].Definition.Kind
		}
		return ordered[i].Definition.Name < ordered[j].Definition.Name
	})
	parts := []string{"bundle", name}
	for _, snap := range ordered {
		parts = append(parts, snap.Digest)
	}
	depDigests := make([]string, 0, len(deps))
	for _, dep := range deps {
		depDigests = append(depDigests, dep.Bundle)
	}
	sort.Strings(depDigests)
	parts = append(parts, depDigests...)
	bundle := Bundle{Name: name, Snapshots: ordered, Deps: deps, Digest: digest(parts...)}
	r.bundles[bundle.Digest] = bundle
	return bundle, nil
}

// Approve seals one known bundle to one approver and RFC3339 expiry:
// the publication-and-approval gate customer configuration must pass.
func (r *Registry) Approve(bundleDigest, approver, expiry string) (Approval, error) {
	if _, ok := r.bundles[bundleDigest]; !ok {
		return Approval{}, fmt.Errorf("cannot approve unknown bundle %q", bundleDigest)
	}
	if strings.TrimSpace(approver) == "" {
		return Approval{}, fmt.Errorf("approver is required")
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(expiry)); err != nil {
		return Approval{}, fmt.Errorf("expiry %q is not RFC3339", expiry)
	}
	return Approval{
		Bundle:   bundleDigest,
		Approver: approver,
		Expiry:   expiry,
		Seal:     digest("approval", bundleDigest, approver, expiry),
	}, nil
}

func validSeal(approval Approval) bool {
	if strings.TrimSpace(approval.Approver) == "" {
		return false
	}
	return approval.Seal == digest("approval", approval.Bundle, approval.Approver, approval.Expiry)
}

// Activate binds one exact bundle digest to its sealed approval and logs
// the activation. Anything without a matching sealed approval fails.
func (r *Registry) Activate(bundleDigest string, approval Approval) (ActivationRecord, error) {
	if _, ok := r.bundles[bundleDigest]; !ok {
		return ActivationRecord{}, fmt.Errorf("cannot activate unknown bundle %q", bundleDigest)
	}
	if approval.Bundle == "" || strings.TrimSpace(approval.Approver) == "" {
		return ActivationRecord{}, fmt.Errorf("activation requires a sealed approval")
	}
	if approval.Bundle != bundleDigest {
		return ActivationRecord{}, fmt.Errorf("approval seals %q, not %q", approval.Bundle, bundleDigest)
	}
	if !validSeal(approval) {
		return ActivationRecord{}, fmt.Errorf("approval seal is broken for %q", bundleDigest)
	}
	record := ActivationRecord{Bundle: bundleDigest, Approval: approval}
	r.log = append(r.log, record)
	return record, nil
}

// Rollback binds one prior bundle digest to its sealed approval and logs
// the rollback activation. Both digests must resolve; the approval must
// seal the rollback target.
func (r *Registry) Rollback(from, to string, approval Approval) (ActivationRecord, error) {
	if _, ok := r.bundles[from]; !ok {
		return ActivationRecord{}, fmt.Errorf("cannot roll back unknown bundle %q", from)
	}
	if _, ok := r.bundles[to]; !ok {
		return ActivationRecord{}, fmt.Errorf("cannot roll back to unknown bundle %q", to)
	}
	record, err := r.Activate(to, approval)
	if err != nil {
		return ActivationRecord{}, err
	}
	record.Rollback = true
	r.log[len(r.log)-1] = record
	return record, nil
}

// Validate reports exact findings for one bundle digest.
func (r *Registry) Validate(bundleDigest string) []Finding {
	bundle, ok := r.bundles[bundleDigest]
	if !ok {
		return []Finding{{Code: UnknownBundle, Field: "bundle", Detail: fmt.Sprintf("unknown bundle %q", bundleDigest)}}
	}
	var findings []Finding
	for i, dep := range bundle.Deps {
		if _, ok := r.bundles[dep.Bundle]; !ok {
			findings = append(findings, Finding{Code: UnresolvedDependency, Field: fmt.Sprintf("deps[%d]", i), Detail: fmt.Sprintf("unresolved dependency %q", dep.Bundle)})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings
}

// Diff compares two known bundles snapshot by snapshot, deterministically.
func (r *Registry) Diff(before, after string) []Change {
	oldBundle, ok := r.bundles[before]
	if !ok {
		return []Change{Change(fmt.Sprintf("unknown bundle %q", before))}
	}
	newBundle, ok := r.bundles[after]
	if !ok {
		return []Change{Change(fmt.Sprintf("unknown bundle %q", after))}
	}
	oldSnaps := make(map[string]string, len(oldBundle.Snapshots))
	for _, snap := range oldBundle.Snapshots {
		oldSnaps[snap.Definition.Kind+"/"+snap.Definition.Name] = snap.Digest
	}
	var changes []Change
	seen := make(map[string]bool, len(newBundle.Snapshots))
	for _, snap := range newBundle.Snapshots {
		key := snap.Definition.Kind + "/" + snap.Definition.Name
		seen[key] = true
		oldDigest, existed := oldSnaps[key]
		switch {
		case !existed:
			changes = append(changes, Change("added "+key))
		case oldDigest != snap.Digest:
			changes = append(changes, Change("changed "+key))
		}
	}
	for _, snap := range oldBundle.Snapshots {
		key := snap.Definition.Kind + "/" + snap.Definition.Name
		if !seen[key] {
			changes = append(changes, Change("removed "+key))
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i] < changes[j] })
	return changes
}
