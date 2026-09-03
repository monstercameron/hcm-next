// Command plancheck runs the GOV-001/003/005/006/007/008/009/012/014
// governance checkers against the real repository tree from the command
// line, independent of `go test`. Each subcommand exits non-zero and
// prints every unresolved violation it finds; a checker with a known
// allow-list (currently only the evidence-freshness checker) treats an
// allow-listed violation as non-fatal.
//
// This does not change tools/planning/cmd/todoregistry's behavior; it is
// a separate binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/hcm-next/tools/planning/atomicity"
	"github.com/monstercameron/hcm-next/tools/planning/deferredimports"
	"github.com/monstercameron/hcm-next/tools/planning/depthvocab"
	"github.com/monstercameron/hcm-next/tools/planning/evidence"
	"github.com/monstercameron/hcm-next/tools/planning/manifest"
	"github.com/monstercameron/hcm-next/tools/planning/plancontradiction"
	"github.com/monstercameron/hcm-next/tools/planning/scopeexchange"
	"github.com/monstercameron/hcm-next/tools/planning/terminology"
	"github.com/monstercameron/hcm-next/tools/planning/todoregistry"
	"github.com/monstercameron/hcm-next/tools/planning/traceability"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: plancheck <manifest|scopeexchange|traceability|depthvocab|atomicity|evidence|deferredimports|terminology|plancontradiction> [root]")
		os.Exit(2)
	}

	cmd := os.Args[1]
	root := "."
	if len(os.Args) >= 3 {
		root = os.Args[2]
	}

	var err error
	switch cmd {
	case "manifest":
		err = runManifest()
	case "scopeexchange":
		err = runScopeExchange()
	case "traceability":
		err = runTraceability(root)
	case "depthvocab":
		err = runDepthVocab(root)
	case "atomicity":
		err = runAtomicity(root)
	case "evidence":
		err = runEvidence(root)
	case "deferredimports":
		err = runDeferredImports(root)
	case "terminology":
		err = runTerminology(root)
	case "plancontradiction":
		err = runPlanContradiction(root)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", cmd)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runManifest and runScopeExchange have no repository data source to check
// yet: no delivery-manifest or scope-exchange record file exists in the
// tree (GOV-001/GOV-006 define the schema and validation rules; a future
// generator or manual manifest file is what these subcommands would then
// validate). They report that plainly rather than fabricating a check
// against nothing. The libraries themselves are fully covered by
// manifest.TestDeliveryManifestRejectsMissingRequiredFields and
// scopeexchange.TestScopeExchangeRejectsUnfundedAddition.
func runManifest() error {
	required := manifest.Validate(manifest.Manifest{})
	fmt.Printf("manifest: no delivery-manifest data source is wired into the repository yet (%d required fields defined); see tools/planning/manifest for the schema and manifest.Validate/CanonicalDigest for the library API\n", len(required))
	return nil
}

func runScopeExchange() error {
	required := scopeexchange.Validate(scopeexchange.Exchange{Requirement: manifest.Manifest{Gate: "GATE_A"}})
	fmt.Printf("scopeexchange: no scope-exchange record source is wired into the repository yet (%d required elements defined for a Gate A/B exchange); see tools/planning/scopeexchange for the schema and scopeexchange.Validate for the library API\n", len(required))
	return nil
}

func readTodos(root string) ([]todoregistry.Todo, error) {
	content, err := os.ReadFile(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return nil, fmt.Errorf("read planning/todos.md: %w", err)
	}
	todos, parseErrs := todoregistry.ParseTodos(string(content))
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("parse planning/todos.md: %v", parseErrs)
	}
	return todos, nil
}

func walkMarkdown(root string, visit func(path, content string) error) error {
	dir := filepath.Join(root, "planning")
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return visit(path, string(content))
	})
}

func runTraceability(root string) error {
	todos, err := readTodos(root)
	if err != nil {
		return err
	}
	tests, err := traceability.ScanTestNames(root)
	if err != nil {
		return fmt.Errorf("scan test names: %w", err)
	}
	orphans := traceability.CheckTraceability(todos, tests)
	if len(orphans) == 0 {
		fmt.Println("traceability: OK")
		return nil
	}
	for _, o := range orphans {
		fmt.Println(o)
	}
	return fmt.Errorf("traceability: %d orphan(s)", len(orphans))
}

func runDepthVocab(root string) error {
	var total int
	err := walkMarkdown(root, func(path, content string) error {
		for _, v := range depthvocab.CheckDepthDeclarations(content) {
			total++
			fmt.Printf("%s: %s\n", path, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("depthvocab: OK")
		return nil
	}
	return fmt.Errorf("depthvocab: %d violation(s)", total)
}

func runAtomicity(root string) error {
	todos, err := readTodos(root)
	if err != nil {
		return err
	}
	var total int
	for _, td := range todos {
		for _, v := range atomicity.CheckAtomicity(td.ID, td.Title, td.Green) {
			total++
			fmt.Println(v)
		}
	}
	if total == 0 {
		fmt.Println("atomicity: OK")
		return nil
	}
	return fmt.Errorf("atomicity: %d violation(s) (advisory - review before splitting todos)", total)
}

func runEvidence(root string) error {
	content, err := os.ReadFile(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return fmt.Errorf("read planning/todos.md: %w", err)
	}
	knownGaps, err := evidence.LoadKnownGaps(filepath.Join(root, "definitions", "planning", "known-defects.yaml"))
	if err != nil {
		return fmt.Errorf("load known-defects.yaml: %w", err)
	}
	allowed := make(map[string]bool, len(knownGaps))
	for _, g := range knownGaps {
		allowed[g.ID+"|"+g.Issue] = true
	}

	var unresolved int
	for _, occ := range evidence.ScanEvidenceFields(string(content)) {
		for _, v := range evidence.CheckFreshness(occ.ID, occ.Label, occ.Body) {
			if allowed[v.ID+"|"+v.Issue] {
				continue
			}
			unresolved++
			fmt.Println(v)
		}
	}
	if unresolved == 0 {
		fmt.Println("evidence: OK")
		return nil
	}
	return fmt.Errorf("evidence: %d unallowed violation(s)", unresolved)
}

func runDeferredImports(root string) error {
	violations, err := deferredimports.ScanDir(filepath.Join(root, "internal"))
	if err != nil {
		return err
	}
	if len(violations) == 0 {
		fmt.Println("deferredimports: OK")
		return nil
	}
	for _, v := range violations {
		fmt.Println(v)
	}
	return fmt.Errorf("deferredimports: %d violation(s)", len(violations))
}

func runTerminology(root string) error {
	var total int
	err := walkMarkdown(root, func(path, content string) error {
		for _, v := range terminology.CheckCanonicalTerms(content) {
			total++
			fmt.Printf("%s: %s\n", path, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("terminology: OK")
		return nil
	}
	return fmt.Errorf("terminology: %d violation(s)", total)
}

func runPlanContradiction(root string) error {
	var total int
	err := walkMarkdown(root, func(path, content string) error {
		for _, v := range plancontradiction.CheckPlanningContradictions(content) {
			total++
			fmt.Printf("%s: %s\n", path, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("plancontradiction: OK")
		return nil
	}
	return fmt.Errorf("plancontradiction: %d violation(s)", total)
}
