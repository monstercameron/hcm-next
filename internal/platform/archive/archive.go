// Package archive preserves executable historical contracts as immutable,
// offline-verifiable packages. It is never an activation or effect source.
package archive

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func Version() int { return 1 }

var (
	ErrInvalidArchive = errors.New("archive: invalid archive")
	ErrTampered       = errors.New("archive: tampered archive")
	ErrUnsupported    = errors.New("archive: unsupported historical contract")
	ErrImmutable      = errors.New("archive: immutable package cannot be replaced")
)

type Source struct {
	Name    string
	Kind    string
	Version string
	Bytes   []byte
	Digest  string
}
type ToolProfile struct {
	Name           string
	Version        string
	CompilerDigest string
}
type Compatibility struct {
	Runtime string
	Schema  string
	Mapping string
}

type BuildRequest struct {
	ArchiveID     string
	ContractID    string
	Revision      string
	Sources       []Source
	Tools         []ToolProfile
	Compatibility Compatibility
	SignerKeyID   string
}

type Package struct {
	ArchiveID       string
	ContractID      string
	Revision        string
	Sources         []Source
	Tools           []ToolProfile
	Compatibility   Compatibility
	Digest          string
	SignerKeyID     string
	SignerPublicKey []byte
	Signature       []byte
}

type Evidence struct {
	ContractID     string
	Revision       string
	ContractDigest string
	ToolVersion    string
	Bytes          []byte
}
type ReplayStatus string

const (
	Replayable        ReplayStatus = "REPLAYED"
	ReplayTampered    ReplayStatus = "TAMPERED"
	ReplayUnsupported ReplayStatus = "UNSUPPORTED"
)

type ReplayResult struct {
	Status               ReplayStatus
	ArchiveDigest        string
	InterpretationDigest string
	Reason               string
}

func Build(req BuildRequest, privateKey ed25519.PrivateKey) (Package, error) {
	p := Package{ArchiveID: req.ArchiveID, ContractID: req.ContractID, Revision: req.Revision, Sources: cloneSources(req.Sources), Tools: append([]ToolProfile(nil), req.Tools...), Compatibility: req.Compatibility, SignerKeyID: req.SignerKeyID}
	if err := p.validateShape(); err != nil || len(privateKey) != ed25519.PrivateKeySize {
		if err != nil {
			return Package{}, err
		}
		return Package{}, ErrInvalidArchive
	}
	p.Digest = digest(archiveBody(p))
	p.SignerPublicKey = append([]byte(nil), privateKey.Public().(ed25519.PublicKey)...)
	p.Signature = ed25519.Sign(privateKey, []byte(p.Digest))
	return clonePackage(p), nil
}

func (p Package) VerifyOffline() error {
	if err := p.validateShape(); err != nil {
		return err
	}
	if digest(archiveBody(p)) != p.Digest || len(p.SignerPublicKey) != ed25519.PublicKeySize || len(p.Signature) != ed25519.SignatureSize || !ed25519.Verify(p.SignerPublicKey, []byte(p.Digest), p.Signature) {
		return ErrTampered
	}
	return nil
}

// Replay validates only archived bytes and pinned compatibility metadata. It
// deliberately has no callback or network/tool lookup, so a successful replay
// cannot execute an external effect.
func (p Package) Replay(e Evidence) (ReplayResult, error) {
	result := ReplayResult{Status: ReplayTampered, ArchiveDigest: p.Digest}
	if err := p.VerifyOffline(); err != nil {
		result.Reason = err.Error()
		return result, err
	}
	if e.ContractID != p.ContractID || e.Revision != p.Revision || e.ContractDigest == "" {
		result.Status = ReplayUnsupported
		result.Reason = "evidence does not identify the archived contract"
		return result, ErrUnsupported
	}
	var source Source
	for _, candidate := range p.Sources {
		if candidate.Kind == "contract" && candidate.Name == e.ContractID {
			source = candidate
			break
		}
	}
	if source.Digest == "" || e.ContractDigest != source.Digest || digest(e.Bytes) != e.ContractDigest {
		result.Reason = "historical contract bytes do not match the content address"
		return result, ErrTampered
	}
	knownTool := false
	for _, tool := range p.Tools {
		if tool.Version == e.ToolVersion {
			knownTool = true
			break
		}
	}
	if !knownTool {
		result.Status = ReplayUnsupported
		result.Reason = "tool version is outside the archived compatibility profile"
		return result, ErrUnsupported
	}
	result.Status = Replayable
	result.InterpretationDigest = digest(append(append([]byte(nil), source.Bytes...), e.Bytes...))
	result.Reason = "historical contract interpreted from archived bytes"
	return result, nil
}

func (r ReplayResult) Explain() string {
	return fmt.Sprintf("archive replay status=%s archive=%s interpretation=%s", r.Status, r.ArchiveDigest, r.InterpretationDigest)
}
func Explain(v any) string {
	switch value := v.(type) {
	case Package:
		return fmt.Sprintf("archive package=%s contract=%s digest=%s", value.ArchiveID, value.ContractID, value.Digest)
	case ReplayResult:
		return value.Explain()
	default:
		return "archive explanation unavailable"
	}
}

type Repository struct{ packages map[string]Package }

func NewRepository() *Repository { return &Repository{packages: make(map[string]Package)} }
func (r *Repository) Put(p Package) error {
	if r == nil {
		return ErrInvalidArchive
	}
	if err := p.VerifyOffline(); err != nil {
		return err
	}
	if old, ok := r.packages[p.ArchiveID]; ok && old.Digest != p.Digest {
		return ErrImmutable
	}
	r.packages[p.ArchiveID] = clonePackage(p)
	return nil
}
func (r *Repository) Get(id string) (Package, bool) {
	if r == nil {
		return Package{}, false
	}
	p, ok := r.packages[id]
	return clonePackage(p), ok
}

func (p Package) validateShape() error {
	if strings.TrimSpace(p.ArchiveID) == "" || strings.TrimSpace(p.ContractID) == "" || strings.TrimSpace(p.Revision) == "" || strings.TrimSpace(p.SignerKeyID) == "" || len(p.Sources) == 0 || len(p.Tools) == 0 || strings.TrimSpace(p.Compatibility.Runtime) == "" || strings.TrimSpace(p.Compatibility.Schema) == "" || strings.TrimSpace(p.Compatibility.Mapping) == "" {
		return ErrInvalidArchive
	}
	seen := make(map[string]bool, len(p.Sources))
	contract := false
	for i := range p.Sources {
		source := &p.Sources[i]
		if source.Name == "" || source.Kind == "" || source.Version == "" || len(source.Bytes) == 0 || source.Digest == "" || source.Digest != digest(source.Bytes) || seen[source.Name] {
			return ErrInvalidArchive
		}
		seen[source.Name] = true
		contract = contract || (source.Kind == "contract" && source.Name == p.ContractID)
	}
	if !contract {
		return ErrInvalidArchive
	}
	for _, tool := range p.Tools {
		if tool.Name == "" || tool.Version == "" || tool.CompilerDigest == "" {
			return ErrInvalidArchive
		}
	}
	return nil
}

type canonicalPackage struct {
	ArchiveID     string        `json:"archive_id"`
	ContractID    string        `json:"contract_id"`
	Revision      string        `json:"revision"`
	Sources       []Source      `json:"sources"`
	Tools         []ToolProfile `json:"tools"`
	Compatibility Compatibility `json:"compatibility"`
	SignerKeyID   string        `json:"signer_key_id"`
}

func archiveBody(p Package) []byte {
	sources := cloneSources(p.Sources)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Name < sources[j].Name })
	tools := append([]ToolProfile(nil), p.Tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Version < tools[j].Version })
	b, _ := json.Marshal(canonicalPackage{p.ArchiveID, p.ContractID, p.Revision, sources, tools, p.Compatibility, p.SignerKeyID})
	return b
}
func cloneSources(in []Source) []Source {
	out := make([]Source, len(in))
	for i, source := range in {
		out[i] = source
		out[i].Bytes = append([]byte(nil), source.Bytes...)
	}
	return out
}
func clonePackage(p Package) Package {
	p.Sources = cloneSources(p.Sources)
	p.Tools = append([]ToolProfile(nil), p.Tools...)
	p.SignerPublicKey = append([]byte(nil), p.SignerPublicKey...)
	p.Signature = append([]byte(nil), p.Signature...)
	return p
}
func digest(b []byte) string { sum := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(sum[:]) }
