package ledger

import (
	"fmt"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	datalogger "github.com/monstercameron/hcm-next/internal/data/ledger"
	"github.com/monstercameron/hcm-next/internal/kernel/canonical"
	"github.com/monstercameron/hcm-next/internal/kernel/digest"
)

// LedgerEventProfileV1 is the registered canonical profile for a ledger
// event's digest input: the schema reference the payload was written under,
// and the payload bytes (or, for out-of-line bytes, the artifact reference
// standing in for them). It projects
// hcmnext.intents.v1.TypedPayload - the canonical-envelope-and-digest.md
// envelope for "typed bytes plus their schema" - rather than inventing a new
// message; digest.ProfileLedgerEvent is the profile id kernel/digest already
// reserves for this purpose.
func LedgerEventProfileV1() canonical.Profile {
	return canonical.Profile{
		ID:                  digest.ProfileLedgerEvent,
		Version:             1,
		SchemaID:            digest.SchemaIntentsV1,
		SchemaVersion:       digest.SchemaIntentsV1Version,
		MessageName:         "hcmnext.intents.v1.TypedPayload",
		Material:            []string{"schema.schema_id", "protobuf_wire_bytes"},
		RejectUnknownFields: true,
	}
}

// NewLedgerEventDigestRegistry returns a digest registry publishing sha256
// and LedgerEventProfileV1. Each caller gets its own registry; there is no
// shared mutable global to fight over.
func NewLedgerEventDigestRegistry() (*digest.Registry, error) {
	r := digest.NewRegistry()
	if err := r.RegisterProfile(LedgerEventProfileV1(), digest.ScopeSpec{}); err != nil {
		return nil, err
	}
	return r, nil
}

// KernelDigester computes a ledger event's digest through
// internal/kernel/digest under a registered, versioned canonicalization
// profile - the platform's one canonical digest authority - rather than
// internal/data/ledger's built-in ad hoc SHA256Digester. It implements the
// [Digester] port (whose method matches internal/data/ledger.Digester
// exactly), so it plugs directly into internal/data/ledger.WithDigester.
type KernelDigester struct {
	registry *digest.Registry
}

// NewKernelDigester wraps a registry that publishes LedgerEventProfileV1
// (NewLedgerEventDigestRegistry, or a caller's own registry that also
// publishes it).
func NewKernelDigester(registry *digest.Registry) *KernelDigester {
	return &KernelDigester{registry: registry}
}

// Digest implements the Digester port: it builds the TypedPayload the
// profile canonicalizes and returns the registry's algorithm, digest and
// canonical length.
func (d *KernelDigester) Digest(payload []byte, schemaRef string) (algorithm, digestHex string, length int, err error) {
	msg := &intentsv1.TypedPayload{
		Schema:            &intentsv1.SchemaReference{SchemaId: schemaRef},
		ProtobufWireBytes: payload,
	}
	ref, _, err := d.registry.Compute(msg, digest.ProfileLedgerEvent)
	if err != nil {
		return "", "", 0, fmt.Errorf("ledger: kernel digest: %w", err)
	}
	return ref.AlgorithmID, ref.Digest, int(ref.CanonicalLength), nil
}

// ErrDigestMismatch reports that a recorded event's digest does not
// reproduce under recomputation - proof that VerifyEvent actually
// recomputes rather than trusting the stored value.
type ErrDigestMismatch struct {
	StreamKey  string
	Sequence   int64
	Recorded   string
	Recomputed string
}

func (ErrDigestMismatch) Code() string { return "LEDGER_DIGEST_MISMATCH" }

func (e ErrDigestMismatch) Error() string {
	return fmt.Sprintf("%s: stream %s@%d recorded digest %s but recomputation yields %s",
		e.Code(), e.StreamKey, e.Sequence, e.Recorded, e.Recomputed)
}

// digestInputOf mirrors internal/data/ledger's own digestInput rule: the
// inline payload when there is one, otherwise the artifact reference that
// stands in for it.
func digestInputOf(rec EventRecord) []byte {
	if rec.Payload != nil {
		return rec.Payload
	}
	return []byte(rec.ArtifactRef)
}

// VerifyEvent recomputes a previously recorded event's digest under d's
// registry and compares algorithm, digest and canonical length against what
// was recorded. It is how "replay verifies it" is proved: reading an event
// back is never enough on its own, because a corrupted or forged row would
// read back fine - only recomputation catches it.
func (d *KernelDigester) VerifyEvent(rec EventRecord) error {
	algorithm, digestHex, length, err := d.Digest(digestInputOf(rec), rec.SchemaRef)
	if err != nil {
		return err
	}
	if algorithm != rec.DigestAlgorithm || digestHex != rec.Digest || length != rec.CanonicalLength {
		return ErrDigestMismatch{StreamKey: rec.StreamKey, Sequence: rec.Sequence, Recorded: rec.Digest, Recomputed: digestHex}
	}
	return nil
}

// NewAppender returns an internal/data/ledger.Appender wired to compute
// digests through registry via KernelDigester, connecting
// internal/kernel/digest to internal/data/ledger's existing WithDigester
// hook (internal/data/ledger/append.go).
func NewAppender(registry *digest.Registry) Appender {
	return datalogger.New(datalogger.WithDigester(NewKernelDigester(registry)))
}

// NewReader returns an internal/data/ledger.Reader as the Reader port.
func NewReader() Reader {
	return datalogger.NewReader()
}
