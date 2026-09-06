// Package dispatch binds privacy decisions to the last safe point before an
// external effect. It is deliberately a pure gate around two injected ports:
// the DLP evaluator and the sender. A changed or revoked processor, transfer,
// destination, region, classification, purpose, or DLP snapshot fails closed
// before the sender is called.
package dispatch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/trust/dlp"
)

const contractVersion = 1

// Version reports the dispatch binding contract version.
func Version() int { return contractVersion }

// Explain describes the enforcement boundary without exposing payload data.
func Explain() string {
	return "dispatch v1: signed processor, transfer, destination, classification, purpose, and DLP snapshots are revalidated before send"
}

var (
	ErrInvalidSnapshot = errors.New("privacy dispatch: invalid policy snapshot")
	ErrInvalidBinding  = errors.New("privacy dispatch: invalid dispatch binding")
	ErrEGRESSBlocked   = errors.New("EGRESS_BLOCKED")
	ErrEgressBlocked   = ErrEGRESSBlocked
	ErrInvalidRequest  = errors.New("privacy dispatch: invalid dispatch request")
)

// SnapshotKind identifies the policy decision represented by a snapshot.
type SnapshotKind string

const (
	SnapshotProcessor SnapshotKind = "PROCESSOR"
	SnapshotTransfer  SnapshotKind = "TRANSFER"
	SnapshotDLP       SnapshotKind = "DLP"
)

// PolicySnapshot is an opaque, signed-at-the-boundary policy release. The
// signature is an external custody/verification token; this package verifies
// the binding digest and requires the token to be present, while key custody
// remains with the trust plane.
type PolicySnapshot struct {
	Kind               SnapshotKind
	Version            string
	Processor          string
	Destination        string
	Region             string
	Classification     dlp.DataClass
	Purpose            string
	TransferAssessment string
	DLPDecision        dlp.Decision
	Revoked            bool
	Digest             string
	Signature          string
}

// SealSnapshot binds the canonical policy fields to an external signature
// token. It does not retain payload bytes or a private key.
func SealSnapshot(snapshot PolicySnapshot, signature string) (PolicySnapshot, error) {
	snapshot.Signature = strings.TrimSpace(signature)
	if snapshot.Signature == "" {
		return PolicySnapshot{}, fmt.Errorf("%w: signature is required", ErrInvalidSnapshot)
	}
	snapshot.Digest = DigestSnapshot(snapshot)
	if err := snapshot.Validate(); err != nil {
		return PolicySnapshot{}, err
	}
	return snapshot, nil
}

// DigestSnapshot computes the canonical digest without including Digest or
// Signature, so revalidation can detect any material policy change.
func DigestSnapshot(snapshot PolicySnapshot) string {
	canonical := fmt.Sprintf("kind=%s;version=%s;processor=%s;destination=%s;region=%s;classification=%s;purpose=%s;transfer=%s;dlp=%s;revoked=%t;",
		snapshot.Kind, snapshot.Version, snapshot.Processor, snapshot.Destination,
		snapshot.Region, snapshot.Classification, snapshot.Purpose,
		snapshot.TransferAssessment, snapshot.DLPDecision, snapshot.Revoked)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// Validate proves that a snapshot is complete, signed, and internally
// consistent. A revoked snapshot remains valid evidence but can never pass
// the dispatch gate.
func (snapshot PolicySnapshot) Validate() error {
	switch snapshot.Kind {
	case SnapshotProcessor, SnapshotTransfer, SnapshotDLP:
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidSnapshot, snapshot.Kind)
	}
	for name, value := range map[string]string{
		"version": snapshot.Version, "processor": snapshot.Processor,
		"destination": snapshot.Destination, "region": snapshot.Region,
		"classification": string(snapshot.Classification), "purpose": snapshot.Purpose,
		"transfer assessment": snapshot.TransferAssessment, "digest": snapshot.Digest,
		"signature": snapshot.Signature,
	} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("%w: %s is required and may not be padded", ErrInvalidSnapshot, name)
		}
	}
	if !snapshot.Classification.Valid() {
		return fmt.Errorf("%w: invalid classification %q", ErrInvalidSnapshot, snapshot.Classification)
	}
	if snapshot.Kind == SnapshotDLP && !snapshot.DLPDecision.Valid() {
		return fmt.Errorf("%w: DLP snapshot has invalid decision %q", ErrInvalidSnapshot, snapshot.DLPDecision)
	}
	if want := DigestSnapshot(snapshot); snapshot.Digest != want {
		return fmt.Errorf("%w: digest mismatch", ErrInvalidSnapshot)
	}
	return nil
}

// SnapshotSet is the complete processor/transfer/DLP decision set required at
// dispatch. Each component is independently signed and digest-bound.
type SnapshotSet struct {
	Processor PolicySnapshot
	Transfer  PolicySnapshot
	DLP       PolicySnapshot
}

func (set SnapshotSet) Validate() error {
	if err := set.Processor.Validate(); err != nil {
		return fmt.Errorf("%w: processor: %v", ErrInvalidBinding, err)
	}
	if err := set.Transfer.Validate(); err != nil {
		return fmt.Errorf("%w: transfer: %v", ErrInvalidBinding, err)
	}
	if err := set.DLP.Validate(); err != nil {
		return fmt.Errorf("%w: dlp: %v", ErrInvalidBinding, err)
	}
	if set.Processor.Kind != SnapshotProcessor || set.Transfer.Kind != SnapshotTransfer || set.DLP.Kind != SnapshotDLP {
		return fmt.Errorf("%w: snapshot kinds do not match dispatch roles", ErrInvalidBinding)
	}
	if set.Processor.Destination != set.Transfer.Destination || set.Processor.Destination != set.DLP.Destination ||
		set.Processor.Purpose != set.Transfer.Purpose || set.Processor.Purpose != set.DLP.Purpose ||
		set.Processor.Region != set.Transfer.Region || set.Processor.Region != set.DLP.Region ||
		set.Processor.Classification != set.Transfer.Classification || set.Processor.Classification != set.DLP.Classification {
		return fmt.Errorf("%w: snapshot route, purpose, region or classification differs", ErrInvalidBinding)
	}
	return nil
}

// Binding is the immutable operation-time binding of all privacy decisions.
type Binding struct {
	OperationDigest string
	Planned         SnapshotSet
}

// Bind creates a binding and derives an operation digest from all snapshots.
func Bind(planned SnapshotSet) (Binding, error) {
	if err := planned.Validate(); err != nil {
		return Binding{}, err
	}
	sum := sha256.Sum256([]byte(planned.Processor.Digest + "|" + planned.Transfer.Digest + "|" + planned.DLP.Digest))
	return Binding{OperationDigest: hex.EncodeToString(sum[:]), Planned: planned}, nil
}

// Decision is the redaction-safe dispatch result. It intentionally contains
// no destination payload or processor response body.
type Decision struct {
	Allowed         bool
	Code            string
	Reason          string
	OperationDigest string
}

func (d Decision) Explain() string {
	return fmt.Sprintf("dispatch allowed=%t code=%s operation=%s reason=%s", d.Allowed, d.Code, d.OperationDigest, d.Reason)
}

// Revalidate compares the planned set with the current signed set. Equality
// is strict because approval-time state cannot substitute for dispatch-time
// state.
func Revalidate(binding Binding, current SnapshotSet) Decision {
	decision := Decision{OperationDigest: binding.OperationDigest}
	if err := binding.Validate(); err != nil {
		decision.Code, decision.Reason = ErrEGRESSBlocked.Error(), err.Error()
		return decision
	}
	if err := current.Validate(); err != nil {
		decision.Code, decision.Reason = ErrEGRESSBlocked.Error(), err.Error()
		return decision
	}
	checks := []struct {
		name string
		want PolicySnapshot
		got  PolicySnapshot
	}{
		{"processor", binding.Planned.Processor, current.Processor},
		{"transfer", binding.Planned.Transfer, current.Transfer},
		{"DLP", binding.Planned.DLP, current.DLP},
	}
	for _, check := range checks {
		if check.got.Digest != check.want.Digest || check.got.Signature != check.want.Signature {
			decision.Code, decision.Reason = ErrEGRESSBlocked.Error(), check.name+" policy snapshot changed"
			return decision
		}
		if check.got.Revoked {
			decision.Code, decision.Reason = ErrEGRESSBlocked.Error(), check.name+" policy snapshot is revoked"
			return decision
		}
	}
	if current.DLP.DLPDecision != dlp.Allow {
		decision.Code, decision.Reason = ErrEGRESSBlocked.Error(), "DLP decision is not ALLOW"
		return decision
	}
	decision.Allowed, decision.Code = true, "DISPATCH_ALLOWED"
	return decision
}

// DLPDecisioner is satisfied by [dlp.Policy] and keeps this gate independent
// of the concrete policy store.
type DLPDecisioner interface {
	Evaluate(dlp.DecisionRequest) (dlp.Evaluation, error)
}

// Sender is the sole external-effect port. Dispatch calls it only after all
// current policy and payload decisions pass.
type Sender interface {
	Send(context.Context, []byte) error
}

// Request carries the ephemeral dispatch payload. The returned receipt keeps
// only its digest.
type Request struct {
	Principal   string
	Destination string
	Purpose     string
	DataClasses []dlp.DataClass
	Inspection  dlp.Inspection
	Payload     []byte
}

// Receipt is durable evidence shaped for an outbox/ledger adapter. The
// sender response is intentionally outside this contract.
type Receipt struct {
	OperationDigest string
	PayloadDigest   string
	FindingsDigest  string
	PolicyDigests   []string
}

// Dispatch revalidates the signed snapshots, evaluates the current DLP
// inspection, and invokes the sender exactly once only when every check is
// allowed. A blocked request returns before the sender is touched.
func Dispatch(ctx context.Context, binding Binding, current SnapshotSet, req Request, policy DLPDecisioner, sender Sender) (Receipt, error) {
	decision := Revalidate(binding, current)
	if !decision.Allowed {
		return Receipt{}, fmt.Errorf("%w: %s", ErrEGRESSBlocked, decision.Reason)
	}
	if ctx == nil || ctx.Err() != nil || sender == nil || policy == nil {
		return Receipt{}, ErrInvalidRequest
	}
	for name, value := range map[string]string{"principal": req.Principal, "destination": req.Destination, "purpose": req.Purpose} {
		if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
			return Receipt{}, fmt.Errorf("%w: %s", ErrInvalidRequest, name)
		}
	}
	if req.Destination != current.DLP.Destination || req.Purpose != current.DLP.Purpose {
		return Receipt{}, fmt.Errorf("%w: request route differs from current policy", ErrEGRESSBlocked)
	}
	evaluation, err := policy.Evaluate(dlp.DecisionRequest{
		Destination: req.Destination, Purpose: req.Purpose, Principal: req.Principal,
		DeclaredClasses: req.DataClasses, Inspection: req.Inspection,
	})
	if err != nil || evaluation.Decision != dlp.Allow || evaluation.Decision != current.DLP.DLPDecision {
		return Receipt{}, fmt.Errorf("%w: current DLP evaluation is not the bound ALLOW decision", ErrEGRESSBlocked)
	}
	if err := sender.Send(ctx, req.Payload); err != nil {
		return Receipt{}, err
	}
	return Receipt{
		OperationDigest: binding.OperationDigest,
		PayloadDigest:   dlp.DigestPayload(req.Payload),
		FindingsDigest:  req.Inspection.Digest(),
		PolicyDigests:   []string{current.Processor.Digest, current.Transfer.Digest, current.DLP.Digest},
	}, nil
}

func (b Binding) Validate() error {
	if strings.TrimSpace(b.OperationDigest) == "" {
		return fmt.Errorf("%w: operation digest is required", ErrInvalidBinding)
	}
	if err := b.Planned.Validate(); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(b.Planned.Processor.Digest + "|" + b.Planned.Transfer.Digest + "|" + b.Planned.DLP.Digest))
	if b.OperationDigest != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%w: operation digest mismatch", ErrInvalidBinding)
	}
	return nil
}
