package aggregates

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Fixture corpus, copied verbatim from internal/domains/fixtures/testdata
// (this package does not import internal/domains/fixtures -- see doc.go's
// import-boundary note). LoadFixtures parses it independently and appends
// the corresponding rows across all fifteen DB-008/009/010 tables in one
// tenant, so DB-019's seeder and the promotion domain can read real rows
// through this adapter rather than hand-building fixtures a second time.

//go:embed testdata/workers.json
var fixtureWorkersJSON []byte

//go:embed testdata/bands.json
var fixtureBandsJSON []byte

//go:embed testdata/legacy-compensation-scenarios.json
var fixtureLegacyScenariosJSON []byte

type fixtureWorkerFile struct {
	Calendar struct {
		Ref     string `json:"ref"`
		Version string `json:"version"`
	} `json:"calendar"`
	Workers []fixtureWorker `json:"workers"`
}

type fixtureWorker struct {
	Key                    string `json:"key"`
	ID                     string `json:"id"`
	WorkerNumber           string `json:"worker_number"`
	LifecycleStatus        string `json:"lifecycle_status"`
	EmploymentStatus       string `json:"employment_status"`
	LegalName              string `json:"legal_name"`
	PreferredName          string `json:"preferred_name"`
	EmploymentID           string `json:"employment_id"`
	LegalEntity            string `json:"legal_entity"`
	WorkerType             string `json:"worker_type"`
	HireDate               string `json:"hire_date"`
	AssignmentID           string `json:"assignment_id"`
	JobCode                string `json:"job_code"`
	Grade                  string `json:"grade"`
	OrgUnit                string `json:"org_unit"`
	PositionID             string `json:"position_id"`
	Location               string `json:"location"`
	PayZone                string `json:"pay_zone"`
	FTE                    string `json:"fte"`
	ManagerRelationshipRef string `json:"manager_relationship_ref"`
	EffectiveFrom          string `json:"effective_from"`
	KnownAt                string `json:"known_at"`
	RecordedAt             string `json:"recorded_at"`
}

type fixtureBandFile struct {
	Bands []fixtureBand `json:"bands"`
}

type fixtureBand struct {
	ID       string `json:"id"`
	JobCode  string `json:"job_code"`
	Grade    string `json:"grade"`
	PayZone  string `json:"pay_zone"`
	Currency string `json:"currency"`
	Minimum  string `json:"minimum"`
	Midpoint string `json:"midpoint"`
	Maximum  string `json:"maximum"`
}

type fixtureLegacyScenarioSet struct {
	Worker string `json:"worker"`
	Target struct {
		JobCode string `json:"job_code"`
		Grade   string `json:"grade"`
		PayZone string `json:"pay_zone"`
	} `json:"target"`
	BudgetAvailable string                  `json:"budget_available"`
	Scenarios       []fixtureLegacyScenario `json:"scenarios"`
}

type fixtureLegacyScenario struct {
	CurrentAmount   string `json:"current_amount"`
	CurrentCurrency string `json:"current_currency"`
	EffectiveAt     string `json:"effective_at"`
}

// LoadedFixtures names every entity id the loader created, keyed by the
// fixture's own natural key, so a test or seeder can look a row back up
// through PeopleStore/OrganizationStore/CompensationStore without having to
// re-derive ids.
type LoadedFixtures struct {
	Tenant uuid.UUID

	PersonID       map[string]uuid.UUID // by worker key
	WorkerID       map[string]uuid.UUID // by worker key
	EmploymentID   map[string]uuid.UUID // by worker key
	AssignmentID   map[string]uuid.UUID // by worker key
	LegalEntityID  map[string]uuid.UUID // by legal entity name
	OrganizationID map[string]uuid.UUID // by org_unit code
	JobID          map[string]uuid.UUID // by job code
	PositionID     map[string]uuid.UUID // by position code
	OccupancyID    map[string]uuid.UUID // by worker key
	BandID         map[string]uuid.UUID // by band id

	BudgetID    uuid.UUID // COMPENSATION_POOL budget seeded from the legacy scenario corpus
	PackageID   uuid.UUID // the legacy scenario worker's compensation package
	ComponentID uuid.UUID // that package's BASE_PAY component
}

// LoadFixtures parses the embedded corpus and appends every row into tenant
// through PeopleStore, OrganizationStore and CompensationStore, in one
// transaction-scoped Executor supplied by the caller.
func LoadFixtures(ctx context.Context, ex Executor, tenant uuid.UUID) (*LoadedFixtures, error) {
	var workerFile fixtureWorkerFile
	if err := json.Unmarshal(fixtureWorkersJSON, &workerFile); err != nil {
		return nil, fmt.Errorf("aggregates: parse workers.json: %w", err)
	}
	var bandFile fixtureBandFile
	if err := json.Unmarshal(fixtureBandsJSON, &bandFile); err != nil {
		return nil, fmt.Errorf("aggregates: parse bands.json: %w", err)
	}
	var scenarios fixtureLegacyScenarioSet
	if err := json.Unmarshal(fixtureLegacyScenariosJSON, &scenarios); err != nil {
		return nil, fmt.Errorf("aggregates: parse legacy-compensation-scenarios.json: %w", err)
	}

	people := PeopleStore{}
	org := OrganizationStore{}
	comp := CompensationStore{}

	out := &LoadedFixtures{
		Tenant:         tenant,
		PersonID:       map[string]uuid.UUID{},
		WorkerID:       map[string]uuid.UUID{},
		EmploymentID:   map[string]uuid.UUID{},
		AssignmentID:   map[string]uuid.UUID{},
		LegalEntityID:  map[string]uuid.UUID{},
		OrganizationID: map[string]uuid.UUID{},
		JobID:          map[string]uuid.UUID{},
		PositionID:     map[string]uuid.UUID{},
		OccupancyID:    map[string]uuid.UUID{},
		BandID:         map[string]uuid.UUID{},
	}

	for _, w := range workerFile.Workers {
		effectiveFrom, err := parseDate(w.EffectiveFrom)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s effective_from: %w", w.Key, err)
		}
		recordedAt, err := parseInstant(w.RecordedAt)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s recorded_at: %w", w.Key, err)
		}
		hireDate, err := parseDate(w.HireDate)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s hire_date: %w", w.Key, err)
		}

		legalEntityRef, err := out.ensureLegalEntity(ctx, org, ex, w.LegalEntity, effectiveFrom, recordedAt)
		if err != nil {
			return nil, err
		}
		organizationRef, err := out.ensureOrganizationUnit(ctx, org, ex, w.OrgUnit, legalEntityRef, effectiveFrom, recordedAt)
		if err != nil {
			return nil, err
		}
		jobRef, err := out.ensureJob(ctx, org, ex, w.JobCode, w.Grade, effectiveFrom, recordedAt)
		if err != nil {
			return nil, err
		}
		positionRef, err := out.ensureJobPosition(ctx, org, ex, w.PositionID, jobRef, organizationRef, legalEntityRef,
			w.Location, w.FTE, effectiveFrom, recordedAt)
		if err != nil {
			return nil, err
		}

		personID := uuid.New()
		person, err := NewPerson(tenant, personID, effectiveFrom, nil, recordedAt, "ACTIVE", w.LegalName, w.PreferredName)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s person: %w", w.Key, err)
		}
		if _, err := people.PutPerson(ctx, ex, person); err != nil {
			return nil, fmt.Errorf("aggregates: worker %s put person: %w", w.Key, err)
		}
		out.PersonID[w.Key] = personID

		workerID := uuid.New()
		worker, err := NewWorker(tenant, workerID, personID, effectiveFrom, nil, recordedAt,
			w.WorkerNumber, mapWorkerType(w.WorkerType), mapLifecycleStatus(w.LifecycleStatus))
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s worker: %w", w.Key, err)
		}
		if _, err := people.PutWorker(ctx, ex, worker); err != nil {
			return nil, fmt.Errorf("aggregates: worker %s put worker: %w", w.Key, err)
		}
		out.WorkerID[w.Key] = workerID

		var hireDatePtr *time.Time
		if !hireDate.IsZero() {
			hireDatePtr = &hireDate
		}
		employmentID := uuid.New()
		employment, err := NewEmployment(tenant, employmentID, workerID, legalEntityRef, effectiveFrom, nil, recordedAt,
			mapWorkerType(w.WorkerType), mapEmploymentStatus(w.EmploymentStatus), hireDatePtr)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s employment: %w", w.Key, err)
		}
		if _, err := people.PutEmployment(ctx, ex, employment); err != nil {
			return nil, fmt.Errorf("aggregates: worker %s put employment: %w", w.Key, err)
		}
		out.EmploymentID[w.Key] = employmentID

		assignmentID := uuid.New()
		assignment, err := NewAssignment(tenant, assignmentID, employmentID, true, effectiveFrom, nil, recordedAt,
			w.JobCode, w.Grade, &organizationRef, &positionRef, w.Location, w.PayZone, w.FTE, w.ManagerRelationshipRef)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s assignment: %w", w.Key, err)
		}
		if _, err := people.PutAssignment(ctx, ex, assignment); err != nil {
			return nil, fmt.Errorf("aggregates: worker %s put assignment: %w", w.Key, err)
		}
		out.AssignmentID[w.Key] = assignmentID

		occupancyID := uuid.New()
		occupancy, err := NewPositionOccupancy(tenant, occupancyID, positionRef, &assignmentID, &workerID,
			effectiveFrom, nil, recordedAt, w.FTE, true)
		if err != nil {
			return nil, fmt.Errorf("aggregates: worker %s position_occupancy: %w", w.Key, err)
		}
		if _, err := org.PutPositionOccupancy(ctx, ex, occupancy); err != nil {
			return nil, fmt.Errorf("aggregates: worker %s put position_occupancy: %w", w.Key, err)
		}
		out.OccupancyID[w.Key] = occupancyID
	}

	catalogRecordedAt := time.Now().UTC()
	for _, b := range bandFile.Bands {
		minimum, err := values.NewMoney(b.Minimum, b.Currency, 2, values.RoundingHalfEven)
		if err != nil {
			return nil, fmt.Errorf("aggregates: band %s minimum: %w", b.ID, err)
		}
		midpoint, err := values.NewMoney(b.Midpoint, b.Currency, 2, values.RoundingHalfEven)
		if err != nil {
			return nil, fmt.Errorf("aggregates: band %s midpoint: %w", b.ID, err)
		}
		maximum, err := values.NewMoney(b.Maximum, b.Currency, 2, values.RoundingHalfEven)
		if err != nil {
			return nil, fmt.Errorf("aggregates: band %s maximum: %w", b.ID, err)
		}
		bandID := uuid.New()
		band, err := NewCompensationBand(tenant, bandID, catalogRecordedAt, nil, catalogRecordedAt,
			b.JobCode, b.Grade, b.PayZone, minimum, midpoint, maximum)
		if err != nil {
			return nil, fmt.Errorf("aggregates: band %s: %w", b.ID, err)
		}
		if _, err := comp.PutCompensationBand(ctx, ex, band); err != nil {
			return nil, fmt.Errorf("aggregates: put band %s: %w", b.ID, err)
		}
		out.BandID[b.ID] = bandID
	}

	if scenarios.Worker != "" {
		workerRef, ok := out.WorkerID[scenarios.Worker]
		if !ok {
			return nil, fmt.Errorf("aggregates: legacy scenario worker %q was not in workers.json", scenarios.Worker)
		}

		budgetID := uuid.New()
		budget, err := NewWorkforceBudget(tenant, budgetID, catalogRecordedAt, nil, catalogRecordedAt,
			"COMPENSATION_POOL", "hcmnext.rewards.catalog", scenarios.Target.JobCode+"/"+scenarios.Target.Grade,
			"legacy-scenario-cycle", "USD", "MONEY", scenarios.BudgetAvailable, "")
		if err != nil {
			return nil, fmt.Errorf("aggregates: legacy scenario budget: %w", err)
		}
		if _, err := comp.PutWorkforceBudget(ctx, ex, budget); err != nil {
			return nil, fmt.Errorf("aggregates: put legacy scenario budget: %w", err)
		}
		out.BudgetID = budgetID

		if len(scenarios.Scenarios) > 0 {
			first := scenarios.Scenarios[0]
			effectiveAt, err := parseDate(first.EffectiveAt)
			if err != nil {
				return nil, fmt.Errorf("aggregates: legacy scenario effective_at: %w", err)
			}

			packageID := uuid.New()
			pkg, err := NewCompensationPackage(tenant, packageID, workerRef, nil, nil, effectiveAt, nil, catalogRecordedAt, first.CurrentCurrency)
			if err != nil {
				return nil, fmt.Errorf("aggregates: legacy scenario package: %w", err)
			}
			if _, err := comp.PutCompensationPackage(ctx, ex, pkg); err != nil {
				return nil, fmt.Errorf("aggregates: put legacy scenario package: %w", err)
			}
			out.PackageID = packageID

			amount, err := values.NewMoney(first.CurrentAmount, first.CurrentCurrency, 2, values.RoundingHalfEven)
			if err != nil {
				return nil, fmt.Errorf("aggregates: legacy scenario current_amount: %w", err)
			}
			componentID := uuid.New()
			component, err := NewCompensationComponent(tenant, componentID, packageID, effectiveAt, nil, catalogRecordedAt,
				"BASE_PAY", amount, "ANNUAL")
			if err != nil {
				return nil, fmt.Errorf("aggregates: legacy scenario component: %w", err)
			}
			if _, err := comp.PutCompensationComponent(ctx, ex, component); err != nil {
				return nil, fmt.Errorf("aggregates: put legacy scenario component: %w", err)
			}
			out.ComponentID = componentID
		}
	}

	return out, nil
}

func (out *LoadedFixtures) ensureLegalEntity(ctx context.Context, org OrganizationStore, ex Executor, name string,
	effectiveFrom, recordedAt time.Time) (uuid.UUID, error) {
	if id, ok := out.LegalEntityID[name]; ok {
		return id, nil
	}
	id := uuid.New()
	entity, err := NewLegalEntity(out.Tenant, id, effectiveFrom, nil, recordedAt, name, "ACTIVE")
	if err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: legal_entity %s: %w", name, err)
	}
	if _, err := org.PutLegalEntity(ctx, ex, entity); err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: put legal_entity %s: %w", name, err)
	}
	out.LegalEntityID[name] = id
	return id, nil
}

func (out *LoadedFixtures) ensureOrganizationUnit(ctx context.Context, org OrganizationStore, ex Executor, code string,
	legalEntityRef uuid.UUID, effectiveFrom, recordedAt time.Time) (uuid.UUID, error) {
	if id, ok := out.OrganizationID[code]; ok {
		return id, nil
	}
	id := uuid.New()
	unit, err := NewOrganizationUnit(out.Tenant, id, effectiveFrom, nil, recordedAt,
		"DEPARTMENT", code, code, &legalEntityRef, nil, "ACTIVE")
	if err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: organization_unit %s: %w", code, err)
	}
	if _, err := org.PutOrganizationUnit(ctx, ex, unit); err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: put organization_unit %s: %w", code, err)
	}
	out.OrganizationID[code] = id
	return id, nil
}

func (out *LoadedFixtures) ensureJob(ctx context.Context, org OrganizationStore, ex Executor, code, grade string,
	effectiveFrom, recordedAt time.Time) (uuid.UUID, error) {
	if id, ok := out.JobID[code]; ok {
		return id, nil
	}
	id := uuid.New()
	job, err := NewJob(out.Tenant, id, effectiveFrom, nil, recordedAt, code, code, "", grade, "EXEMPT")
	if err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: job %s: %w", code, err)
	}
	if _, err := org.PutJob(ctx, ex, job); err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: put job %s: %w", code, err)
	}
	out.JobID[code] = id
	return id, nil
}

func (out *LoadedFixtures) ensureJobPosition(ctx context.Context, org OrganizationStore, ex Executor, code string,
	jobRef, organizationRef, legalEntityRef uuid.UUID, location, capacityFTE string,
	effectiveFrom, recordedAt time.Time) (uuid.UUID, error) {
	if id, ok := out.PositionID[code]; ok {
		return id, nil
	}
	id := uuid.New()
	position, err := NewJobPosition(out.Tenant, id, jobRef, organizationRef, &legalEntityRef,
		effectiveFrom, nil, recordedAt, code, location, capacityFTE, "FILLED")
	if err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: job_position %s: %w", code, err)
	}
	if _, err := org.PutJobPosition(ctx, ex, position); err != nil {
		return uuid.Nil, fmt.Errorf("aggregates: put job_position %s: %w", code, err)
	}
	out.PositionID[code] = id
	return id, nil
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", s)
}

func parseInstant(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, s)
}

func mapWorkerType(fixtureType string) string {
	switch fixtureType {
	case "employee":
		return "EMPLOYEE"
	case "contractor":
		return "CONTRACTOR"
	case "intern":
		return "INTERN"
	default:
		return "EMPLOYEE"
	}
}

func mapLifecycleStatus(fixtureStatus string) string {
	switch fixtureStatus {
	case "active":
		return "ACTIVE"
	case "terminated":
		return "TERMINATED"
	case "on_leave":
		return "ON_LEAVE"
	default:
		return "PENDING"
	}
}

func mapEmploymentStatus(fixtureStatus string) string {
	switch fixtureStatus {
	case "active":
		return "ACTIVE"
	case "ended":
		return "ENDED"
	case "suspended":
		return "SUSPENDED"
	case "leave":
		return "LEAVE"
	default:
		return "PENDING"
	}
}
