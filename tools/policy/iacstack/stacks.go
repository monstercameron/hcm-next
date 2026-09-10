// Package iacstack owns the IAC-002 reproducible environment contract. One
// pinned module graph produces the dev, test, stage and production-cell
// stacks from reviewed variables with deterministic plan summaries and
// explicit cost and resource bounds. It is kernel-pure: no database, cloud
// SDK, network call or mutable global is involved.
package iacstack

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Standard stack names. Exactly these four environments exist; anything else
// is an undocumented manual step.
const (
	StackDev            = "dev"
	StackTest           = "test"
	StackStage          = "stage"
	StackProductionCell = "production-cell"
)

// Data classes. Production data never enters a non-production stack.
const (
	DataSynthetic  = "synthetic"
	DataMasked     = "masked"
	DataProduction = "production"
)

// Module is one pinned entry of the shared module graph. The version plus
// digest pair is the pin; nothing unpinned may produce an environment.
type Module struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// CostBounds carries the explicit cost and resource ceiling of one stack.
type CostBounds struct {
	MonthlyCentsMax int64 `json:"monthly_cents_max"`
	ResourcesMax    int   `json:"resources_max"`
}

// Stack is one isolated environment produced from the shared module graph.
// Only Variables, Cost, CredentialIDs, DataClass and markings differ between
// stacks; the module graph is identical everywhere.
type Stack struct {
	Name              string            `json:"name"`
	Production        bool              `json:"production"`
	NonProductionMark bool              `json:"non_production_mark,omitempty"`
	Modules           []Module          `json:"modules"`
	Variables         map[string]string `json:"variables"`
	CredentialIDs     []string          `json:"credential_ids"`
	DataClass         string            `json:"data_class"`
	Cost              CostBounds        `json:"cost"`
}

// ReviewedVariable is one variable the stack contract may consume. Sensitive
// variables never carry literal values; they reference issued credentials.
type ReviewedVariable struct {
	Name      string `json:"name"`
	Sensitive bool   `json:"sensitive"`
}

// Finding is one fail-closed stack admission finding.
type Finding struct {
	Stack  string `json:"stack,omitempty"`
	Field  string `json:"field,omitempty"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Report is the deterministic result of validating stacks.
type Report struct {
	Findings []Finding `json:"findings,omitempty"`
}

// OK reports whether every stack is admissible.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// PlanSummary is the deterministic, credential-free rendering of stacks.
type PlanSummary struct {
	Lines  []string `json:"lines"`
	Digest string   `json:"digest"`
}

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

var reviewedVariables = []ReviewedVariable{
	{Name: "region"},
	{Name: "residency"},
	{Name: "environment"},
	{Name: "data_class"},
	{Name: "instance_size"},
	{Name: "replica_count"},
	{Name: "retention_days"},
	{Name: "db_password", Sensitive: true},
}

// ReviewedVariableSet returns the reviewed variable vocabulary.
func ReviewedVariableSet() []ReviewedVariable {
	return append([]ReviewedVariable(nil), reviewedVariables...)
}

func standardModules() []Module {
	mods := []struct{ name, version, seed string }{
		{"network", "1.4.0", "a"}, {"compute", "2.1.0", "b"}, {"postgres", "3.0.1", "c"},
		{"object", "1.0.2", "d"}, {"queue", "1.2.0", "e"}, {"cache", "1.1.0", "f"},
		{"telemetry", "0.9.4", "0"}, {"secrets", "1.0.0", "1"}, {"edge", "0.5.2", "2"},
	}
	out := make([]Module, 0, len(mods))
	for _, mod := range mods {
		out = append(out, Module{Name: mod.name, Version: mod.version, Digest: "sha256:" + strings.Repeat(mod.seed, 64)})
	}
	return out
}

func standardStack(name string, production, mark bool, dataClass, credential, size string, replicas, retention int, monthlyCents int64, resources int) Stack {
	variables := map[string]string{
		"region": "region-a", "residency": "eu", "environment": name, "data_class": dataClass,
		"instance_size": size, "replica_count": strconv.Itoa(replicas), "retention_days": strconv.Itoa(retention),
		"db_password": "ref://" + credential + "/db_password",
	}
	return Stack{
		Name: name, Production: production, NonProductionMark: mark,
		Modules: standardModules(), Variables: variables,
		CredentialIDs: []string{credential}, DataClass: dataClass,
		Cost: CostBounds{MonthlyCentsMax: monthlyCents, ResourcesMax: resources},
	}
}

// StandardStacks returns the four canonical isolated environments. Every
// stack is independent: mutating one never affects another.
func StandardStacks() []Stack {
	return []Stack{
		standardStack(StackDev, false, true, DataSynthetic, "cred-dev-01", "s", 1, 7, 100000, 25),
		standardStack(StackTest, false, true, DataSynthetic, "cred-test-01", "m", 2, 14, 200000, 40),
		standardStack(StackStage, false, true, DataMasked, "cred-stage-01", "l", 2, 30, 500000, 60),
		standardStack(StackProductionCell, true, false, DataProduction, "cred-prod-01", "xl", 3, 90, 2000000, 120),
	}
}

// ValidateStacks checks the complete IAC-002 contract. It is deterministic
// and fail-closed: findings carry exact codes and sort stably.
func ValidateStacks(stacks []Stack, reviewed []ReviewedVariable) Report {
	report := Report{}
	required := map[string]bool{StackDev: false, StackTest: false, StackStage: false, StackProductionCell: false}
	byName := map[string]Stack{}
	for _, stack := range stacks {
		if _, ok := required[stack.Name]; !ok {
			report.add(Finding{Stack: stack.Name, Field: "name", Code: "UNKNOWN_STACK", Detail: "environment is not one of dev, test, stage, production-cell"})
			continue
		}
		if required[stack.Name] {
			report.add(Finding{Stack: stack.Name, Field: "name", Code: "DUPLICATE_STACK", Detail: "environment is declared more than once"})
			continue
		}
		required[stack.Name] = true
		byName[stack.Name] = stack
	}
	for name, present := range required {
		if !present {
			report.add(Finding{Field: "stacks", Code: "MISSING_STACK", Detail: "environment " + name + " is required"})
		}
	}
	reviewedByName := map[string]ReviewedVariable{}
	for _, variable := range reviewed {
		reviewedByName[variable.Name] = variable
	}
	reference := referenceGraph(stacks, byName)
	seenCredentials := map[string]string{}
	names := sortedStackNames(byName)
	for _, name := range names {
		stack := byName[name]
		validateModules(&report, stack)
		validateGraph(&report, stack, reference)
		validateVariables(&report, stack, reviewedByName)
		validateIdentity(&report, stack)
		validateBounds(&report, stack)
		for _, id := range stack.CredentialIDs {
			if strings.TrimSpace(id) == "" {
				report.add(Finding{Stack: name, Field: "credential_ids", Code: "MISSING_CREDENTIAL", Detail: "credential reference must not be blank"})
				continue
			}
			if owner, ok := seenCredentials[id]; ok {
				report.add(Finding{Stack: name, Field: "credential_ids", Code: "SHARED_CREDENTIAL", Detail: "credential " + id + " is already issued to " + owner})
				continue
			}
			seenCredentials[id] = name
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Stack != report.Findings[j].Stack {
			return report.Findings[i].Stack < report.Findings[j].Stack
		}
		if report.Findings[i].Code != report.Findings[j].Code {
			return report.Findings[i].Code < report.Findings[j].Code
		}
		return report.Findings[i].Field < report.Findings[j].Field
	})
	return report
}

func (r *Report) add(finding Finding) {
	r.Findings = append(r.Findings, finding)
}

func sortedStackNames(byName map[string]Stack) []string {
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func referenceGraph(stacks []Stack, byName map[string]Stack) map[string]Module {
	if stack, ok := byName[StackProductionCell]; ok {
		return graphIndex(stack.Modules)
	}
	if len(stacks) > 0 {
		return graphIndex(stacks[0].Modules)
	}
	return map[string]Module{}
}

func graphIndex(modules []Module) map[string]Module {
	index := make(map[string]Module, len(modules))
	for _, module := range modules {
		index[module.Name] = module
	}
	return index
}

func validateModules(report *Report, stack Stack) {
	if len(stack.Modules) == 0 {
		report.add(Finding{Stack: stack.Name, Field: "modules", Code: "MISSING_MODULES", Detail: "stack must be produced from the pinned module graph"})
		return
	}
	for _, module := range stack.Modules {
		if strings.TrimSpace(module.Name) == "" || strings.TrimSpace(module.Version) == "" || !digestPattern.MatchString(module.Digest) {
			report.add(Finding{Stack: stack.Name, Field: "modules", Code: "UNPINNED_MODULE", Detail: "module " + module.Name + " must carry a version and a sha256 digest"})
		}
	}
}

func validateGraph(report *Report, stack Stack, reference map[string]Module) {
	if len(stack.Modules) == 0 || len(reference) == 0 {
		return
	}
	index := graphIndex(stack.Modules)
	if len(index) != len(reference) {
		report.add(Finding{Stack: stack.Name, Field: "modules", Code: "MODULE_GRAPH_DIVERGENCE", Detail: "stack module graph differs from the pinned graph"})
		return
	}
	for name, module := range reference {
		got, ok := index[name]
		if !ok || got.Version != module.Version || got.Digest != module.Digest {
			report.add(Finding{Stack: stack.Name, Field: "modules", Code: "MODULE_GRAPH_DIVERGENCE", Detail: "stack module graph differs from the pinned graph"})
			return
		}
	}
}

func validateVariables(report *Report, stack Stack, reviewed map[string]ReviewedVariable) {
	keys := make([]string, 0, len(stack.Variables))
	for key := range stack.Variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		variable, ok := reviewed[key]
		if !ok {
			report.add(Finding{Stack: stack.Name, Field: key, Code: "UNREVIEWED_VARIABLE", Detail: "variable is not in the reviewed set"})
			continue
		}
		if variable.Sensitive && !strings.HasPrefix(stack.Variables[key], "ref://") {
			report.add(Finding{Stack: stack.Name, Field: key, Code: "LITERAL_SENSITIVE_VALUE", Detail: "sensitive variable must reference an issued credential"})
		}
	}
}

func validateIdentity(report *Report, stack Stack) {
	production := stack.Name == StackProductionCell
	if stack.Production != production {
		report.add(Finding{Stack: stack.Name, Field: "production", Code: "INVALID_PRODUCTION_FLAG", Detail: "only production-cell is the production environment"})
	}
	if production {
		if stack.NonProductionMark {
			report.add(Finding{Stack: stack.Name, Field: "non_production_mark", Code: "INVALID_PRODUCTION_FLAG", Detail: "production-cell must not carry the non-production marking"})
		}
		if stack.DataClass != DataProduction {
			report.add(Finding{Stack: stack.Name, Field: "data_class", Code: "INVALID_DATA_CLASS", Detail: "production-cell observes production data"})
		}
		return
	}
	if !stack.NonProductionMark {
		report.add(Finding{Stack: stack.Name, Field: "non_production_mark", Code: "MISSING_NONPROD_MARKING", Detail: "non-production environment must carry the visible marking"})
	}
	if stack.DataClass != DataSynthetic && stack.DataClass != DataMasked {
		if stack.DataClass == DataProduction {
			report.add(Finding{Stack: stack.Name, Field: "data_class", Code: "PRODUCTION_DATA_IN_NONPROD", Detail: "production data must not enter a non-production environment"})
		} else {
			report.add(Finding{Stack: stack.Name, Field: "data_class", Code: "INVALID_DATA_CLASS", Detail: "data class must be synthetic, masked or production"})
		}
	}
}

func validateBounds(report *Report, stack Stack) {
	if stack.Cost.MonthlyCentsMax <= 0 || stack.Cost.ResourcesMax <= 0 {
		report.add(Finding{Stack: stack.Name, Field: "cost", Code: "MISSING_BOUNDS", Detail: "stack must declare explicit cost and resource bounds"})
	}
}

// SummarizeStacks renders the deterministic credential-free plan summary.
// Stacks, modules, credentials and variables sort canonically, so declaration
// order never changes the digest. Sensitive values render as redacted.
func SummarizeStacks(stacks []Stack) PlanSummary {
	sensitive := map[string]bool{}
	for _, variable := range reviewedVariables {
		if variable.Sensitive {
			sensitive[variable.Name] = true
		}
	}
	ordered := append([]Stack(nil), stacks...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	var lines []string
	for _, stack := range ordered {
		lines = append(lines, "stack "+stack.Name+" production="+boolString(stack.Production))
		lines = append(lines, "stack "+stack.Name+" bound monthly_cents<="+strconv.FormatInt(stack.Cost.MonthlyCentsMax, 10)+" resources<="+strconv.Itoa(stack.Cost.ResourcesMax))
		credentials := append([]string(nil), stack.CredentialIDs...)
		sort.Strings(credentials)
		for _, id := range credentials {
			lines = append(lines, "stack "+stack.Name+" credential "+id)
		}
		lines = append(lines, "stack "+stack.Name+" dataclass "+stack.DataClass)
		modules := append([]Module(nil), stack.Modules...)
		sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
		for _, module := range modules {
			lines = append(lines, "stack "+stack.Name+" module "+module.Name+" "+module.Version+" "+module.Digest)
		}
		keys := make([]string, 0, len(stack.Variables))
		for key := range stack.Variables {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := stack.Variables[key]
			if sensitive[key] {
				value = "<redacted>"
			}
			lines = append(lines, "stack "+stack.Name+" var "+key+"="+value)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return PlanSummary{Lines: lines, Digest: "sha256:" + hex.EncodeToString(sum[:])}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
