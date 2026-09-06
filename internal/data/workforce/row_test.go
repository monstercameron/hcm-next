package workforce_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/workforce"
)

// TestWorkerRowValidateAcceptsACompleteRow is the positive half of the row
// contract: everything [newRow] builds is insertable.
func TestWorkerRowValidateAcceptsACompleteRow(t *testing.T) {
	t.Parallel()
	if err := newRow(uuid.New(), "complete-1").Validate(); err != nil {
		t.Fatalf("Validate() on a complete row: %v", err)
	}
}

// TestWorkerRowValidateNamesTheMissingField walks every refusal Validate
// declares. Each case asserts the message names the field, because an
// append-only table's refusal is the caller's only chance to fix the row.
func TestWorkerRowValidateNamesTheMissingField(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mutate func(*workforce.WorkerRow)
		names  string
	}{
		"nil tenant":         {func(r *workforce.WorkerRow) { r.TenantID = uuid.Nil }, "tenant"},
		"nil worker":         {func(r *workforce.WorkerRow) { r.WorkerID = uuid.Nil }, "worker id"},
		"blank key":          {func(r *workforce.WorkerRow) { r.WorkerKey = "  " }, "worker_key"},
		"blank preferred":    {func(r *workforce.WorkerRow) { r.PreferredName = "" }, "preferred_name"},
		"blank number":       {func(r *workforce.WorkerRow) { r.WorkerNumber = "" }, "worker_number"},
		"blank type":         {func(r *workforce.WorkerRow) { r.WorkerType = "" }, "worker_type"},
		"blank lifecycle":    {func(r *workforce.WorkerRow) { r.LifecycleStatus = "" }, "lifecycle_status"},
		"blank employment":   {func(r *workforce.WorkerRow) { r.EmploymentID = "" }, "employment_id"},
		"blank assignment":   {func(r *workforce.WorkerRow) { r.AssignmentID = "" }, "assignment_id"},
		"blank grade":        {func(r *workforce.WorkerRow) { r.Grade = "" }, "grade"},
		"blank org unit":     {func(r *workforce.WorkerRow) { r.OrgUnit = "" }, "org_unit"},
		"blank position":     {func(r *workforce.WorkerRow) { r.PositionID = "" }, "position_id"},
		"blank location":     {func(r *workforce.WorkerRow) { r.Location = "" }, "location"},
		"blank pay zone":     {func(r *workforce.WorkerRow) { r.PayZone = "" }, "pay_zone"},
		"blank fte":          {func(r *workforce.WorkerRow) { r.FTE = "" }, "fte"},
		"blank manager":      {func(r *workforce.WorkerRow) { r.ManagerRelationshipRef = "" }, "manager_relationship_ref"},
		"blank base pay":     {func(r *workforce.WorkerRow) { r.BasePay = "" }, "base_pay"},
		"blank pay basis":    {func(r *workforce.WorkerRow) { r.PayBasis = "" }, "pay_basis"},
		"blank bonus":        {func(r *workforce.WorkerRow) { r.BonusTarget = "" }, "bonus_target"},
		"blank stream":       {func(r *workforce.WorkerRow) { r.RevisionStream = "" }, "revision_stream"},
		"blank creator":      {func(r *workforce.WorkerRow) { r.CreatedBy = "" }, "created_by"},
		"short currency":     {func(r *workforce.WorkerRow) { r.Currency = "US" }, "currency"},
		"lowercase currency": {func(r *workforce.WorkerRow) { r.Currency = "usd" }, "currency"},
		"bad effective":      {func(r *workforce.WorkerRow) { r.EffectiveFrom = "2021-13-40" }, "effective_from"},
		"zero known at":      {func(r *workforce.WorkerRow) { r.KnownAt = time.Time{} }, "known_at"},
		"zero recorded at":   {func(r *workforce.WorkerRow) { r.RecordedAt = time.Time{} }, "recorded_at"},
		"unknown source":     {func(r *workforce.WorkerRow) { r.Source = "IMPORTED" }, "source"},
	} {
		t.Run(name, func(t *testing.T) {
			row := newRow(uuid.New(), "row-"+uuid.NewString())
			tc.mutate(&row)
			err := row.Validate()
			if !errors.Is(err, workforce.ErrInvalidRow) {
				t.Fatalf("Validate() = %v, want ErrInvalidRow", err)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("Validate() = %q, want it to name %q", err, tc.names)
			}
		})
	}
}

// TestWorkerRowDisplayNameFallsBackToTheLegalName proves the one naming rule
// this type owns, so no surface re-decides it.
func TestWorkerRowDisplayNameFallsBackToTheLegalName(t *testing.T) {
	t.Parallel()
	row := newRow(uuid.New(), "display-1")
	if got := row.DisplayName(); got != "Ada" {
		t.Errorf("DisplayName() = %q, want the preferred name", got)
	}
	row.PreferredName = "   "
	if got := row.DisplayName(); got != "Ada Lovelace" {
		t.Errorf("DisplayName() with no preferred name = %q, want the legal name", got)
	}
}

// TestWorkerRowSourceIsClosed pins the one source token that exists. A second
// population would add its own beside it, and this test is what makes that a
// deliberate change rather than a silent one.
func TestWorkerRowSourceIsClosed(t *testing.T) {
	t.Parallel()
	if workforce.SourceCreated != "CREATED" {
		t.Fatalf("SourceCreated = %q, want CREATED", workforce.SourceCreated)
	}
}
