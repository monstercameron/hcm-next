// Package lineage traces one worker field through the promotion
// transaction and repair lineage (DATA-015): proposal, approval, plan,
// event, connector operation, observation and repair. It is kernel-pure:
// the trace is an attributed digest chain assembled from owner records,
// redaction-safe by construction. UI and audit export consume this same
// service; persistence and publishing stay with internal/data/provenance.
package lineage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrBrokenChain reports nodes whose PrevDigest does not name the
	// previous node's digest.
	ErrBrokenChain = errors.New("lineage: digest chain is broken")

	// ErrIncompleteTrace reports a trace missing stages, repeating a
	// stage or stepping out of order.
	ErrIncompleteTrace = errors.New("lineage: trace is incomplete")

	// ErrUnattributed reports a node missing source, version, time,
	// evidence or digest attribution.
	ErrUnattributed = errors.New("lineage: node is unattributed")

	// ErrHiddenLeak reports a restricted node carrying its hidden payload:
	// the assembler refuses to launder a leak.
	ErrHiddenLeak = errors.New("lineage: restricted node leaks its payload")

	// ErrFieldMismatch reports a node filed under another worker field.
	ErrFieldMismatch = errors.New("lineage: node belongs to another field")
)

// Stage is the closed lineage vocabulary: every trace walks all seven in
// order, from the proposal that asked to the repair that closed.
type Stage string

const (
	StageProposal    Stage = "PROPOSAL"
	StageApproval    Stage = "APPROVAL"
	StagePlan        Stage = "PLAN"
	StageEvent       Stage = "EVENT"
	StageConnectorOp Stage = "CONNECTOR_OPERATION"
	StageObservation Stage = "OBSERVATION"
	StageRepair      Stage = "REPAIR"
)

// Stages is the canonical walk in order.
var Stages = []Stage{
	StageProposal, StageApproval, StagePlan, StageEvent,
	StageConnectorOp, StageObservation, StageRepair,
}

// Node is one attributed hop for one worker field.
type Node struct {
	Stage          Stage
	Field          string
	Source         string
	Version        string
	Authority      string
	Principal      string
	EffectiveAt    time.Time
	KnownAt        time.Time
	Evidence       []string
	Digest         string
	PrevDigest     string
	StreamKey      string
	Sequence       int64
	ObservationRef string
	ConnectorRef   string
	Payload        string
	Restricted     bool
	Reason         string
}

// Stub returns the node as the trace carries it: restricted nodes lose
// their payload and keep only the redaction reason.
func (n Node) Stub() Node {
	if !n.Restricted {
		return n
	}
	stub := n
	stub.Payload = ""
	return stub
}

// Trace is one complete assembled chain for one worker field.
type Trace struct {
	Tenant    string
	IntentRef string
	Field     string
	Nodes     []Node
	Redacted  int
	Digest    string
}

func traceDigest(tenant, intentRef, field string, nodes []Node) string {
	parts := []string{"lineage", tenant, intentRef, field}
	for _, node := range nodes {
		parts = append(parts, string(node.Stage), node.Digest)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Assemble verifies and seals one chain: continuity, the exact seven
// stages in order, full source/version/time/evidence/digest attribution
// and redaction safety. Restricted nodes are carried as stubs.
func Assemble(tenant, intentRef, field string, nodes []Node) (Trace, error) {
	if strings.TrimSpace(tenant) == "" || strings.TrimSpace(intentRef) == "" || strings.TrimSpace(field) == "" {
		return Trace{}, fmt.Errorf("lineage: Assemble: %w", ErrUnattributed)
	}
	if len(nodes) != len(Stages) {
		return Trace{}, fmt.Errorf("lineage: Assemble %d nodes: %w", len(nodes), ErrIncompleteTrace)
	}
	sealed := make([]Node, 0, len(nodes))
	redacted := 0
	for i, node := range nodes {
		if node.Stage != Stages[i] {
			return Trace{}, fmt.Errorf("lineage: Assemble node %d is %s, want %s: %w", i, node.Stage, Stages[i], ErrIncompleteTrace)
		}
		if node.Field != field {
			return Trace{}, fmt.Errorf("lineage: Assemble node %d field %q: %w", i, node.Field, ErrFieldMismatch)
		}
		if strings.TrimSpace(node.Source) == "" || strings.TrimSpace(node.Version) == "" ||
			strings.TrimSpace(node.Authority) == "" || strings.TrimSpace(node.Digest) == "" ||
			len(node.Evidence) == 0 || node.EffectiveAt.IsZero() || node.KnownAt.IsZero() {
			return Trace{}, fmt.Errorf("lineage: Assemble %s node: %w", node.Stage, ErrUnattributed)
		}
		if i == 0 {
			if node.PrevDigest != "" {
				return Trace{}, fmt.Errorf("lineage: Assemble first node names a predecessor: %w", ErrBrokenChain)
			}
		} else if node.PrevDigest != nodes[i-1].Digest {
			return Trace{}, fmt.Errorf("lineage: Assemble %s node: %w", node.Stage, ErrBrokenChain)
		}
		if node.Restricted {
			if node.Payload != "" || strings.TrimSpace(node.Reason) == "" {
				return Trace{}, fmt.Errorf("lineage: Assemble %s node: %w", node.Stage, ErrHiddenLeak)
			}
			redacted++
		}
		sealed = append(sealed, node.Stub())
	}
	return Trace{
		Tenant: tenant, IntentRef: intentRef, Field: field,
		Nodes: sealed, Redacted: redacted,
		Digest: traceDigest(tenant, intentRef, field, sealed),
	}, nil
}

// Verify recomputes the seal: edited nodes, swapped evidence or a restaged
// chain all fail.
func (t Trace) Verify() error {
	if len(t.Nodes) != len(Stages) {
		return fmt.Errorf("lineage: Verify: %w", ErrIncompleteTrace)
	}
	for i, node := range t.Nodes {
		if node.Stage != Stages[i] {
			return fmt.Errorf("lineage: Verify: %w", ErrIncompleteTrace)
		}
		if node.Restricted && node.Payload != "" {
			return fmt.Errorf("lineage: Verify %s node: %w", node.Stage, ErrHiddenLeak)
		}
		if i > 0 && node.PrevDigest != t.Nodes[i-1].Digest {
			return fmt.Errorf("lineage: Verify %s node: %w", node.Stage, ErrBrokenChain)
		}
	}
	if traceDigest(t.Tenant, t.IntentRef, t.Field, t.Nodes) != t.Digest {
		return fmt.Errorf("lineage: Verify: %w", ErrBrokenChain)
	}
	return nil
}

// At returns the attributed node for one stage.
func (t Trace) At(stage Stage) (Node, error) {
	for _, node := range t.Nodes {
		if node.Stage == stage {
			return node, nil
		}
	}
	return Node{}, fmt.Errorf("lineage: At %s: %w", stage, ErrIncompleteTrace)
}
