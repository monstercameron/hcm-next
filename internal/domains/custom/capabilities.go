package custom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/customobject"
)

// Capability is the governed contract for one operation on one custom type.
// It is intentionally descriptive: clients can only invoke a capability
// generated here, never a generic table or unrestricted CRUD endpoint.
type Capability struct {
	Name             string
	Kind             string
	Namespace        string
	Operation        Operation
	Version          uint64
	AuthZDomains     []string
	Purpose          string
	SideEffects      []string
	InputSchema      string
	OutputSchema     string
	EvidenceRequired bool
	Digest           string
}

// CapabilityManifest is the stable, transport-neutral generated surface.
type CapabilityManifest struct {
	Kind          string
	Namespace     string
	SchemaVersion uint64
	Capabilities  []Capability
	Digest        string
}

var (
	ErrCapabilityInvalid = errors.New("custom: invalid capability")
	ErrCapabilityDenied  = errors.New("custom: capability denied")
	ErrCapabilityParity  = errors.New("custom: capability parity mismatch")
)

// GenerateCapabilities builds the same manifest used by the native and
// grpcbridge clients. Every operation carries the complete field-domain set,
// declared purpose, schemas, side effects, and an evidence requirement.
func GenerateCapabilities(def CustomObjectDefinition, purpose string) (CapabilityManifest, error) {
	if err := def.Validate(); err != nil {
		return CapabilityManifest{}, err
	}
	if strings.TrimSpace(purpose) == "" || strings.TrimSpace(purpose) != purpose {
		return CapabilityManifest{}, fmt.Errorf("%w: purpose is required", ErrCapabilityInvalid)
	}
	policy, err := ResolvePolicy(def)
	if err != nil {
		return CapabilityManifest{}, err
	}
	domains := make([]string, 0, len(policy.Fields))
	seen := make(map[string]struct{})
	for _, field := range policy.Fields {
		if _, ok := seen[field.AuthZDomain]; ok {
			continue
		}
		seen[field.AuthZDomain] = struct{}{}
		domains = append(domains, field.AuthZDomain)
	}
	sort.Strings(domains)
	compiled, err := compileDomainSchema(def)
	if err != nil {
		return CapabilityManifest{}, err
	}
	operations := []Operation{OperationCreate, OperationRead, OperationChange, OperationCorrect, OperationRetire}
	capabilities := make([]Capability, 0, len(operations))
	for _, operation := range operations {
		name := fmt.Sprintf("custom.%s.%s.%s/v%d", def.Namespace, strings.ToLower(def.Kind), strings.ToLower(string(operation)), def.Version)
		capability := Capability{
			Name: name, Kind: def.Kind, Namespace: def.Namespace, Operation: operation, Version: def.Version,
			AuthZDomains: append([]string(nil), domains...), Purpose: purpose,
			SideEffects: sideEffects(operation), InputSchema: compiled.Digest, OutputSchema: compiled.Digest,
			EvidenceRequired: true,
		}
		capability.Digest = digestCapability(capability)
		capabilities = append(capabilities, capability)
	}
	manifest := CapabilityManifest{Kind: def.Kind, Namespace: def.Namespace, SchemaVersion: def.Version, Capabilities: capabilities}
	manifest.Digest = digestManifest(manifest)
	return cloneManifest(manifest), nil
}

// CapabilityClient is a generated client over an injected invocation port.
// Its methods preserve the governed capability name and reject missing
// evidence or purpose before the transport is called.
type CapabilityClient struct {
	manifest CapabilityManifest
	invoke   func(context.Context, Capability, Invocation) (CommitReceipt, error)
}

// Invocation is the transport-neutral request body shared by native and gRPC
// clients. The service remains responsible for validating the mutation.
type Invocation struct {
	Operation      Operation
	ObjectID       string
	EvidenceDigest string
}

// NewCapabilityClient creates a client for a generated manifest.
func NewCapabilityClient(manifest CapabilityManifest, invoke func(context.Context, Capability, Invocation) (CommitReceipt, error)) (*CapabilityClient, error) {
	if invoke == nil || manifest.Digest == "" || len(manifest.Capabilities) != 5 {
		return nil, fmt.Errorf("%w: complete manifest and invoker are required", ErrCapabilityInvalid)
	}
	return &CapabilityClient{manifest: cloneManifest(manifest), invoke: invoke}, nil
}

func (c *CapabilityClient) Invoke(ctx context.Context, operation Operation, objectID, evidenceDigest string) (CommitReceipt, error) {
	if c == nil || c.invoke == nil {
		return CommitReceipt{}, fmt.Errorf("%w: nil client", ErrCapabilityInvalid)
	}
	if objectID == "" || evidenceDigest == "" {
		return CommitReceipt{}, fmt.Errorf("%w: object and evidence are required", ErrCapabilityDenied)
	}
	for _, capability := range c.manifest.Capabilities {
		if capability.Operation == operation {
			return c.invoke(ctx, capability, Invocation{Operation: operation, ObjectID: objectID, EvidenceDigest: evidenceDigest})
		}
	}
	return CommitReceipt{}, fmt.Errorf("%w: operation %s is not generated", ErrCapabilityDenied, operation)
}

func (c *CapabilityClient) Create(ctx context.Context, objectID, evidenceDigest string) (CommitReceipt, error) {
	return c.Invoke(ctx, OperationCreate, objectID, evidenceDigest)
}
func (c *CapabilityClient) Read(ctx context.Context, objectID, evidenceDigest string) (CommitReceipt, error) {
	return c.Invoke(ctx, OperationRead, objectID, evidenceDigest)
}
func (c *CapabilityClient) Change(ctx context.Context, objectID, evidenceDigest string) (CommitReceipt, error) {
	return c.Invoke(ctx, OperationChange, objectID, evidenceDigest)
}
func (c *CapabilityClient) Correct(ctx context.Context, objectID, evidenceDigest string) (CommitReceipt, error) {
	return c.Invoke(ctx, OperationCorrect, objectID, evidenceDigest)
}
func (c *CapabilityClient) Retire(ctx context.Context, objectID, evidenceDigest string) (CommitReceipt, error) {
	return c.Invoke(ctx, OperationRetire, objectID, evidenceDigest)
}

// GRPCCapabilityClient is the grpcbridge-facing equivalent. Keeping the same
// manifest value makes parity a data check instead of duplicated policy code.
type GRPCCapabilityClient struct{ *CapabilityClient }

func NewGRPCCapabilityClient(manifest CapabilityManifest, invoke func(context.Context, Capability, Invocation) (CommitReceipt, error)) (*GRPCCapabilityClient, error) {
	client, err := NewCapabilityClient(manifest, invoke)
	if err != nil {
		return nil, err
	}
	return &GRPCCapabilityClient{CapabilityClient: client}, nil
}

// CheckClientParity ensures native and grpcbridge generated surfaces cannot
// drift in operation names, policy fields, schemas, or digests.
func CheckClientParity(native, grpc CapabilityManifest) error {
	if native.Digest == "" || native.Digest != grpc.Digest || native.Kind != grpc.Kind || native.Namespace != grpc.Namespace || native.SchemaVersion != grpc.SchemaVersion {
		return fmt.Errorf("%w: manifest metadata differs", ErrCapabilityParity)
	}
	if len(native.Capabilities) != len(grpc.Capabilities) {
		return fmt.Errorf("%w: capability count differs", ErrCapabilityParity)
	}
	for i := range native.Capabilities {
		if native.Capabilities[i].Digest != grpc.Capabilities[i].Digest || native.Capabilities[i].Name != grpc.Capabilities[i].Name {
			return fmt.Errorf("%w: capability %d differs", ErrCapabilityParity, i)
		}
	}
	return nil
}

func sideEffects(operation Operation) []string {
	if operation == OperationRead {
		return []string{"READ_PROJECTION"}
	}
	return []string{"APPEND_LEDGER_EVENT", "ADVANCE_PROJECTION", "ENQUEUE_OUTBOX"}
}

func compileDomainSchema(def CustomObjectDefinition) (customobject.CompiledSchema, error) {
	fields := make([]customobject.Field, 0, len(def.Fields))
	for name, field := range def.Fields {
		fields = append(fields, customobject.Field{Name: name, Type: field.Type})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	return customobject.Compile(customobject.CustomObjectType{Name: def.Kind, Namespace: def.Namespace, Owner: "custom-domain", Version: uint32(def.Version), Fields: fields})
}

func digestCapability(c Capability) string {
	parts := []string{c.Name, c.Kind, c.Namespace, string(c.Operation), fmt.Sprint(c.Version), c.Purpose, c.InputSchema, c.OutputSchema, fmt.Sprint(c.EvidenceRequired)}
	parts = append(parts, c.AuthZDomains...)
	parts = append(parts, c.SideEffects...)
	return digestStrings(parts)
}
func digestManifest(m CapabilityManifest) string {
	parts := []string{m.Kind, m.Namespace, fmt.Sprint(m.SchemaVersion)}
	for _, c := range m.Capabilities {
		parts = append(parts, c.Digest)
	}
	return digestStrings(parts)
}
func digestStrings(values []string) string {
	h := sha256.New()
	for _, value := range values {
		fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
func cloneManifest(in CapabilityManifest) CapabilityManifest {
	in.Capabilities = append([]Capability(nil), in.Capabilities...)
	for i := range in.Capabilities {
		in.Capabilities[i].AuthZDomains = append([]string(nil), in.Capabilities[i].AuthZDomains...)
		in.Capabilities[i].SideEffects = append([]string(nil), in.Capabilities[i].SideEffects...)
	}
	return in
}
