// Package trace proves the canonical Promotion logical-persistence trace
// (PROMO-008): one deterministic T0…Tn walk from intent creation through
// snapshot, simulation, proposal, approvals, revalidation, reservations,
// one atomic local commit, external observations, degraded completion,
// targeted repair and final closure. Logical table names are contracts,
// not a physical layout: rows assert exact inserts, transitions and
// zero-forbidden-row counts, and every row chains lineage back to the
// intent. Scarcity fencing reuses the shared reservation protocol.
package trace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/resource/reservation"
)

// Sentinel causes. Classify with errors.Is.
var (
	// ErrStageOrder reports a lifecycle step taken outside its permitted
	// boundary.
	ErrStageOrder = errors.New("trace: stage out of order")

	// ErrStaleProposal reports a commit or revalidation naming a proposal
	// digest that is not the approved one.
	ErrStaleProposal = errors.New("trace: proposal changed after approval")

	// ErrCloseWhileRepairOpen reports closing the intent while external
	// repair is still required.
	ErrCloseWhileRepairOpen = errors.New("trace: intent closes while repair is required")

	// ErrPartialCommit reports a commit failpoint firing before the batch
	// landed: no partial rows may exist.
	ErrPartialCommit = errors.New("trace: commit did not land atomically")
)

// Logical tables. Names are contracts; the trace never implies a physical
// layout.
const (
	TableIntent         = "intent"
	TableSnapshot       = "snapshot"
	TableSimulation     = "simulation"
	TableProposal       = "proposal"
	TableApproval       = "approval"
	TableRevalidation   = "revalidation"
	TableReservation    = "reservation"
	TablePositionEdge   = "position_edge"
	TableCompComponent  = "comp_component"
	TableLedger         = "ledger_entry"
	TableObservation    = "observation"
	TableReconciliation = "reconciliation"
	TableRepair         = "repair"
	TableClosure        = "closure"
)

// Row is one logical insert with its tick, lineage chain and detail.
type Row struct {
	Tick       int
	Table      string
	ID         string
	State      string
	Revision   uint64
	IntentRef  string
	Digest     string
	PrevDigest string
	Detail     string
}

// Tracer walks one promotion through its closed lifecycle, appending one
// tick per stage. The zero value is invalid: build with New.
type Tracer struct {
	intentRef   string
	workerID    string
	oldManager  string
	newManager  string
	proposal    string
	approvals   []string
	stage       string
	tick        int
	rows        []Row
	commitIDs   map[string]bool
	positionRev uint64
	failpoint   func(stage string) error
	holds       []reservation.Reservation
	now         time.Time
}

const (
	stageIntent    = "INTENT"
	stageSnapshot  = "SNAPSHOT"
	stageSimulated = "SIMULATED"
	stageProposed  = "PROPOSED"
	stageApproved  = "APPROVED"
	stageRevalid   = "REVALIDATED"
	stageReserved  = "RESERVED"
	stageCommitted = "COMMITTED"
	stageObserved  = "OBSERVED"
	stageDegraded  = "DEGRADED"
	stageRepaired  = "REPAIRED"
	stageClosed    = "CLOSED"
)

// New starts one promotion trace at T0.
func New(intentRef, workerID, oldManager, newManager string, now time.Time) *Tracer {
	return &Tracer{
		intentRef: intentRef, workerID: workerID,
		oldManager: oldManager, newManager: newManager,
		commitIDs: make(map[string]bool), now: now,
	}
}

// Failpoint arms a test hook fired before the commit batch lands.
func (t *Tracer) Failpoint(fn func(stage string) error) { t.failpoint = fn }

func rowDigest(intentRef string, tick int, table, id, state, prev, detail string, revision uint64) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		"promotion-trace", intentRef, fmt.Sprintf("T%d", tick), table, id, state, prev, detail,
		fmt.Sprintf("rev=%d", revision),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (t *Tracer) append(table, id, state, detail string, revision uint64) Row {
	t.tick++
	prev := ""
	if len(t.rows) > 0 {
		prev = t.rows[len(t.rows)-1].Digest
	}
	row := Row{
		Tick: t.tick, Table: table, ID: id, State: state, Revision: revision,
		IntentRef: t.intentRef, PrevDigest: prev, Detail: detail,
	}
	row.Digest = rowDigest(t.intentRef, row.Tick, table, id, state, prev, detail, revision)
	t.rows = append(t.rows, row)
	return row
}

func (t *Tracer) require(stage string) error {
	if t.stage != stage {
		return fmt.Errorf("trace: want %s, at %s: %w", stage, t.stage, ErrStageOrder)
	}
	return nil
}

// Rows returns the full logical trace.
func (t *Tracer) Rows() []Row { return append([]Row(nil), t.rows...) }

// Count reports exact logical inserts for one table.
func (t *Tracer) Count(table string) int {
	count := 0
	for _, row := range t.rows {
		if row.Table == table {
			count++
		}
	}
	return count
}

// CountState reports rows for one table in one state.
func (t *Tracer) CountState(table, state string) int {
	count := 0
	for _, row := range t.rows {
		if row.Table == table && row.State == state {
			count++
		}
	}
	return count
}

// Seal is the head digest of the trace.
func (t *Tracer) Seal() string {
	if len(t.rows) == 0 {
		return ""
	}
	return t.rows[len(t.rows)-1].Digest
}

// CreateIntent opens the promotion intent at T1.
func (t *Tracer) CreateIntent() error {
	if t.stage != "" {
		return fmt.Errorf("trace: CreateIntent: %w", ErrStageOrder)
	}
	t.append(TableIntent, t.intentRef, "OPEN", "worker="+t.workerID, 1)
	t.stage = stageIntent
	return nil
}

// Snapshot freezes the mixed-authority read at T2.
func (t *Tracer) Snapshot(digest string) error {
	if err := t.require(stageIntent); err != nil {
		return err
	}
	t.append(TableSnapshot, "snapshot/"+t.intentRef, "FROZEN", "digest="+digest, 1)
	t.stage = stageSnapshot
	return nil
}

// Simulate records the zero-effect simulation at T3.
func (t *Tracer) Simulate(digest string) error {
	if err := t.require(stageSnapshot); err != nil {
		return err
	}
	t.append(TableSimulation, "simulation/"+t.intentRef, "ZERO_EFFECT", "digest="+digest, 1)
	t.stage = stageSimulated
	return nil
}

// Propose files the immutable proposal at T4.
func (t *Tracer) Propose(revision uint64, digest string) error {
	if err := t.require(stageSimulated); err != nil {
		return err
	}
	t.proposal = digest
	t.append(TableProposal, "proposal/"+t.intentRef, "FILED", fmt.Sprintf("rev=%d digest=%s", revision, digest), revision)
	t.stage = stageProposed
	return nil
}

// Approve records the governed approvals at T5: the current manager and
// the HR partner, both bound to the filed proposal.
func (t *Tracer) Approve(approvals ...string) error {
	if err := t.require(stageProposed); err != nil {
		return err
	}
	if len(approvals) < 2 {
		return fmt.Errorf("trace: Approve needs quorum: %w", ErrStageOrder)
	}
	for _, approver := range approvals {
		t.append(TableApproval, "approval/"+approver, "GRANTED", "proposal="+t.proposal, 1)
		t.approvals = append(t.approvals, approver)
	}
	t.stage = stageApproved
	return nil
}

// Revalidate rechecks the proposal binding and versions at T6.
func (t *Tracer) Revalidate(proposalDigest string, graphVersion, authorityVersion uint64) error {
	if err := t.require(stageApproved); err != nil {
		return err
	}
	if proposalDigest != t.proposal {
		return fmt.Errorf("trace: Revalidate: %w", ErrStaleProposal)
	}
	t.append(TableRevalidation, "revalidation/"+t.intentRef, "CURRENT",
		fmt.Sprintf("proposal=%s graph=%d authority=%d", proposalDigest, graphVersion, authorityVersion), 1)
	t.stage = stageRevalid
	return nil
}

// Reserve fences the budget and capacity holds at T7 through the shared
// reservation protocol. Amounts are integer thousandths, never float.
func (t *Tracer) Reserve(store *reservation.Store, budgetQty, capacityQty reservation.Quantity) error {
	if err := t.require(stageRevalid); err != nil {
		return err
	}
	interval := reservation.Interval{From: t.now, To: t.now.Add(30 * 24 * time.Hour)}
	budget, err := store.Acquire(reservation.Request{
		Resource: "budget/" + t.intentRef, Version: 1, Quantity: budgetQty, Interval: interval,
		Owner: t.workerID, Priority: 1, ExpiresAt: t.now.Add(time.Hour),
		ProposalDigest:  reservation.Digest([]byte(t.proposal + ":budget")),
		AuthorityDigest: reservation.Digest([]byte("authority/" + t.intentRef)),
		IdempotencyKey:  "budget-" + t.intentRef,
	}, budgetQty, t.now)
	if err != nil {
		return fmt.Errorf("trace: Reserve budget: %w", err)
	}
	capacity, err := store.Acquire(reservation.Request{
		Resource: "capacity/" + t.intentRef, Version: 1, Quantity: capacityQty, Interval: interval,
		Owner: t.workerID, Priority: 1, ExpiresAt: t.now.Add(time.Hour),
		ProposalDigest:  reservation.Digest([]byte(t.proposal + ":capacity")),
		AuthorityDigest: reservation.Digest([]byte("authority/" + t.intentRef)),
		IdempotencyKey:  "capacity-" + t.intentRef,
	}, capacityQty, t.now)
	if err != nil {
		return fmt.Errorf("trace: Reserve capacity: %w", err)
	}
	t.holds = []reservation.Reservation{budget, capacity}
	t.append(TableReservation, "hold/budget", "HELD", "proposal="+t.proposal, 1)
	t.append(TableReservation, "hold/capacity", "HELD", "proposal="+t.proposal, 1)
	t.stage = stageReserved
	return nil
}

// Commit lands the atomic local promotion at T8: the old manager edge
// supersedes by revision (never overwritten), the new edge, the base-pay
// component and the ledger entries land as one batch while both holds
// consume. A commit ID replay returns the sealed batch without new rows.
func (t *Tracer) Commit(store *reservation.Store, commitID string) error {
	if t.commitIDs[commitID] {
		return nil
	}
	if err := t.require(stageReserved); err != nil {
		return err
	}
	if t.failpoint != nil {
		if err := t.failpoint("commit-append"); err != nil {
			return fmt.Errorf("trace: Commit: %w", err)
		}
	}
	for _, hold := range t.holds {
		if _, err := store.Consume(hold.ID, hold.Fence, t.now); err != nil {
			return fmt.Errorf("trace: Commit consume: %w", err)
		}
	}
	t.positionRev++
	batch := []struct {
		table, id, state, detail string
		revision                 uint64
	}{
		{TablePositionEdge, "edge/" + t.workerID + "/old", "SUPERSEDED", "manager=" + t.oldManager, t.positionRev},
		{TablePositionEdge, "edge/" + t.workerID + "/new", "ACTIVE", "manager=" + t.newManager, t.positionRev},
		{TableCompComponent, "comp/" + t.workerID + "/base-pay", "ACTIVE", "proposal=" + t.proposal, 1},
		{TableLedger, "ledger/promotion", "APPENDED", "commit=" + commitID, 1},
		{TableLedger, "ledger/compensation", "APPENDED", "commit=" + commitID, 1},
	}
	for _, row := range batch {
		t.append(row.table, row.id, row.state, row.detail, row.revision)
	}
	t.commitIDs[commitID] = true
	t.stage = stageCommitted
	return nil
}

// Observe records the external observations at T9: the HRIS applies the
// manager and base-pay effects while IAM provisioning fails, so the trace
// completes degraded with repair required and the intent stays open.
func (t *Tracer) Observe(iamApplied bool) error {
	if err := t.require(stageCommitted); err != nil {
		return err
	}
	t.append(TableObservation, "obs/manager-edge", "APPLIED", "external=hris-1", 1)
	t.append(TableObservation, "obs/base-pay", "APPLIED", "external=hris-2", 1)
	if iamApplied {
		t.append(TableObservation, "obs/iam", "APPLIED", "external=iam-1", 1)
		t.stage = stageObserved
		return nil
	}
	t.append(TableObservation, "obs/iam", "FAILED", "external=iam-1", 1)
	t.append(TableReconciliation, "recon/"+t.intentRef, "REPAIR_REQUIRED", "effect=obs/iam", 1)
	t.append(TableIntent, t.intentRef, "DEGRADED", "repair-required", 2)
	t.stage = stageDegraded
	return nil
}

// RepairIAM redrives only the failed IAM effect at T10: fresh observation,
// targeted repair, no rerun of any committed promotion mutation.
func (t *Tracer) RepairIAM() error {
	if err := t.require(stageDegraded); err != nil {
		return err
	}
	edges, comps := t.Count(TablePositionEdge), t.Count(TableCompComponent)
	t.append(TableRepair, "repair/iam", "REDIVEN", "effect=obs/iam", 1)
	t.append(TableObservation, "obs/iam-retry", "APPLIED", "external=iam-2", 1)
	if t.Count(TablePositionEdge) != edges || t.Count(TableCompComponent) != comps {
		return fmt.Errorf("trace: RepairIAM reran promotion mutations: %w", ErrPartialCommit)
	}
	t.append(TableIntent, t.intentRef, "REPAIRED", "repair=repair/iam", 3)
	t.stage = stageRepaired
	return nil
}

// Close files the terminal closure at T11. Closing while repair is
// required is refused: degraded completion never closes the intent.
func (t *Tracer) Close() error {
	if t.stage == stageDegraded {
		return fmt.Errorf("trace: Close: %w", ErrCloseWhileRepairOpen)
	}
	if err := t.require(stageRepaired); err != nil {
		return err
	}
	t.append(TableClosure, "closure/"+t.intentRef, "CLOSED", "terminal=closed", 1)
	t.append(TableIntent, t.intentRef, "CLOSED", "terminal=closed", 4)
	t.stage = stageClosed
	return nil
}

// Resume rebuilds a tracer from persisted rows after a crash: the stage
// resumes from the last lifecycle state, ticks continue, and committed
// commit IDs stay replay-safe.
func Resume(intentRef, workerID, oldManager, newManager string, now time.Time, rows []Row) *Tracer {
	tracer := New(intentRef, workerID, oldManager, newManager, now)
	for _, row := range rows {
		tracer.rows = append(tracer.rows, row)
		if row.Tick > tracer.tick {
			tracer.tick = row.Tick
		}
		if row.Table == TableProposal {
			for _, field := range strings.Split(row.Detail, " ") {
				if rest, ok := strings.CutPrefix(field, "digest="); ok {
					tracer.proposal = rest
				}
			}
		}
		if row.Table == TableLedger {
			for _, field := range strings.Split(row.Detail, " ") {
				if rest, ok := strings.CutPrefix(field, "commit="); ok {
					tracer.commitIDs[rest] = true
				}
			}
		}
		if row.Table == TablePositionEdge && row.Revision > tracer.positionRev {
			tracer.positionRev = row.Revision
		}
		if row.Table == TableIntent {
			switch row.State {
			case "OPEN":
				tracer.stage = stageIntent
			case "DEGRADED":
				tracer.stage = stageDegraded
			case "REPAIRED":
				tracer.stage = stageRepaired
			case "CLOSED":
				tracer.stage = stageClosed
			}
		}
		if row.Table == TableSnapshot {
			tracer.stage = stageSnapshot
		}
		if row.Table == TableSimulation {
			tracer.stage = stageSimulated
		}
		if row.Table == TableProposal {
			tracer.stage = stageProposed
		}
		if row.Table == TableApproval {
			tracer.stage = stageApproved
		}
		if row.Table == TableRevalidation {
			tracer.stage = stageRevalid
		}
		if row.Table == TableReservation {
			tracer.stage = stageReserved
		}
		if row.Table == TableLedger {
			tracer.stage = stageCommitted
		}
	}
	return tracer
}
