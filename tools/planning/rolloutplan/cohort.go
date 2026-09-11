package rolloutplan

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
)

// Cohort finding codes.
const (
	InvalidPlan       = "INVALID_PLAN"
	MissingPopulation = "MISSING_POPULATION"
	UnknownStage      = "UNKNOWN_STAGE"
	EmptyTarget       = "EMPTY_TARGET"
)

// CohortError is one exact cohort failure carrying its code.
type CohortError struct {
	Code   string
	Detail string
}

func (e *CohortError) Error() string { return e.Code + ": " + e.Detail }

// HasCohortCode reports whether err carries the cohort code.
func HasCohortCode(err error, code string) bool {
	var cohortErr *CohortError
	if !errors.As(err, &cohortErr) {
		return false
	}
	return cohortErr.Code == code
}

// MemberPlacement binds one population subject to its tenant, org and
// residency for rollout targeting.
type MemberPlacement struct {
	SubjectID string `json:"subject_id"`
	Tenant    string `json:"tenant"`
	Org       string `json:"org"`
	Residency string `json:"residency"`
}

// StageTarget restricts one plan stage to explicit tenant, org and
// residency allowlists. An empty allowlist matches nothing and is
// refused: targets are always stated, never ambient.
type StageTarget struct {
	Stage       string   `json:"stage"`
	Tenants     []string `json:"tenants"`
	Orgs        []string `json:"orgs"`
	Residencies []string `json:"residencies"`
}

// CohortRequest resolves one validated plan into per-stage cohorts over
// one frozen population snapshot.
type CohortRequest struct {
	Plan       Plan                `json:"plan"`
	Targets    []StageTarget       `json:"targets"`
	Population population.Snapshot `json:"population"`
	Placements []MemberPlacement   `json:"placements"`
}

// Cohort is one immutable per-stage member snapshot.
type Cohort struct {
	Stage            string   `json:"stage"`
	Members          []string `json:"members"`
	ExcludedCount    int      `json:"excluded_count"`
	Digest           string   `json:"digest"`
	PopulationDigest string   `json:"population_digest"`
	PlanDigest       string   `json:"plan_digest"`
}

// CohortSet freezes every stage cohort plus the set identity.
type CohortSet struct {
	Owner            string   `json:"owner"`
	PlanDigest       string   `json:"plan_digest"`
	PopulationDigest string   `json:"population_digest"`
	Cohorts          []Cohort `json:"cohorts"`
	Digest           string   `json:"digest"`
}

func cohortDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func inAllowlist(allowlist []string, value string) bool {
	for _, allowed := range allowlist {
		if allowed == value {
			return true
		}
	}
	return false
}

// FreezeCohorts resolves one plan into immutable per-stage cohorts.
// Subjects missing placement are excluded and counted, never smuggled in.
func FreezeCohorts(req CohortRequest) (CohortSet, error) {
	if findings := Validate(req.Plan); len(findings) != 0 {
		return CohortSet{}, &CohortError{Code: InvalidPlan, Detail: findings[0].String()}
	}
	if strings.TrimSpace(req.Population.Digest) == "" {
		return CohortSet{}, &CohortError{Code: MissingPopulation, Detail: "population snapshot carries no digest"}
	}
	compiled, err := Compile(req.Plan)
	if err != nil {
		return CohortSet{}, &CohortError{Code: InvalidPlan, Detail: err.Error()}
	}
	stages := make(map[string]bool, len(req.Plan.Stages))
	for _, stage := range req.Plan.Stages {
		stages[stage.Name] = true
	}
	known := make(map[string]bool, len(req.Population.SubjectIDs))
	for _, subject := range req.Population.SubjectIDs {
		known[subject] = true
	}
	placed := make(map[string]MemberPlacement, len(req.Placements))
	for _, placement := range req.Placements {
		placed[placement.SubjectID] = placement
	}
	set := CohortSet{
		Owner:            req.Plan.Owner,
		PlanDigest:       compiled.Digest,
		PopulationDigest: req.Population.Digest,
	}
	for _, target := range req.Targets {
		if !stages[target.Stage] {
			return CohortSet{}, &CohortError{Code: UnknownStage, Detail: fmt.Sprintf("stage %q is not in the plan", target.Stage)}
		}
		if len(target.Tenants) == 0 || len(target.Orgs) == 0 || len(target.Residencies) == 0 {
			return CohortSet{}, &CohortError{Code: EmptyTarget, Detail: fmt.Sprintf("stage %q leaves an allowlist empty", target.Stage)}
		}
		cohort := Cohort{
			Stage:            target.Stage,
			PopulationDigest: req.Population.Digest,
			PlanDigest:       compiled.Digest,
		}
		for _, subject := range req.Population.SubjectIDs {
			placement, ok := placed[subject]
			if !ok {
				cohort.ExcludedCount++
				continue
			}
			if !inAllowlist(target.Tenants, placement.Tenant) ||
				!inAllowlist(target.Orgs, placement.Org) ||
				!inAllowlist(target.Residencies, placement.Residency) {
				continue
			}
			cohort.Members = append(cohort.Members, subject)
		}
		sort.Strings(cohort.Members)
		cohort.Digest = cohortDigest(compiled.Digest, target.Stage, req.Population.Digest, strings.Join(cohort.Members, ","))
		set.Cohorts = append(set.Cohorts, cohort)
	}
	parts := make([]string, 0, len(set.Cohorts))
	for _, cohort := range set.Cohorts {
		parts = append(parts, cohort.Digest)
	}
	set.Digest = cohortDigest(parts...)
	return set, nil
}

// RenderOwner renders one stage cohort for its owner: members and counts.
func RenderOwner(set CohortSet, stage string) (string, error) {
	for _, cohort := range set.Cohorts {
		if cohort.Stage == stage {
			return renderCohort(cohort, true), nil
		}
	}
	return "", &CohortError{Code: UnknownStage, Detail: fmt.Sprintf("stage %q has no cohort", stage)}
}

// RenderObserver renders one stage cohort for anyone else: digests only,
// so member identities and counts never leak across the boundary.
func RenderObserver(set CohortSet, stage string) (string, error) {
	for _, cohort := range set.Cohorts {
		if cohort.Stage == stage {
			return renderCohort(cohort, false), nil
		}
	}
	return "", &CohortError{Code: UnknownStage, Detail: fmt.Sprintf("stage %q has no cohort", stage)}
}

func renderCohort(cohort Cohort, owner bool) string {
	var sb strings.Builder
	sb.WriteString("stage: " + cohort.Stage + "\n")
	sb.WriteString("digest: " + cohort.Digest + "\n")
	if owner {
		sb.WriteString(fmt.Sprintf("members: %d\n", len(cohort.Members)))
		for _, member := range cohort.Members {
			sb.WriteString("member: " + member + "\n")
		}
		sb.WriteString(fmt.Sprintf("excluded: %d\n", cohort.ExcludedCount))
	}
	return sb.String()
}
