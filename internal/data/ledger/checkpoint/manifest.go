package checkpoint

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// ComputeRootDigest folds every included stream head into one value.
//
// Each field is length-prefixed with a 64-bit big-endian length before it is
// hashed, the same framing internal/data/ledger.SHA256Digester and
// internal/data/ledger/hashchain use. Without it, two different head sets
// could produce the same preimage by moving a character across a field
// boundary. Heads are sorted by stream key first, so the digest is a
// function of the set and not of the order it happened to be read in.
func ComputeRootDigest(streams []StreamHead) (string, error) {
	ordered := make([]StreamHead, len(streams))
	copy(ordered, streams)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].StreamKey < ordered[j].StreamKey })

	h := sha256.New()
	var frame [8]byte
	binary.BigEndian.PutUint64(frame[:], uint64(len(ordered)))
	if _, err := h.Write(frame[:]); err != nil {
		return "", fmt.Errorf("checkpoint: fold stream count: %w", err)
	}
	for i, s := range ordered {
		if i > 0 && s.StreamKey == ordered[i-1].StreamKey {
			return "", fmt.Errorf("checkpoint: stream %s is included twice", s.StreamKey)
		}
		var sequence [8]byte
		binary.BigEndian.PutUint64(sequence[:], uint64(s.Sequence))
		fields := [][]byte{
			[]byte(s.StreamKey),
			sequence[:],
			[]byte(s.ChainHash),
			[]byte(s.ChainAlgorithm),
		}
		for _, field := range fields {
			binary.BigEndian.PutUint64(frame[:], uint64(len(field)))
			if _, err := h.Write(frame[:]); err != nil {
				return "", fmt.Errorf("checkpoint: fold stream %s: %w", s.StreamKey, err)
			}
			if _, err := h.Write(field); err != nil {
				return "", fmt.Errorf("checkpoint: fold stream %s: %w", s.StreamKey, err)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// manifestPayload is the canonical projection [Manifest.CanonicalDigest]
// hashes and a [Signature] therefore covers: every field of the manifest
// except the signature value itself, so a signature can never cover its own
// bytes.
//
// The signing key's identifier and public key ARE inside the payload. That
// is deliberate: it binds the attestation to the key that made it, so a
// signature cannot be re-attributed to a different key by rewriting the
// envelope around it.
type manifestPayload struct {
	SchemaVersion          int                 `json:"schema_version"`
	Tenant                 string              `json:"tenant"`
	EpochID                string              `json:"epoch_id"`
	EpochNumber            int64               `json:"epoch_number"`
	PreviousEpochID        string              `json:"previous_epoch_id"`
	PreviousManifestDigest string              `json:"previous_manifest_digest"`
	CorrectsEpochID        string              `json:"corrects_epoch_id"`
	CorrectsReason         string              `json:"corrects_reason"`
	SchemaReleaseVersion   int64               `json:"schema_release_version"`
	SchemaReleaseDigest    string              `json:"schema_release_digest"`
	Streams                []streamHeadPayload `json:"streams"`
	RootDigest             string              `json:"root_digest"`
	RootDigestAlgorithm    string              `json:"root_digest_algorithm"`
	CoversFrom             int64               `json:"covers_from_ns"`
	CoversTo               int64               `json:"covers_to_ns"`
	CreatedAt              int64               `json:"created_at_ns"`
	SignatureAlgorithm     string              `json:"signature_algorithm"`
	SigningKeyID           string              `json:"signing_key_id"`
	SigningPublicKey       string              `json:"signing_public_key"`
}

type streamHeadPayload struct {
	StreamKey      string `json:"stream_key"`
	Sequence       int64  `json:"sequence"`
	ChainHash      string `json:"chain_hash"`
	ChainAlgorithm string `json:"chain_algorithm"`
}

// OrderedStreams returns the manifest's heads sorted by stream key, which is
// the order the root digest and the canonical projection both use.
func (m Manifest) OrderedStreams() []StreamHead {
	out := make([]StreamHead, len(m.Streams))
	copy(out, m.Streams)
	sort.Slice(out, func(i, j int) bool { return out[i].StreamKey < out[j].StreamKey })
	return out
}

func (m Manifest) payload() manifestPayload {
	streams := m.OrderedStreams()
	projected := make([]streamHeadPayload, 0, len(streams))
	for _, s := range streams {
		projected = append(projected, streamHeadPayload{
			StreamKey: s.StreamKey, Sequence: s.Sequence,
			ChainHash: s.ChainHash, ChainAlgorithm: s.ChainAlgorithm,
		})
	}
	p := manifestPayload{
		SchemaVersion:          m.SchemaVersion,
		Tenant:                 m.Tenant.String(),
		EpochID:                m.EpochID.String(),
		EpochNumber:            m.EpochNumber,
		PreviousEpochID:        m.PreviousEpochID.String(),
		PreviousManifestDigest: m.PreviousManifestDigest,
		CorrectsEpochID:        m.CorrectsEpochID.String(),
		CorrectsReason:         m.CorrectsReason,
		SchemaReleaseVersion:   m.Schema.Version,
		SchemaReleaseDigest:    m.Schema.Digest,
		Streams:                projected,
		RootDigest:             m.RootDigest,
		RootDigestAlgorithm:    m.RootDigestAlgorithm,
		CoversFrom:             m.CoversFrom.UTC().UnixNano(),
		CoversTo:               m.CoversTo.UTC().UnixNano(),
		CreatedAt:              m.CreatedAt.UTC().UnixNano(),
	}
	if m.Signature != nil {
		p.SignatureAlgorithm = m.Signature.Algorithm
		p.SigningKeyID = m.Signature.KeyID
		p.SigningPublicKey = m.Signature.PublicKey
	}
	return p
}

// CanonicalDigest returns the hex-encoded sha256 digest of the manifest's
// canonical projection. It is what [Sign] signs and what [Verify] checks,
// and it is stable across re-serialization: two manifests with identical
// content digest identically no matter how their stream heads were ordered
// on the way in.
func (m Manifest) CanonicalDigest() (string, error) {
	body, err := json.Marshal(m.payload())
	if err != nil {
		return "", fmt.Errorf("checkpoint: marshal canonical manifest: %w", err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// Validate reports every reason the manifest is not a checkpoint. It is the
// gate the RED clause describes: a manifest missing its stream range, a
// head, its schema binding, its key or its times is refused here rather than
// signed and discovered later.
//
// It does not check the signature - a manifest is validated before it is
// signed, and [Verify] checks the signature afterwards.
func (m Manifest) Validate() error {
	var missing []string
	add := func(format string, args ...any) { missing = append(missing, fmt.Sprintf(format, args...)) }

	if m.SchemaVersion != ManifestSchemaVersion {
		add("manifest schema version is %d, want %d", m.SchemaVersion, ManifestSchemaVersion)
	}
	if m.Tenant == uuid.Nil {
		add("tenant is required")
	}
	if m.EpochID == uuid.Nil {
		add("epoch id is required")
	}
	if m.EpochNumber < 1 {
		add("epoch number %d is not positive", m.EpochNumber)
	}

	// Epoch 1 opens a chain; every later epoch must name its predecessor and
	// carry that predecessor's digest, so a removed epoch leaves a hole.
	if m.EpochNumber > 1 {
		if m.PreviousEpochID == uuid.Nil {
			add("epoch %d does not name the epoch it follows", m.EpochNumber)
		}
		if m.PreviousManifestDigest == "" {
			add("epoch %d does not carry the previous epoch's manifest digest", m.EpochNumber)
		}
	} else {
		if m.PreviousEpochID != uuid.Nil || m.PreviousManifestDigest != "" {
			add("epoch 1 opens the chain and cannot follow another epoch")
		}
	}
	if m.CorrectsEpochID != uuid.Nil && m.CorrectsReason == "" {
		add("a corrective epoch must say why it corrects epoch %s", m.CorrectsEpochID)
	}
	if m.CorrectsEpochID == uuid.Nil && m.CorrectsReason != "" {
		add("a correction reason was given without an epoch to correct")
	}

	if m.Schema.Version < 1 {
		add("schema release version is required")
	}
	if m.Schema.Digest == "" {
		add("schema release digest is required")
	}

	if len(m.Streams) == 0 {
		add("a checkpoint must cover at least one stream")
	}
	seen := make(map[string]bool, len(m.Streams))
	for _, s := range m.Streams {
		switch {
		case s.StreamKey == "":
			add("a stream head carries no stream key")
			continue
		case seen[s.StreamKey]:
			add("stream %s is included twice", s.StreamKey)
			continue
		}
		seen[s.StreamKey] = true
		if s.Sequence < 1 {
			add("stream %s has head sequence %d; a covered stream holds at least one event", s.StreamKey, s.Sequence)
		}
		if s.ChainHash == "" {
			add("stream %s has no chain hash; its head proves nothing about what came before it", s.StreamKey)
		}
		if s.ChainAlgorithm == "" {
			add("stream %s does not record the algorithm its chain hash was produced with", s.StreamKey)
		}
	}

	if m.RootDigestAlgorithm != DigestAlgorithm {
		add("root digest algorithm is %q, want %q", m.RootDigestAlgorithm, DigestAlgorithm)
	}
	if m.RootDigest == "" {
		add("root digest is required")
	}

	if m.CoversFrom.IsZero() || m.CoversTo.IsZero() {
		add("the covered recorded-time window is required")
	} else if !m.CoversFrom.Before(m.CoversTo) {
		add("the covered window [%s, %s) is empty or inverted",
			m.CoversFrom.UTC().Format("2006-01-02T15:04:05Z07:00"),
			m.CoversTo.UTC().Format("2006-01-02T15:04:05Z07:00"))
	}
	if m.CreatedAt.IsZero() {
		add("creation time is required")
	}

	if m.Signature == nil {
		add("the signing key must be named before the manifest is signed")
	} else {
		if m.Signature.Algorithm != AlgorithmEd25519 {
			add("signature algorithm is %q, want %q", m.Signature.Algorithm, AlgorithmEd25519)
		}
		if m.Signature.KeyID == "" {
			add("signing key id is required")
		}
		if m.Signature.PublicKey == "" {
			add("signing public key is required")
		}
	}

	if len(missing) > 0 {
		return ErrManifestInvalid{EpochNumber: m.EpochNumber, Missing: missing}
	}

	// Only once the shape is sound is the root digest worth comparing: a
	// recomputation over an incomplete head set would report a mismatch that
	// is really a missing field.
	expected, err := ComputeRootDigest(m.Streams)
	if err != nil {
		return err
	}
	if expected != m.RootDigest {
		return ErrRootDigestMismatch{EpochNumber: m.EpochNumber, Expected: expected, Actual: m.RootDigest}
	}
	return nil
}
