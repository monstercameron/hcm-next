package execute

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/connectivity/mapping"
	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/exec"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func transformProgram(t *testing.T) ir.Program {
	t.Helper()
	p := ir.Program{
		IRVersion: ir.IRVersion, DefinitionName: "mapping-transform",
		Instructions: []ir.Instruction{{
			Op:          ir.OpProject,
			Sources:     []transformation.Path{{Schema: "people", Field: "Name", Type: transformation.TypeString}},
			Destination: transformation.Path{Schema: "worker", Field: "display_name", Type: transformation.TypeString},
		}},
		Limits: ir.Limits{MaxSteps: 1, MaxFanOut: 1},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("program.Validate: %v", err)
	}
	return p
}

func profileFor(t *testing.T, digest string) mapping.MappingProfileVersion {
	t.Helper()
	profile, err := mapping.Compile(mapping.MappingProfile{
		MappingID: "worker-import", Version: 1, SourceSystemRef: "people@v1", TargetEntity: "worker",
		TargetFields: []mapping.TargetField{{Name: "worker.display_name", Classification: mapping.ClassificationInternal}, {Name: "worker.external_id", Classification: mapping.ClassificationInternal}},
		Mappings: []mapping.FieldMapping{
			{SourceField: "Name", TargetField: "worker.display_name", TransformationIRDigest: digest, Required: true, Classification: mapping.ClassificationInternal},
			{SourceField: "ID", TargetField: "worker.external_id", TransformationIRDigest: mapping.IdentityTransformation, Required: true, Classification: mapping.ClassificationInternal},
		},
	})
	if err != nil {
		t.Fatalf("mapping.Compile: %v", err)
	}
	return profile
}

func sourceRecord() exec.Record {
	return exec.Record{
		"Name": {Type: transformation.TypeString, State: values.PresenceValue, Data: "Grace"},
		"ID":   {Type: transformation.TypeString, State: values.PresenceValue, Data: "W-7"},
	}
}

// TestTodo_CONN_RT_005 executes a mixed identity and compiled-IR profile and
// confirms that each mapped target has lineage.
func TestTodo_CONN_RT_005(t *testing.T) {
	program := transformProgram(t)
	digest, err := program.Digest()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(profileFor(t, digest), sourceRecord(), Programs{digest: program})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["worker.display_name"].Data != "Grace" || result.Output["worker.external_id"].Data != "W-7" {
		t.Fatalf("output = %+v", result.Output)
	}
	for _, target := range []string{"worker.display_name", "worker.external_id"} {
		if _, err := result.Lineage.SourcePath(target); err != nil {
			t.Fatalf("lineage for %s: %v", target, err)
		}
	}
	if result.PayloadDigest == "" || result.Lineage.Digest() == "" {
		t.Fatal("missing result digest")
	}
}

// TestTodo_CONN_RT_005_Golden pins the output digest for the mapping fixture.
func TestTodo_CONN_RT_005_Golden(t *testing.T) {
	program := transformProgram(t)
	digest, _ := program.Digest()
	result, err := Execute(profileFor(t, digest), sourceRecord(), Programs{digest: program})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:7235f5745ff51bfb9123666c5f1eb4f35ef3650db58f9cb65094bc797391570d"
	if result.PayloadDigest != want {
		t.Fatalf("payload digest = %s, want %s", result.PayloadDigest, want)
	}
}

// FuzzTodo_CONN_RT_005 proves output replay is stable for arbitrary source
// text while remaining bounded by the typed IR interpreter.
func FuzzTodo_CONN_RT_005(f *testing.F) {
	for _, value := range []string{"Grace", "", "x", "\x00"} {
		f.Add(value)
	}
	program := transformProgramForFuzz()
	digest, _ := program.Digest()
	f.Fuzz(func(t *testing.T, value string) {
		source := sourceRecord()
		field := source["Name"]
		field.Data = value
		source["Name"] = field
		profile := mustProfile(profileForFuzz(digest))
		first, err1 := Execute(profile, source, Programs{digest: program})
		second, err2 := Execute(profile, source, Programs{digest: program})
		if (err1 == nil) != (err2 == nil) || first.PayloadDigest != second.PayloadDigest {
			t.Fatalf("replay changed for %q: %v/%v %s/%s", value, err1, err2, first.PayloadDigest, second.PayloadDigest)
		}
	})
}

func transformProgramForFuzz() ir.Program {
	return ir.Program{IRVersion: ir.IRVersion, DefinitionName: "fuzz", Instructions: []ir.Instruction{{Op: ir.OpProject, Sources: []transformation.Path{{Schema: "people", Field: "Name", Type: transformation.TypeString}}, Destination: transformation.Path{Schema: "worker", Field: "display_name", Type: transformation.TypeString}}}, Limits: ir.Limits{MaxSteps: 1, MaxFanOut: 1}}
}
func profileForFuzz(digest string) mapping.MappingProfile {
	return mapping.MappingProfile{MappingID: "fuzz", Version: 1, SourceSystemRef: "people", TargetEntity: "worker", TargetFields: []mapping.TargetField{{Name: "worker.display_name", Classification: mapping.ClassificationInternal}}, Mappings: []mapping.FieldMapping{{SourceField: "Name", TargetField: "worker.display_name", TransformationIRDigest: digest, Required: true, Classification: mapping.ClassificationInternal}}}
}
func mustProfile(profile mapping.MappingProfile) mapping.MappingProfileVersion {
	compiled, err := mapping.Compile(profile)
	if err != nil {
		panic(err)
	}
	return compiled
}

// TestTodo_CONN_RT_005_Integration confirms concurrent callers share no
// mutable interpreter or profile state.
func TestTodo_CONN_RT_005_Integration(t *testing.T) {
	program := transformProgram(t)
	digest, _ := program.Digest()
	profile := profileFor(t, digest)
	want, err := Execute(profile, sourceRecord(), Programs{digest: program})
	if err != nil {
		t.Fatal(err)
	}
	const workers = 24
	got := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := Execute(profile, sourceRecord(), Programs{digest: program})
			errs[i] = err
			if err == nil {
				got[i] = result.PayloadDigest + ":" + result.Lineage.Digest()
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil || got[i] != want.PayloadDigest+":"+want.Lineage.Digest() {
			t.Fatalf("worker %d: %v %s", i, err, got[i])
		}
	}
}

// TestTodo_CONN_RT_005_Fault refuses a profile whose pinned digest and
// supplied compiled program differ, before producing any output.
func TestTodo_CONN_RT_005_Fault(t *testing.T) {
	program := transformProgram(t)
	digest, _ := program.Digest()
	profile := profileFor(t, digest)
	changed := transformProgram(t)
	changed.DefinitionName = "different-program"
	wrongDigest, _ := changed.Digest()
	if wrongDigest == digest {
		t.Fatal("fixture mutation did not change digest")
	}
	_, err := Execute(profile, sourceRecord(), Programs{digest: changed})
	if err == nil || !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("Execute error = %v, want digest mismatch", err)
	}
}

// TestTodo_CONN_RT_005_Recovery verifies a refused execution leaves no
// residue and the same profile can execute successfully afterward.
func TestTodo_CONN_RT_005_Recovery(t *testing.T) {
	program := transformProgram(t)
	digest, _ := program.Digest()
	profile := profileFor(t, digest)
	if _, err := Execute(profile, sourceRecord(), Programs{digest: transformProgramForFuzz()}); err == nil {
		t.Fatal("wrong compiled program was accepted")
	}
	result, err := Execute(profile, sourceRecord(), Programs{digest: program})
	if err != nil || result.Output["worker.display_name"].Data != "Grace" {
		t.Fatalf("recovery result=%+v err=%v", result, err)
	}
}

// TestTodo_CONN_RT_005_Mutation covers missing programs and required sources.
func TestTodo_CONN_RT_005_Mutation(t *testing.T) {
	program := transformProgram(t)
	digest, _ := program.Digest()
	profile := profileFor(t, digest)
	if _, err := Execute(profile, sourceRecord(), nil); err == nil || !errors.Is(err, ErrMissingProgram) {
		t.Fatalf("missing program error = %v", err)
	}
	missing := sourceRecord()
	delete(missing, "Name")
	if _, err := Execute(profile, missing, Programs{digest: program}); err == nil || !errors.Is(err, ErrMissingSource) {
		t.Fatalf("missing source error = %v", err)
	}
}

func TestExplainNamesDigestsWithoutValues(t *testing.T) {
	program := transformProgram(t)
	digest, _ := program.Digest()
	result, err := Execute(profileFor(t, digest), sourceRecord(), Programs{digest: program})
	if err != nil {
		t.Fatal(err)
	}
	text := Explain(result)
	if !strings.Contains(text, result.PayloadDigest) || !strings.Contains(text, result.Lineage.Digest()) || strings.Contains(text, "Grace") || strings.Contains(text, "W-7") || strings.Contains(text, digest) {
		t.Fatalf("unsafe explanation: %q", text)
	}
}
