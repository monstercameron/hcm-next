package rebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// SnapshotStream binds one accelerated replay input to the exact ledger head
// and schema that produced it.
type SnapshotStream struct {
	StreamKey  string `json:"stream_key"`
	Head       int64  `json:"head"`
	HeadDigest string `json:"head_digest"`
	SchemaRef  string `json:"schema_ref"`
}

// SnapshotManifest is the verified control plane for a replay snapshot. The
// snapshot is never authoritative: a caller may use it only when Verify and
// CompatibleWithCurrentHeads both succeed.
type SnapshotManifest struct {
	Tenant         uuid.UUID        `json:"tenant"`
	ProjectionName string           `json:"projection_name"`
	SchemaDigest   string           `json:"schema_digest"`
	ConfigDigest   string           `json:"config_digest"`
	KeyRefs        []string         `json:"key_refs"`
	Streams        []SnapshotStream `json:"streams"`
	CreatedAt      time.Time        `json:"created_at"`
	ManifestDigest string           `json:"manifest_digest"`
}

var (
	ErrSnapshotInvalid = errors.New("rebuild: invalid replay snapshot")
	ErrSnapshotDrift   = errors.New("rebuild: replay snapshot does not match current heads")
)

// CanonicalBytes returns the stable manifest preimage, with streams and key
// references sorted so database row order cannot change its identity.
func (m SnapshotManifest) CanonicalBytes() ([]byte, error) {
	copyManifest := m
	copyManifest.ManifestDigest = ""
	copyManifest.KeyRefs = append([]string(nil), m.KeyRefs...)
	sort.Strings(copyManifest.KeyRefs)
	copyManifest.Streams = append([]SnapshotStream(nil), m.Streams...)
	sort.Slice(copyManifest.Streams, func(i, j int) bool { return copyManifest.Streams[i].StreamKey < copyManifest.Streams[j].StreamKey })
	return json.Marshal(copyManifest)
}

// Digest computes the content identity of the manifest.
func (m SnapshotManifest) Digest() (string, error) {
	b, err := m.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Validate verifies required bindings, uniqueness, positive heads and the
// stored canonical digest.
func (m SnapshotManifest) Validate() error {
	if m.Tenant == uuid.Nil || m.ProjectionName == "" || m.SchemaDigest == "" || m.ConfigDigest == "" || m.CreatedAt.IsZero() || len(m.Streams) == 0 {
		return fmt.Errorf("%w: tenant, projection, schema, config, time and streams are required", ErrSnapshotInvalid)
	}
	seen := make(map[string]struct{}, len(m.Streams))
	for _, stream := range m.Streams {
		if stream.StreamKey == "" || stream.Head < 1 || stream.HeadDigest == "" || stream.SchemaRef == "" {
			return fmt.Errorf("%w: every stream binds a positive head, digest and schema", ErrSnapshotInvalid)
		}
		if _, ok := seen[stream.StreamKey]; ok {
			return fmt.Errorf("%w: stream %s is repeated", ErrSnapshotInvalid, stream.StreamKey)
		}
		seen[stream.StreamKey] = struct{}{}
	}
	got, err := m.Digest()
	if err != nil {
		return fmt.Errorf("%w: digest: %v", ErrSnapshotInvalid, err)
	}
	if got != m.ManifestDigest {
		return fmt.Errorf("%w: digest %s does not match %s", ErrSnapshotInvalid, m.ManifestDigest, got)
	}
	return nil
}

// CompatibleWithCurrentHeads verifies a manifest before accelerated replay.
func (m SnapshotManifest) CompatibleWithCurrentHeads(heads map[string]SnapshotStream) error {
	if err := m.Validate(); err != nil {
		return err
	}
	for _, stream := range m.Streams {
		current, ok := heads[stream.StreamKey]
		if !ok || current.Head != stream.Head || current.HeadDigest != stream.HeadDigest || current.SchemaRef != stream.SchemaRef {
			return fmt.Errorf("%w: stream %s", ErrSnapshotDrift, stream.StreamKey)
		}
	}
	return nil
}

// SaveSnapshot durably records a verified manifest and state digest. The row
// is append-only; a corrected snapshot receives a new snapshot ID.
func SaveSnapshot(ctx context.Context, tx dbport.Tx, m SnapshotManifest, rowCount int64, stateDigest string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if rowCount < 0 || stateDigest == "" {
		return fmt.Errorf("%w: state count and digest are required", ErrSnapshotInvalid)
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("rebuild: marshal snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO replay_snapshot (tenant_id, snapshot_id, projection_name, manifest, manifest_digest, state_row_count, state_digest, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, m.Tenant, uuid.New(), m.ProjectionName, manifest, m.ManifestDigest, rowCount, stateDigest, m.CreatedAt.UTC()); err != nil {
		return fmt.Errorf("rebuild: save snapshot: %w", err)
	}
	return nil
}
