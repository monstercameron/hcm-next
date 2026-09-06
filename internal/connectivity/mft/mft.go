// Package mft defines the pure, provider-independent contract for managed
// file transfer. The memory port is deliberately the only persistence
// implementation here; adapters may persist these immutable records later.
package mft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

type Direction string

const (
	DirectionPickup Direction = "PICKUP"
	DirectionDrop   Direction = "DROP"
	Pickup          Direction = DirectionPickup
	Drop            Direction = DirectionDrop
)

type State string

const (
	StateDeclared     State = "DECLARED"
	StateStaged       State = "STAGED"
	StateVerified     State = "VERIFIED"
	StateDelivered    State = "DELIVERED"
	StateAcknowledged State = "ACKNOWLEDGED"
	StateFailed       State = "FAILED"
	StateQuarantined  State = "QUARANTINED"

	Declared     State = StateDeclared
	Staged       State = StateStaged
	Verified     State = StateVerified
	Delivered    State = StateDelivered
	Acknowledged State = StateAcknowledged
	Failed       State = StateFailed
	Quarantined  State = StateQuarantined
)

var (
	ErrInvalidDefinition = errors.New("mft: invalid transfer definition")
	ErrInvalidTransition = errors.New("mft: invalid lifecycle transition")
	ErrManifestMismatch  = errors.New("mft: manifest does not match expectation")
	ErrAlreadyExists     = errors.New("mft: transfer already exists")
	ErrNotFound          = errors.New("mft: transfer not found")
	ErrReceiptMismatch   = errors.New("mft: receipt does not match transfer")
	ErrTamperedEvent     = errors.New("mft: event chain is tampered")
)

// ManifestFile is an immutable file claim. Content is used only while a
// manifest is being verified and is never included in a manifest digest.
type ManifestFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Content []byte `json:"-"`
}

// Manifest is the sorted, content-addressed description of a transfer.
type Manifest struct {
	Files  []ManifestFile `json:"files"`
	Digest string         `json:"digest"`
}

// ManifestExpectation permits either an exact digest/file set or bounded
// manifests. Zero bounds mean no bound.
type ManifestExpectation struct {
	Digest       string         `json:"digest,omitempty"`
	Files        []ManifestFile `json:"files,omitempty"`
	Count        int            `json:"count,omitempty"`
	MinFileSize  int64          `json:"min_file_size,omitempty"`
	MaxFileSize  int64          `json:"max_file_size,omitempty"`
	MinTotalSize int64          `json:"min_total_size,omitempty"`
	MaxTotalSize int64          `json:"max_total_size,omitempty"`
}

// TransferDefinition contains only references to external endpoint and
// schedule records. Credentials and provider clients are intentionally absent.
type TransferDefinition struct {
	ID               string              `json:"id"`
	Direction        Direction           `json:"direction"`
	EndpointRef      string              `json:"endpoint_ref"`
	ScheduleRef      string              `json:"schedule_ref"`
	FilePattern      string              `json:"file_pattern"`
	ExpectedManifest ManifestExpectation `json:"expected_manifest"`
	EncryptionPolicy string              `json:"encryption_policy"`
	IntegrityPolicy  string              `json:"integrity_policy"`
}

func (d TransferDefinition) Validate() error {
	if d.ID == "" || d.EndpointRef == "" || d.ScheduleRef == "" || d.FilePattern == "" || d.EncryptionPolicy == "" || d.IntegrityPolicy == "" {
		return fmt.Errorf("%w: id, endpoint, schedule, pattern, encryption and integrity are required", ErrInvalidDefinition)
	}
	if d.Direction != DirectionPickup && d.Direction != DirectionDrop {
		return fmt.Errorf("%w: direction %q", ErrInvalidDefinition, d.Direction)
	}
	if _, err := path.Match(d.FilePattern, "probe"); err != nil {
		return fmt.Errorf("%w: file pattern: %v", ErrInvalidDefinition, err)
	}
	e := d.ExpectedManifest
	if e.Count < 0 || e.MinFileSize < 0 || e.MaxFileSize < 0 || e.MinTotalSize < 0 || e.MaxTotalSize < 0 || (e.MaxFileSize > 0 && e.MinFileSize > e.MaxFileSize) || (e.MaxTotalSize > 0 && e.MinTotalSize > e.MaxTotalSize) {
		return fmt.Errorf("%w: invalid manifest bounds", ErrInvalidDefinition)
	}
	return nil
}

// NewManifest derives all file claims and its digest from the supplied bytes.
func NewManifest(files []ManifestFile) (Manifest, error) {
	copyFiles := cloneFiles(files)
	for i := range copyFiles {
		if err := validatePath(copyFiles[i].Path); err != nil {
			return Manifest{}, err
		}
		copyFiles[i].Size = int64(len(copyFiles[i].Content))
		copyFiles[i].SHA256 = sha256Hex(copyFiles[i].Content)
	}
	return manifestWithDigest(copyFiles)
}

func (m Manifest) ContentDigest() string {
	copyFiles := cloneFiles(m.Files)
	for i := range copyFiles {
		copyFiles[i].Content = nil
	}
	canonical, _ := canonicalManifest(copyFiles)
	return hashBytes(canonical)
}

func (m Manifest) Validate() error {
	if len(m.Files) == 0 {
		return fmt.Errorf("%w: empty manifest", ErrManifestMismatch)
	}
	seen := make(map[string]struct{}, len(m.Files))
	for _, file := range m.Files {
		if err := validateFileClaim(file); err != nil {
			return err
		}
		if _, ok := seen[file.Path]; ok {
			return fmt.Errorf("%w: duplicate file %s", ErrManifestMismatch, file.Path)
		}
		seen[file.Path] = struct{}{}
	}
	if m.Digest == "" || m.Digest != m.ContentDigest() {
		return fmt.Errorf("%w: manifest digest", ErrManifestMismatch)
	}
	return nil
}

// VerifyManifest checks file hashes, duplicate/path claims, count and all
// configured size bounds before a transfer can be delivered.
func VerifyManifest(def TransferDefinition, manifest Manifest) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	expected := def.ExpectedManifest
	if expected.Digest != "" && expected.Digest != manifest.Digest {
		return fmt.Errorf("%w: expected digest %s, got %s", ErrManifestMismatch, expected.Digest, manifest.Digest)
	}
	if expected.Count > 0 && expected.Count != len(manifest.Files) {
		return fmt.Errorf("%w: expected %d files, got %d", ErrManifestMismatch, expected.Count, len(manifest.Files))
	}
	var total int64
	for _, file := range manifest.Files {
		fullMatch, _ := path.Match(def.FilePattern, file.Path)
		baseMatch, _ := path.Match(def.FilePattern, path.Base(file.Path))
		if !fullMatch && !baseMatch {
			return fmt.Errorf("%w: %s does not match %s", ErrManifestMismatch, file.Path, def.FilePattern)
		}
		if file.Size < expected.MinFileSize || (expected.MaxFileSize > 0 && file.Size > expected.MaxFileSize) {
			return fmt.Errorf("%w: file %s size %d outside bounds", ErrManifestMismatch, file.Path, file.Size)
		}
		total += file.Size
	}
	if total < expected.MinTotalSize || (expected.MaxTotalSize > 0 && total > expected.MaxTotalSize) {
		return fmt.Errorf("%w: total size %d outside bounds", ErrManifestMismatch, total)
	}
	if len(expected.Files) > 0 {
		want := cloneFiles(expected.Files)
		sort.Slice(want, func(i, j int) bool { return want[i].Path < want[j].Path })
		got := cloneFiles(manifest.Files)
		sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
		if len(want) != len(got) {
			return fmt.Errorf("%w: exact file set count", ErrManifestMismatch)
		}
		for i := range want {
			if want[i].Path != got[i].Path || !strings.EqualFold(want[i].SHA256, got[i].SHA256) || want[i].Size != got[i].Size {
				return fmt.Errorf("%w: exact file claim for %s", ErrManifestMismatch, got[i].Path)
			}
		}
	}
	return nil
}

func validatePath(name string) error {
	clean := path.Clean(name)
	if name == "" || path.IsAbs(name) || strings.Contains(name, "\\") || clean != name || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%w: unsafe file path %q", ErrManifestMismatch, name)
	}
	return nil
}

func validateFileClaim(file ManifestFile) error {
	if err := validatePath(file.Path); err != nil {
		return err
	}
	if len(file.SHA256) != sha256.Size*2 {
		return fmt.Errorf("%w: invalid sha256 for %s", ErrManifestMismatch, file.Path)
	}
	if _, err := hex.DecodeString(file.SHA256); err != nil {
		return fmt.Errorf("%w: invalid sha256 for %s", ErrManifestMismatch, file.Path)
	}
	if file.Size < 0 || int64(len(file.Content)) != file.Size || !strings.EqualFold(sha256Hex(file.Content), file.SHA256) {
		return fmt.Errorf("%w: hash or size for %s", ErrManifestMismatch, file.Path)
	}
	return nil
}

func manifestWithDigest(files []ManifestFile) (Manifest, error) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for i := 1; i < len(files); i++ {
		if files[i-1].Path == files[i].Path {
			return Manifest{}, fmt.Errorf("%w: duplicate file %s", ErrManifestMismatch, files[i].Path)
		}
	}
	canonical, err := canonicalManifest(files)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{Files: files, Digest: hashBytes(canonical)}, nil
}

func canonicalManifest(files []ManifestFile) ([]byte, error) {
	claims := make([]ManifestFile, len(files))
	copy(claims, files)
	for i := range claims {
		claims[i].Content = nil
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].Path < claims[j].Path })
	return json.Marshal(struct {
		Files []ManifestFile `json:"files"`
	}{claims})
}

func cloneFiles(in []ManifestFile) []ManifestFile {
	out := append([]ManifestFile(nil), in...)
	for i := range out {
		out[i].Content = append([]byte(nil), in[i].Content...)
	}
	return out
}

// Event is a hash-chained state transition. EvidenceDigest binds the event
// to the manifest or receipt that caused it.
type Event struct {
	Sequence       uint64    `json:"sequence"`
	TransferID     string    `json:"transfer_id"`
	From           State     `json:"from"`
	To             State     `json:"to"`
	At             time.Time `json:"at"`
	Reason         string    `json:"reason"`
	EvidenceDigest string    `json:"evidence_digest,omitempty"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	Digest         string    `json:"digest"`
}

// Receipt is a hash-chained observation from an in-memory adapter or caller.
type Receipt struct {
	ID             string    `json:"id"`
	TransferID     string    `json:"transfer_id"`
	ManifestDigest string    `json:"manifest_digest"`
	Kind           string    `json:"kind"`
	At             time.Time `json:"at"`
	PreviousDigest string    `json:"previous_digest,omitempty"`
	Digest         string    `json:"digest"`
}

type Transfer struct {
	Definition     TransferDefinition `json:"definition"`
	State          State              `json:"state"`
	ManifestDigest string             `json:"manifest_digest,omitempty"`
	Events         []Event            `json:"events"`
	Receipts       []Receipt          `json:"receipts"`
}

func (t Transfer) EventChainValid() bool {
	previous := ""
	for i, event := range t.Events {
		if event.Sequence != uint64(i+1) || event.PreviousDigest != previous || event.Digest != digestEvent(event, false) {
			return false
		}
		previous = event.Digest
	}
	return true
}

func (t Transfer) ReceiptChainValid() bool {
	previous := ""
	for _, receipt := range t.Receipts {
		if receipt.TransferID != t.Definition.ID || receipt.ManifestDigest != t.ManifestDigest || receipt.PreviousDigest != previous || receipt.Digest != digestReceipt(receipt, false) {
			return false
		}
		previous = receipt.Digest
	}
	return true
}

// Lifecycle is a standalone pure state machine useful to adapters and tests.
type Lifecycle struct {
	TransferID string
	State      State
	Events     []Event
}

func NewLifecycle(transferID string) (Lifecycle, error) {
	if transferID == "" {
		return Lifecycle{}, ErrInvalidDefinition
	}
	return Lifecycle{TransferID: transferID, State: StateDeclared}, nil
}

func (l *Lifecycle) Transition(to State, at time.Time, reason, evidenceDigest string) (Event, error) {
	if !allowedTransition(l.State, to) {
		return Event{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, l.State, to)
	}
	event := Event{Sequence: uint64(len(l.Events) + 1), TransferID: l.TransferID, From: l.State, To: to, At: at.UTC(), Reason: reason, EvidenceDigest: evidenceDigest}
	if len(l.Events) > 0 {
		event.PreviousDigest = l.Events[len(l.Events)-1].Digest
	}
	event.Digest = digestEvent(event, true)
	l.Events = append(l.Events, event)
	l.State = to
	return event, nil
}

func allowedTransition(from, to State) bool {
	switch from {
	case StateDeclared:
		return to == StateStaged || to == StateFailed || to == StateQuarantined
	case StateStaged:
		return to == StateVerified || to == StateFailed || to == StateQuarantined
	case StateVerified:
		return to == StateDelivered || to == StateFailed || to == StateQuarantined
	case StateDelivered:
		return to == StateAcknowledged || to == StateFailed
	case StateFailed, StateQuarantined:
		return to == StateStaged
	default:
		return false
	}
}

func digestEvent(event Event, includeDigest bool) string {
	if !includeDigest {
		event.Digest = ""
	}
	b, _ := json.Marshal(event)
	return hashBytes(b)
}

func digestReceipt(receipt Receipt, includeDigest bool) string {
	if !includeDigest {
		receipt.Digest = ""
	}
	b, _ := json.Marshal(receipt)
	return hashBytes(b)
}

func hashBytes(b []byte) string {
	return "sha256:" + sha256Hex(b)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// MemoryPort is a concurrency-safe in-memory port for declarations and their
// immutable evidence history.
type MemoryPort struct {
	mu        sync.RWMutex
	transfers map[string]Transfer
}

type Port = MemoryPort

func NewMemoryPort() *MemoryPort { return &MemoryPort{transfers: make(map[string]Transfer)} }
func NewPort() *MemoryPort       { return NewMemoryPort() }

func (p *MemoryPort) Declare(def TransferDefinition) (Transfer, error) {
	if p == nil {
		return Transfer{}, ErrInvalidDefinition
	}
	if err := def.Validate(); err != nil {
		return Transfer{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.transfers[def.ID]; exists {
		return Transfer{}, ErrAlreadyExists
	}
	t := Transfer{Definition: def, State: StateDeclared}
	p.transfers[def.ID] = cloneTransfer(t)
	return cloneTransfer(t), nil
}

func (p *MemoryPort) Get(id string) (Transfer, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	t, ok := p.transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	return cloneTransfer(t), nil
}

func (p *MemoryPort) Stage(id string, manifest Manifest, at ...time.Time) (Transfer, error) {
	return p.transition(id, StateStaged, manifest, "staged", at)
}

func (p *MemoryPort) Verify(id string, manifest Manifest, at ...time.Time) (Transfer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if err := VerifyManifest(t.Definition, manifest); err != nil {
		if allowedTransition(t.State, StateQuarantined) {
			l := lifecycleFor(t)
			_, _ = l.Transition(StateQuarantined, eventTime(at), "manifest verification failed", manifest.Digest)
			t = transferFor(t, l)
			p.transfers[id] = cloneTransfer(t)
		}
		return cloneTransfer(t), err
	}
	if t.State != StateStaged {
		return cloneTransfer(t), fmt.Errorf("%w: verify requires STAGED", ErrInvalidTransition)
	}
	t.ManifestDigest = manifest.Digest
	l := lifecycleFor(t)
	_, err := l.Transition(StateVerified, eventTime(at), "manifest verified", manifest.Digest)
	if err != nil {
		return cloneTransfer(t), err
	}
	t = transferFor(t, l)
	p.transfers[id] = cloneTransfer(t)
	return cloneTransfer(t), nil
}

func (p *MemoryPort) Deliver(id string, manifestDigest string, at ...time.Time) (Transfer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if t.ManifestDigest != manifestDigest || manifestDigest == "" {
		return cloneTransfer(t), ErrManifestMismatch
	}
	if t.State == StateDelivered || t.State == StateAcknowledged {
		return cloneTransfer(t), nil
	}
	if t.State != StateVerified {
		return cloneTransfer(t), fmt.Errorf("%w: deliver requires VERIFIED", ErrInvalidTransition)
	}
	l := lifecycleFor(t)
	_, err := l.Transition(StateDelivered, eventTime(at), "delivered", manifestDigest)
	if err != nil {
		return cloneTransfer(t), err
	}
	t = transferFor(t, l)
	p.transfers[id] = cloneTransfer(t)
	return cloneTransfer(t), nil
}

func (p *MemoryPort) Acknowledge(id string, receipt Receipt, at ...time.Time) (Transfer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if t.State == StateAcknowledged {
		if receipt.TransferID != id || receipt.ManifestDigest != t.ManifestDigest || receipt.Kind == "" {
			return cloneTransfer(t), ErrReceiptMismatch
		}
		return cloneTransfer(t), nil
	}
	if t.State != StateDelivered || receipt.TransferID != id || receipt.ManifestDigest != t.ManifestDigest || receipt.Kind == "" {
		return cloneTransfer(t), ErrReceiptMismatch
	}
	receipt.ID = strings.TrimSpace(receipt.ID)
	if receipt.ID == "" {
		receipt.ID = fmt.Sprintf("receipt-%d", len(t.Receipts)+1)
	}
	when := receipt.At
	if len(at) > 0 {
		when = at[0]
	}
	receipt.At = eventTime([]time.Time{when})
	if len(t.Receipts) > 0 {
		receipt.PreviousDigest = t.Receipts[len(t.Receipts)-1].Digest
	}
	receipt.Digest = digestReceipt(receipt, true)
	l := lifecycleFor(t)
	_, err := l.Transition(StateAcknowledged, receipt.At, "acknowledged", receipt.Digest)
	if err != nil {
		return cloneTransfer(t), err
	}
	t.Receipts = append(t.Receipts, receipt)
	t = transferFor(t, l)
	p.transfers[id] = cloneTransfer(t)
	return cloneTransfer(t), nil
}

func (p *MemoryPort) transition(id string, state State, manifest Manifest, reason string, at []time.Time) (Transfer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.transfers[id]
	if !ok {
		return Transfer{}, ErrNotFound
	}
	if state == StateStaged {
		if manifest.Digest == "" {
			return cloneTransfer(t), fmt.Errorf("%w: staged manifest digest is required", ErrManifestMismatch)
		}
		t.ManifestDigest = manifest.Digest
	}
	l := lifecycleFor(t)
	_, err := l.Transition(state, eventTime(at), reason, manifest.Digest)
	if err != nil {
		return cloneTransfer(t), err
	}
	t = transferFor(t, l)
	p.transfers[id] = cloneTransfer(t)
	return cloneTransfer(t), nil
}

func lifecycleFor(t Transfer) Lifecycle {
	return Lifecycle{TransferID: t.Definition.ID, State: t.State, Events: append([]Event(nil), t.Events...)}
}
func transferFor(t Transfer, l Lifecycle) Transfer {
	t.State, t.Events = l.State, append([]Event(nil), l.Events...)
	return t
}
func eventTime(at []time.Time) time.Time {
	if len(at) > 0 && !at[0].IsZero() {
		return at[0].UTC()
	}
	return time.Now().UTC()
}
func cloneTransfer(t Transfer) Transfer {
	t.Definition.ExpectedManifest.Files = cloneFiles(t.Definition.ExpectedManifest.Files)
	t.Events = append([]Event(nil), t.Events...)
	t.Receipts = append([]Receipt(nil), t.Receipts...)
	return t
}
