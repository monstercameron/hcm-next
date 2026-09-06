// Package subprocessor owns the pure governance decision for processor
// inventory changes. It produces a revision and activation fence; persistence,
// notices and provider calls remain composition-root responsibilities.
package subprocessor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const contractVersion = 1

func Version() int { return contractVersion }

type Processor struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Regions         []string `json:"regions"`
	Purposes        []string `json:"purposes"`
	DataCategories  []string `json:"data_categories"`
	Retention       string   `json:"retention"`
	ContractVersion string   `json:"contract_version"`
	ExitPlan        string   `json:"exit_plan"`
}

type Flow struct {
	ID             string   `json:"id"`
	TenantID       string   `json:"tenant_id"`
	IntentID       string   `json:"intent_id"`
	ProcessorID    string   `json:"processor_id"`
	Region         string   `json:"region"`
	Purpose        string   `json:"purpose"`
	DataCategories []string `json:"data_categories"`
	Retention      string   `json:"retention"`
}

type Inventory struct {
	Revision   uint64      `json:"revision"`
	Processors []Processor `json:"processors"`
	Flows      []Flow      `json:"flows"`
}

type Difference struct {
	Path   string `json:"path"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

type Review struct {
	Security  bool   `json:"security"`
	Privacy   bool   `json:"privacy"`
	Residency bool   `json:"residency"`
	Contract  bool   `json:"contract"`
	Evidence  string `json:"evidence"`
}

func (r Review) Complete() bool {
	return r.Security && r.Privacy && r.Residency && r.Contract && strings.TrimSpace(r.Evidence) != ""
}

type Notice struct {
	ID              string    `json:"id"`
	IssuedAt        time.Time `json:"issued_at"`
	ObjectionOpens  time.Time `json:"objection_opens"`
	ObjectionCloses time.Time `json:"objection_closes"`
	TenantIDs       []string  `json:"tenant_ids"`
}

func (n Notice) Valid() bool {
	return strings.TrimSpace(n.ID) != "" && !n.IssuedAt.IsZero() && !n.ObjectionOpens.IsZero() && !n.ObjectionCloses.IsZero() && n.ObjectionOpens.Before(n.ObjectionCloses) && !n.IssuedAt.After(n.ObjectionOpens) && len(n.TenantIDs) > 0
}

type Objection struct {
	TenantID    string    `json:"tenant_id"`
	Reason      string    `json:"reason"`
	Disposition string    `json:"disposition"`
	ResolvedAt  time.Time `json:"resolved_at"`
}

func (o Objection) Resolved() bool {
	return strings.TrimSpace(o.Disposition) != "" && !o.ResolvedAt.IsZero()
}

type EmergencyReplacement struct {
	IncidentRef          string    `json:"incident_ref"`
	RequestedBy          string    `json:"requested_by"`
	ApprovedBy           string    `json:"approved_by"`
	CompensatingControls []string  `json:"compensating_controls"`
	ExpiresAt            time.Time `json:"expires_at"`
}

type Change struct {
	Revision         uint64                `json:"revision"`
	BeforeDigest     string                `json:"before_digest"`
	AfterDigest      string                `json:"after_digest"`
	Material         bool                  `json:"material"`
	Differences      []Difference          `json:"differences"`
	AffectedTenants  []string              `json:"affected_tenants"`
	AffectedFlows    []string              `json:"affected_flows"`
	AffectedIntents  []string              `json:"affected_intents"`
	Reviews          Review                `json:"reviews"`
	Notice           Notice                `json:"notice"`
	Objections       []Objection           `json:"objections"`
	ContractApproved bool                  `json:"contract_approved"`
	ExitFallback     bool                  `json:"exit_fallback"`
	Emergency        *EmergencyReplacement `json:"emergency,omitempty"`
}

type Status string

const (
	StatusBlocked   Status = "BLOCKED"
	StatusActive    Status = "ACTIVE"
	StatusEmergency Status = "EMERGENCY_ACTIVE"
)

type ActivationReceipt struct {
	Allowed   bool   `json:"allowed"`
	Status    Status `json:"status"`
	Reason    string `json:"reason"`
	Revision  uint64 `json:"revision"`
	Digest    string `json:"digest"`
	Emergency bool   `json:"emergency"`
}

var (
	ErrInvalidInventory = errors.New("subprocessor: invalid inventory")
	ErrRevision         = errors.New("subprocessor: revision must increase")
	ErrNoticeRequired   = errors.New("subprocessor: scoped customer notice is required")
	ErrObjectionWindow  = errors.New("subprocessor: objection window is open")
	ErrObjectionPending = errors.New("subprocessor: objection is unresolved")
	ErrReviewRequired   = errors.New("subprocessor: security privacy residency and contract review required")
	ErrExitFallback     = errors.New("subprocessor: exit fallback is required")
	ErrEmergency        = errors.New("subprocessor: emergency replacement controls are invalid")
)

func ValidateInventory(in Inventory) error {
	if in.Revision == 0 || len(in.Processors) == 0 {
		return ErrInvalidInventory
	}
	processors := make(map[string]Processor, len(in.Processors))
	for _, p := range in.Processors {
		if !validID(p.ID) || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.ContractVersion) == "" || strings.TrimSpace(p.Retention) == "" || strings.TrimSpace(p.ExitPlan) == "" || !allNonEmpty(p.Regions) || !allNonEmpty(p.Purposes) || !allNonEmpty(p.DataCategories) {
			return fmt.Errorf("%w: processor %q is incomplete", ErrInvalidInventory, p.ID)
		}
		if _, exists := processors[p.ID]; exists {
			return fmt.Errorf("%w: duplicate processor %q", ErrInvalidInventory, p.ID)
		}
		processors[p.ID] = p
	}
	seenFlows := make(map[string]struct{}, len(in.Flows))
	for _, f := range in.Flows {
		if !validID(f.ID) || !validID(f.TenantID) || !validID(f.IntentID) || strings.TrimSpace(f.Purpose) == "" || strings.TrimSpace(f.Region) == "" || strings.TrimSpace(f.Retention) == "" || !allNonEmpty(f.DataCategories) {
			return fmt.Errorf("%w: flow %q is incomplete", ErrInvalidInventory, f.ID)
		}
		if _, exists := seenFlows[f.ID]; exists {
			return fmt.Errorf("%w: duplicate flow %q", ErrInvalidInventory, f.ID)
		}
		seenFlows[f.ID] = struct{}{}
		p, exists := processors[f.ProcessorID]
		if !exists || !contains(p.Regions, f.Region) || !contains(p.Purposes, f.Purpose) || !containsAll(p.DataCategories, f.DataCategories) || f.Retention != p.Retention {
			return fmt.Errorf("%w: flow %q is outside processor contract", ErrInvalidInventory, f.ID)
		}
	}
	return nil
}

func Digest(in Inventory) string {
	b, _ := json.Marshal(canonicalInventory(in))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Diff creates an immutable change revision and computes the impact graph.
func Diff(before, after Inventory) (Change, error) {
	if err := ValidateInventory(before); err != nil {
		return Change{}, err
	}
	if err := ValidateInventory(after); err != nil {
		return Change{}, err
	}
	if after.Revision <= before.Revision {
		return Change{}, ErrRevision
	}
	change := Change{Revision: after.Revision, BeforeDigest: Digest(before), AfterDigest: Digest(after)}
	change.Differences = differences(before, after)
	change.Material = len(change.Differences) > 0
	change.AffectedFlows, change.AffectedTenants, change.AffectedIntents = impact(before, after)
	return cloneChange(change), nil
}

// BuildChange is an explicit alias for callers that prefer plan terminology.
func BuildChange(before, after Inventory) (Change, error) { return Diff(before, after) }

func (c Change) Digest() string {
	b, _ := json.Marshal(canonicalChange(c))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func RecordObjection(c Change, objection Objection) (Change, error) {
	if strings.TrimSpace(objection.TenantID) == "" || strings.TrimSpace(objection.Reason) == "" || !contains(c.Notice.TenantIDs, objection.TenantID) {
		return Change{}, ErrInvalidInventory
	}
	for _, existing := range c.Objections {
		if existing.TenantID == objection.TenantID {
			return Change{}, ErrInvalidInventory
		}
	}
	c.Objections = append(append([]Objection(nil), c.Objections...), objection)
	sort.Slice(c.Objections, func(i, j int) bool { return c.Objections[i].TenantID < c.Objections[j].TenantID })
	return cloneChange(c), nil
}

func ResolveObjection(c Change, tenantID, disposition string, resolvedAt time.Time) (Change, error) {
	if strings.TrimSpace(disposition) == "" || resolvedAt.IsZero() {
		return Change{}, ErrInvalidInventory
	}
	for i := range c.Objections {
		if c.Objections[i].TenantID == tenantID {
			c.Objections[i].Disposition = disposition
			c.Objections[i].ResolvedAt = resolvedAt
			return cloneChange(c), nil
		}
	}
	return Change{}, ErrInvalidInventory
}

// Activate applies the fail-closed fence. It has no persistence or provider
// side effect and is therefore safe to use before a transaction boundary.
func Activate(c Change, now time.Time) (ActivationReceipt, error) {
	receipt := ActivationReceipt{Revision: c.Revision, Digest: c.Digest()}
	if !c.Material {
		receipt.Allowed, receipt.Status, receipt.Reason = true, StatusActive, "NO_MATERIAL_CHANGE"
		return receipt, nil
	}
	if !c.Reviews.Complete() {
		return blocked(receipt, ErrReviewRequired)
	}
	if !c.ContractApproved {
		return blocked(receipt, ErrReviewRequired)
	}
	if !c.ExitFallback {
		return blocked(receipt, ErrExitFallback)
	}
	emergency := c.Emergency != nil
	if emergency {
		if !validEmergency(*c.Emergency, now) {
			return blocked(receipt, ErrEmergency)
		}
	} else {
		if !c.Notice.Valid() {
			return blocked(receipt, ErrNoticeRequired)
		}
		for _, tenant := range c.AffectedTenants {
			if !contains(c.Notice.TenantIDs, tenant) {
				return blocked(receipt, ErrNoticeRequired)
			}
		}
		if now.Before(c.Notice.ObjectionCloses) {
			return blocked(receipt, ErrObjectionWindow)
		}
		for _, objection := range c.Objections {
			if !objection.Resolved() {
				return blocked(receipt, ErrObjectionPending)
			}
		}
	}
	receipt.Allowed = true
	receipt.Status = StatusActive
	receipt.Reason = "OBLIGATIONS_COMPLETE"
	receipt.Emergency = emergency
	if emergency {
		receipt.Status = StatusEmergency
		receipt.Reason = "EMERGENCY_CONTROLS_ACTIVE"
	}
	return receipt, nil
}

func Explain(c Change) string {
	status := "IMMATERIAL"
	if c.Material {
		status = "MATERIAL"
	}
	return fmt.Sprintf("subprocessor revision=%d status=%s differences=%d tenants=%d flows=%d digest=%s", c.Revision, status, len(c.Differences), len(c.AffectedTenants), len(c.AffectedFlows), c.Digest())
}

func blocked(r ActivationReceipt, err error) (ActivationReceipt, error) {
	r.Status, r.Reason = StatusBlocked, err.Error()
	return r, err
}

func validEmergency(e EmergencyReplacement, now time.Time) bool {
	return validID(e.IncidentRef) && validID(e.RequestedBy) && validID(e.ApprovedBy) && e.RequestedBy != e.ApprovedBy && allNonEmpty(e.CompensatingControls) && !e.ExpiresAt.IsZero() && e.ExpiresAt.After(now) && e.ExpiresAt.Sub(now) <= 72*time.Hour
}

func differences(before, after Inventory) []Difference {
	var out []Difference
	canonicalBefore, canonicalAfter := canonicalInventory(before), canonicalInventory(after)
	bp, ap := indexProcessors(canonicalBefore), indexProcessors(canonicalAfter)
	for _, id := range unionKeys(bp, ap) {
		b, bok := bp[id]
		a, aok := ap[id]
		if !bok {
			out = append(out, Difference{Path: "processor:" + id, After: mustJSON(a)})
		} else if !aok {
			out = append(out, Difference{Path: "processor:" + id, Before: mustJSON(b)})
		} else if mustJSON(b) != mustJSON(a) {
			out = append(out, Difference{Path: "processor:" + id, Before: mustJSON(b), After: mustJSON(a)})
		}
	}
	bf, af := indexFlows(canonicalBefore), indexFlows(canonicalAfter)
	for _, id := range unionKeys(bf, af) {
		b, bok := bf[id]
		a, aok := af[id]
		if !bok {
			out = append(out, Difference{Path: "flow:" + id, After: mustJSON(a)})
		} else if !aok {
			out = append(out, Difference{Path: "flow:" + id, Before: mustJSON(b)})
		} else if mustJSON(b) != mustJSON(a) {
			out = append(out, Difference{Path: "flow:" + id, Before: mustJSON(b), After: mustJSON(a)})
		}
	}
	return out
}

func impact(before, after Inventory) ([]string, []string, []string) {
	diffs := differences(before, after)
	changed := make(map[string]struct{}, len(diffs))
	for _, d := range diffs {
		parts := strings.SplitN(d.Path, ":", 2)
		if len(parts) == 2 {
			changed[parts[1]] = struct{}{}
		}
	}
	flows := make(map[string]Flow)
	for _, f := range append(append([]Flow(nil), before.Flows...), after.Flows...) {
		if _, ok := changed[f.ID]; ok {
			flows[f.ID] = f
			continue
		}
		if _, ok := changed[f.ProcessorID]; ok {
			flows[f.ID] = f
		}
	}
	tenantSet, intentSet, flowSet := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for id, f := range flows {
		flowSet[id] = struct{}{}
		tenantSet[f.TenantID] = struct{}{}
		intentSet[f.IntentID] = struct{}{}
	}
	return sortedKeys(flowSet), sortedKeys(tenantSet), sortedKeys(intentSet)
}

func canonicalInventory(in Inventory) Inventory {
	out := in
	out.Processors = append([]Processor(nil), in.Processors...)
	out.Flows = append([]Flow(nil), in.Flows...)
	for i := range out.Processors {
		out.Processors[i].Regions = sortedCopy(out.Processors[i].Regions)
		out.Processors[i].Purposes = sortedCopy(out.Processors[i].Purposes)
		out.Processors[i].DataCategories = sortedCopy(out.Processors[i].DataCategories)
	}
	for i := range out.Flows {
		out.Flows[i].DataCategories = sortedCopy(out.Flows[i].DataCategories)
	}
	sort.Slice(out.Processors, func(i, j int) bool { return out.Processors[i].ID < out.Processors[j].ID })
	sort.Slice(out.Flows, func(i, j int) bool { return out.Flows[i].ID < out.Flows[j].ID })
	return out
}

func canonicalChange(in Change) Change {
	out := cloneChange(in)
	sort.Slice(out.Differences, func(i, j int) bool { return out.Differences[i].Path < out.Differences[j].Path })
	sort.Slice(out.AffectedTenants, func(i, j int) bool { return out.AffectedTenants[i] < out.AffectedTenants[j] })
	sort.Slice(out.AffectedFlows, func(i, j int) bool { return out.AffectedFlows[i] < out.AffectedFlows[j] })
	sort.Slice(out.AffectedIntents, func(i, j int) bool { return out.AffectedIntents[i] < out.AffectedIntents[j] })
	sort.Slice(out.Objections, func(i, j int) bool { return out.Objections[i].TenantID < out.Objections[j].TenantID })
	return out
}

func cloneChange(in Change) Change {
	out := in
	out.Differences = append([]Difference(nil), in.Differences...)
	out.AffectedTenants = append([]string(nil), in.AffectedTenants...)
	out.AffectedFlows = append([]string(nil), in.AffectedFlows...)
	out.AffectedIntents = append([]string(nil), in.AffectedIntents...)
	out.Objections = append([]Objection(nil), in.Objections...)
	out.Notice.TenantIDs = append([]string(nil), in.Notice.TenantIDs...)
	if in.Emergency != nil {
		e := *in.Emergency
		e.CompensatingControls = append([]string(nil), in.Emergency.CompensatingControls...)
		out.Emergency = &e
	}
	return out
}

func indexProcessors(in Inventory) map[string]Processor {
	out := map[string]Processor{}
	for _, v := range in.Processors {
		out[v.ID] = v
	}
	return out
}
func indexFlows(in Inventory) map[string]Flow {
	out := map[string]Flow{}
	for _, v := range in.Flows {
		out[v.ID] = v
	}
	return out
}
func unionKeys[A any](left, right map[string]A) []string {
	set := map[string]struct{}{}
	for k := range left {
		set[k] = struct{}{}
	}
	for k := range right {
		set[k] = struct{}{}
	}
	return sortedKeys(set)
}
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func containsAll(values, required []string) bool {
	for _, value := range required {
		if !contains(values, value) {
			return false
		}
	}
	return true
}

func validID(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, " \t\r\n")
}

func allNonEmpty(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}
func mustJSON(value any) string { b, _ := json.Marshal(value); return string(b) }
