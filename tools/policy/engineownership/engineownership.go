// Package engineownership assigns every material HCM semantic cluster an
// explicit production engine owner (GOV-029): the owner package plus its
// authoritative entities, intent sets, capability surface, shared-engine
// dependencies, persistence and evidence, external-observation boundary,
// correction policy, phase depth and atomic test closure. Deferred
// clusters stay explicitly CONFORMANCE_ONLY and publish no production
// capability. Ownership is declared here and verified against the tree:
// it is never inferred from import direction or matching names.
package engineownership

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OwnershipStatus names whether a cluster ships a production engine or is
// deferred to conformance-only depth.
type OwnershipStatus string

const (
	// StatusOwned marks a cluster with a production engine owner.
	StatusOwned OwnershipStatus = "OWNED"
	// StatusConformanceOnly marks a deferred cluster: no production
	// capability may be published for it.
	StatusConformanceOnly OwnershipStatus = "CONFORMANCE_ONLY"
)

// ValidIntentSets is the closed BusinessIntent set vocabulary.
var ValidIntentSets = []string{
	"BI.ALL", "BI.DATAOPS", "BI.OPERATIONS", "BI.PEOPLE",
	"BI.REGULATORY", "BI.REWARDS", "BI.TENANT", "BI.WORK", "BI.WORKFORCE",
}

// Cluster is one material semantic cluster's ownership contract.
type Cluster struct {
	Name          string
	Status        OwnershipStatus
	OwnerPackage  string
	Entities      []string
	IntentSets    []string
	Capabilities  []string
	SharedEngines []string
	Persistence   string
	Boundary      string
	Correction    string
	PhaseDepth    string
	ClosureTest   string
}

// Finding is one exact registry refusal.
type Finding struct {
	Cluster string
	Code    string
	Detail  string
}

// Finding codes.
const (
	DuplicateCluster         = "DUPLICATE_CLUSTER"
	MissingOwnerDir          = "MISSING_OWNER_DIR"
	DeferredWithCapabilities = "DEFERRED_WITH_CAPABILITIES"
	DeferredWithOwner        = "DEFERRED_WITH_OWNER"
	MissingContract          = "MISSING_CONTRACT"
	InvalidIntentSet         = "INVALID_INTENT_SET"
	MissingClosureTest       = "MISSING_CLOSURE_TEST"
	UnknownCluster           = "UNKNOWN_CLUSTER"
)

func owned(name, owner string, entities, sets, capabilities, shared []string, persistence, boundary, correction, phase, closure string) Cluster {
	return Cluster{
		Name: name, Status: StatusOwned, OwnerPackage: owner,
		Entities: entities, IntentSets: sets, Capabilities: capabilities,
		SharedEngines: shared, Persistence: persistence, Boundary: boundary,
		Correction: correction, PhaseDepth: phase, ClosureTest: closure,
	}
}

func deferred(name, phase, closure string) Cluster {
	return Cluster{
		Name: name, Status: StatusConformanceOnly,
		Entities:    []string{"deferred: no authoritative entities at conformance-only depth"},
		IntentSets:  []string{"BI.ALL"},
		PhaseDepth:  phase,
		ClosureTest: closure,
		Persistence: "none: no production persistence at conformance-only depth",
		Boundary:    "none: no external observation at conformance-only depth",
		Correction:  "none: no correction policy at conformance-only depth",
	}
}

// Registry is the fixed ownership table: every GOV-029 material cluster.
func Registry() []Cluster {
	return []Cluster{
		owned("workforce-access", "internal/domains/access",
			[]string{"AccessDecision", "AccessGrant"}, []string{"BI.WORKFORCE"},
			[]string{"workforce access decisions"}, []string{"internal/kernel/values"},
			"decision records with digests", "HRIS source of truth for employment",
			"grant supersession, never silent widening", "P0",
			"TestTodo_ACCESS_002_Property"),
		owned("hr-cases", "internal/domains/hrcase",
			[]string{"HRCase", "CaseTask"}, []string{"BI.WORKFORCE"},
			[]string{"case lifecycle and task routing"}, []string{"internal/workflow/steps/subworkflow"},
			"case records with lineage", "case reporter and HR partner observations",
			"case correction appends, never edits", "P1A",
			"TestTodo_CASE_001_Property"),
		owned("worker-lifecycle", "internal/domains/workerlifecycle",
			[]string{"WorkerLifecycle", "LifecycleEvent"}, []string{"BI.WORKFORCE"},
			[]string{"hire-to-retire lifecycle transitions"}, []string{"internal/engines/schedule"},
			"lifecycle event log", "HRIS employment authority",
			"retroactive correction with bitemporal evidence", "P0",
			"TestTodo_WORKER_LIFE_001_Property"),
		owned("fx", "internal/domains/fx",
			[]string{"FXRate", "FXConversion"}, []string{"BI.REWARDS"},
			[]string{"exact currency conversion"}, []string{"internal/kernel/values"},
			"rate tables with effective intervals", "treasury rate source",
			"rate restatement supersedes, never rewrites", "P1B",
			"TestTodo_FX_003_Golden"),
		owned("location-jurisdiction", "internal/domains/location",
			[]string{"WorkLocation", "Jurisdiction"}, []string{"BI.REGULATORY", "BI.WORKFORCE"},
			[]string{"location and jurisdiction resolution"}, []string{"internal/governance/legal"},
			"location registry with provenance", "legal entity and payroll authority",
			"jurisdiction correction with effective dating", "P0",
			"TestTodo_LOCATION_003_Property"),
		owned("service-seniority", "internal/domains/service",
			[]string{"ServiceCredit", "SeniorityFact"}, []string{"BI.WORKFORCE", "BI.REWARDS"},
			[]string{"service-credit and seniority vocabulary"}, []string{"internal/kernel/values"},
			"immutable service revisions", "HRIS employment continuity",
			"credit restatement appends new revisions", "P1B",
			"TestTodo_SERVICE_001_Property"),
		owned("collective-bargaining", "internal/domains/cba",
			[]string{"CollectiveAgreement", "BargainingUnit"}, []string{"BI.REGULATORY", "BI.WORKFORCE"},
			[]string{"agreement coverage and unit membership"}, []string{"internal/governance/legal"},
			"agreement records with effective intervals", "labor authority filings",
			"agreement amendment supersedes", "P1B",
			"TestTodo_CBA_002_Property"),
		owned("payroll-inputs", "internal/domains/payinput",
			[]string{"PayInput", "InputBatch"}, []string{"BI.REWARDS"},
			[]string{"payroll input collection and validation"}, []string{"internal/kernel/values"},
			"input batches with digests", "timekeeping and HRIS sources",
			"input correction batches, never edits", "P0",
			"TestTodo_PAYINPUT_002_Property"),
		owned("tax-profile", "internal/domains/taxprofile",
			[]string{"TaxProfile", "WithholdingElection"}, []string{"BI.REWARDS", "BI.REGULATORY"},
			[]string{"withholding profile resolution"}, []string{"internal/governance/legal"},
			"profile revisions with authority", "payroll tax authority",
			"election changes version the profile", "P0",
			"TestTodo_TAXPROFILE_002_Property"),
		owned("pay-methods", "internal/domains/paymethod",
			[]string{"PayMethod", "Disbursement"}, []string{"BI.REWARDS"},
			[]string{"disbursement method selection"}, []string{"internal/kernel/values"},
			"method records with verification evidence", "banking provider observations",
			"method change re-verifies", "P0",
			"TestTodo_PAYMETHOD_002_Property"),
		owned("job-architecture", "internal/domains/jobarch",
			[]string{"JobFamily", "JobLevel"}, []string{"BI.WORKFORCE", "BI.REWARDS"},
			[]string{"job family and level taxonomy"}, nil,
			"versioned taxonomy releases", "compensation committee approval",
			"taxonomy releases supersede", "P1A",
			"TestTodo_JOBARCH_001_Property"),
		owned("identity-work-authorization", "internal/domains/proofing",
			[]string{"ProofingSession", "WorkAuthorization"}, []string{"BI.WORKFORCE", "BI.REGULATORY"},
			[]string{"identity proofing and work-authorization evidence"}, []string{"internal/domains/privacy"},
			"references and digests only", "document custody boundary",
			"re-proofing supersedes sessions", "P0",
			"TestTodo_PROOF_002_Property"),
		owned("employee-relations", "internal/domains/employeerelations",
			[]string{"ERCase", "ERAction"}, []string{"BI.WORKFORCE"},
			[]string{"relations case management"}, []string{"internal/domains/hrcase"},
			"case records with restricted visibility", "HR partner observations",
			"action appends with approval lineage", "P1A",
			"TestTodo_ER_001_Property"),
		owned("mobility", "internal/domains/mobility",
			[]string{"MobilityCase", "TransferPlan"}, []string{"BI.WORKFORCE"},
			[]string{"internal mobility and transfer planning"}, []string{"internal/domains/position"},
			"mobility plans with approvals", "manager and HR observations",
			"plan revision supersedes", "P1A",
			"TestTodo_MOBILITY_002_Property"),
		owned("safety", "internal/domains/safety",
			[]string{"SafetyIncident", "SafetyMeasure"}, []string{"BI.WORKFORCE", "BI.REGULATORY"},
			[]string{"incident recording and measure tracking"}, []string{"internal/governance/legal"},
			"incident records with OSHA lineage", "safety authority filings",
			"incident amendments append", "P1A",
			"TestTodo_SAFETY_001_Property"),
		owned("skills-career-succession", "internal/domains/skill",
			[]string{"Skill", "Proficiency"}, []string{"BI.WORKFORCE"},
			[]string{"skill taxonomy and proficiency"}, []string{"internal/domains/career", "internal/domains/succession"},
			"skill assertions with evidence", "manager and peer attestations",
			"re-assessment supersedes", "P1A",
			"TestTodo_SKILL_001_Conformance"),
		owned("advanced-rewards", "internal/domains/rewards",
			[]string{"RewardPlan", "RewardGrant"}, []string{"BI.REWARDS"},
			[]string{"long-term and recognition rewards"}, []string{"internal/domains/compensation", "internal/kernel/values"},
			"grant records with vesting schedules", "compensation committee authority",
			"grant amendments re-approve", "P1B",
			"TestTodo_COMP_002"),
		owned("assets", "internal/domains/asset",
			[]string{"Asset", "Assignment"}, []string{"BI.WORKFORCE", "BI.OPERATIONS"},
			[]string{"asset assignment and return"}, []string{"internal/domains/tenant"},
			"assignment chain with custody", "IT asset source",
			"reassignment appends custody hops", "P1B",
			"TestTodo_ASSET_002_Property"),
		owned("contact-verification", "internal/domains/contact",
			[]string{"ContactPoint", "VerificationChallenge"}, []string{"BI.WORKFORCE"},
			[]string{"contact verification challenges"}, []string{"internal/domains/privacy"},
			"challenge records with digests", "messaging provider observations",
			"reissue revokes the old challenge", "P0",
			"TestTodo_CONTACT_002_ReissueRevokesOldChallenge"),
		deferred("ats", "Phase 2", "TestSemanticEngineOwnershipRejectsConformanceOnlyOrGenericOwner"),
		deferred("reporting", "Phase 2", "TestSemanticEngineOwnershipRejectsConformanceOnlyOrGenericOwner"),
		deferred("localization", "Phase 2", "TestSemanticEngineOwnershipRejectsConformanceOnlyOrGenericOwner"),
	}
}

// Resolve returns the ownership contract for one cluster name.
func Resolve(name string) (Cluster, error) {
	for _, cluster := range Registry() {
		if cluster.Name == name {
			return cluster, nil
		}
	}
	return Cluster{}, &RegistryError{Code: UnknownCluster, Detail: "cluster " + name + " is not registered"}
}

// RegistryError is one registry lookup failure.
type RegistryError struct {
	Code   string
	Detail string
}

func (e *RegistryError) Error() string { return e.Code + ": " + e.Detail }

// Verify checks one registry table against the tree rooted at root:
// no duplicates, owned packages exist on disk, deferred clusters publish
// nothing and own nothing, every contract dimension is stated, intent
// sets come from the closed vocabulary, and every closure test is
// declared by its owner's tests.
func Verify(root string, clusters []Cluster) []Finding {
	var findings []Finding
	seen := make(map[string]bool)
	for _, cluster := range clusters {
		if cluster.Name == "" || seen[cluster.Name] {
			findings = append(findings, Finding{Cluster: cluster.Name, Code: DuplicateCluster, Detail: "cluster name is empty or repeated"})
			continue
		}
		seen[cluster.Name] = true
		switch cluster.Status {
		case StatusOwned:
			if strings.TrimSpace(cluster.OwnerPackage) == "" {
				findings = append(findings, Finding{Cluster: cluster.Name, Code: MissingContract, Detail: "owned cluster names no owner package"})
				continue
			}
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(cluster.OwnerPackage)))
			if err != nil || !info.IsDir() {
				findings = append(findings, Finding{Cluster: cluster.Name, Code: MissingOwnerDir, Detail: "owner package " + cluster.OwnerPackage + " is not in the tree"})
			}
			if !closureDeclared(root, cluster.OwnerPackage, cluster.ClosureTest) {
				findings = append(findings, Finding{Cluster: cluster.Name, Code: MissingClosureTest, Detail: "closure test " + cluster.ClosureTest + " is not declared by " + cluster.OwnerPackage})
			}
		case StatusConformanceOnly:
			if strings.TrimSpace(cluster.OwnerPackage) != "" {
				findings = append(findings, Finding{Cluster: cluster.Name, Code: DeferredWithOwner, Detail: "deferred cluster names an owner package"})
			}
			if len(cluster.Capabilities) != 0 {
				findings = append(findings, Finding{Cluster: cluster.Name, Code: DeferredWithCapabilities, Detail: "deferred cluster publishes capabilities"})
			}
		default:
			findings = append(findings, Finding{Cluster: cluster.Name, Code: MissingContract, Detail: "unknown ownership status"})
		}
		if len(cluster.Entities) == 0 || strings.TrimSpace(cluster.Persistence) == "" ||
			strings.TrimSpace(cluster.Boundary) == "" || strings.TrimSpace(cluster.Correction) == "" ||
			strings.TrimSpace(cluster.PhaseDepth) == "" || strings.TrimSpace(cluster.ClosureTest) == "" {
			findings = append(findings, Finding{Cluster: cluster.Name, Code: MissingContract, Detail: "entities, persistence, boundary, correction, phase or closure are unstated"})
		}
		if len(cluster.IntentSets) == 0 {
			findings = append(findings, Finding{Cluster: cluster.Name, Code: MissingContract, Detail: "no intent set is stated"})
		}
		for _, set := range cluster.IntentSets {
			valid := false
			for _, candidate := range ValidIntentSets {
				if set == candidate {
					valid = true
				}
			}
			if !valid {
				findings = append(findings, Finding{Cluster: cluster.Name, Code: InvalidIntentSet, Detail: "intent set " + set + " is outside the vocabulary"})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Cluster != findings[j].Cluster {
			return findings[i].Cluster < findings[j].Cluster
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}

// closureDeclared reports whether the owner's tests declare the closure
// test by name. Declaration, not import direction, is the evidence.
func closureDeclared(root, ownerPackage, test string) bool {
	if strings.TrimSpace(test) == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(ownerPackage)))
	if err != nil {
		return false
	}
	want := "func " + test + "("
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ownerPackage), entry.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(raw), want) {
			return true
		}
	}
	return false
}

// Digest seals the canonical registry encoding.
func Digest(clusters []Cluster) string {
	parts := []string{"engine-ownership/v1"}
	names := make([]string, 0, len(clusters))
	byName := make(map[string]Cluster, len(clusters))
	for _, cluster := range clusters {
		names = append(names, cluster.Name)
		byName[cluster.Name] = cluster
	}
	sort.Strings(names)
	for _, name := range names {
		cluster := byName[name]
		parts = append(parts, name, string(cluster.Status), cluster.OwnerPackage,
			strings.Join(cluster.Entities, ","), strings.Join(cluster.IntentSets, ","),
			strings.Join(cluster.Capabilities, ","), strings.Join(cluster.SharedEngines, ","),
			cluster.Persistence, cluster.Boundary, cluster.Correction, cluster.PhaseDepth, cluster.ClosureTest)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
