package documentredact

import (
	"context"
	"errors"
	"testing"
)

func TestPolicyAndSurfaceValidation(t *testing.T) {
	for _, surface := range []Surface{Preview, Thumbnail, OCR, Search, Export} {
		if !validSurface(surface) {
			t.Fatalf("surface %q rejected", surface)
		}
	}
	if validSurface("RAW") {
		t.Fatal("unknown surface accepted")
	}
	base := request().Policy
	if _, _, err := base.ranges(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Policy)
	}{
		{"id", func(p *Policy) { p.ID = " " }},
		{"version", func(p *Policy) { p.Version = "" }},
		{"purpose", func(p *Policy) { p.Purpose = "" }},
		{"recipient", func(p *Policy) { p.Recipient = "" }},
		{"field path", func(p *Policy) { p.Fields[0].Path = " " }},
		{"no ranges", func(p *Policy) { p.Regions = nil; p.Fields = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.Regions = append([]Region(nil), base.Regions...)
			bad.Fields = append([]Field(nil), base.Fields...)
			tc.edit(&bad)
			if _, _, err := bad.ranges(); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("ranges error = %v", err)
			}
		})
	}
	if err := validateRanges([]Region{{Start: 2, End: 2}}, 10); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty range = %v", err)
	}
	if err := validateRanges([]Region{{Start: 1, End: 11}}, 10); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("out-of-bounds range = %v", err)
	}
	if err := validateRanges([]Region{{Start: 1, End: 2}}, 10); err != nil {
		t.Fatal(err)
	}
}

func TestGenerate_RequestContextAndPolicyBoundaries(t *testing.T) {
	valid := request()
	//lint:ignore SA1012 deliberate nil context: Generate must succeed with a nil context for a valid request.
	if _, err := Generate(nil, valid); err != nil {
		t.Fatalf("nil context = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Generate(canceled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context = %v", err)
	}
	cases := []struct {
		name string
		edit func(*Request)
		want error
	}{
		{"source id", func(r *Request) { r.Source.ID = "" }, ErrInvalidRequest},
		{"source bytes", func(r *Request) { r.Source.Bytes = nil }, ErrInvalidRequest},
		{"source digest", func(r *Request) { r.Source.Digest = "bad" }, ErrInvalidRequest},
		{"no surfaces", func(r *Request) { r.Surfaces = nil }, ErrInvalidRequest},
		{"duplicate surface", func(r *Request) { r.Surfaces = []Surface{Preview, Preview} }, ErrInvalidRequest},
		{"unknown surface", func(r *Request) { r.Surfaces = []Surface{"RAW"} }, ErrInvalidRequest},
		{"bad field range", func(r *Request) { r.Policy.Fields[0].Region = Region{Start: 999, End: 1000} }, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := valid
			bad.Source.Bytes = append([]byte(nil), valid.Source.Bytes...)
			bad.Surfaces = append([]Surface(nil), valid.Surfaces...)
			bad.Policy.Regions = append([]Region(nil), valid.Policy.Regions...)
			bad.Policy.Fields = append([]Field(nil), valid.Policy.Fields...)
			tc.edit(&bad)
			if _, err := Generate(context.Background(), bad); !errors.Is(err, tc.want) {
				t.Fatalf("Generate error = %v, want %v", err, tc.want)
			}
		})
	}
	result, err := Redact(context.Background(), valid)
	if err != nil || result.OutputDigest == "" {
		t.Fatalf("Redact = %+v, %v", result, err)
	}
}

func TestResultVerifyRejectsEveryBindingMutation(t *testing.T) {
	requestValue := request()
	result, err := Generate(context.Background(), requestValue)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Verify(requestValue); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Result)
	}{
		{"source digest", func(r *Result) { r.SourceDigest = "changed" }},
		{"proof incomplete", func(r *Result) { r.Proof.Complete = false }},
		{"proof digest", func(r *Result) { r.Proof.Digest = "changed" }},
		{"proof regions", func(r *Result) { r.Proof.Regions = nil }},
		{"proof fields", func(r *Result) { r.Proof.Fields = nil }},
		{"evidence policy", func(r *Result) { r.Evidence.PolicyID = "changed" }},
		{"evidence version", func(r *Result) { r.Evidence.PolicyVersion = "changed" }},
		{"evidence output", func(r *Result) { r.Evidence.OutputDigest = "changed" }},
		{"evidence coverage", func(r *Result) { r.Evidence.CoverageDigest = "changed" }},
		{"surface missing", func(r *Result) { delete(r.Surfaces, Preview) }},
		{"surface digest", func(r *Result) { v := r.Surfaces[Preview]; v.Digest = "changed"; r.Surfaces[Preview] = v }},
		{"surface bytes", func(r *Result) { r.Surfaces[Preview] = SurfaceResult{Digest: r.OutputDigest, Bytes: []byte("leak")} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := result
			mutated.Surfaces = make(map[Surface]SurfaceResult, len(result.Surfaces))
			for key, value := range result.Surfaces {
				value.Bytes = append([]byte(nil), value.Bytes...)
				mutated.Surfaces[key] = value
			}
			tc.mutate(&mutated)
			if !errors.Is(mutated.Verify(requestValue), ErrRedactionIncomplete) {
				t.Fatal("mutated result was accepted")
			}
		})
	}
	if !sameRegions([]Region{{Start: 1, End: 2}}, []Region{{Start: 1, End: 2}}) || sameRegions(nil, []Region{{Start: 1, End: 2}}) {
		t.Fatal("sameRegions comparison failed")
	}
	if !sameStrings([]string{"a"}, []string{"a"}) || sameStrings([]string{"a"}, []string{"b"}) {
		t.Fatal("sameStrings comparison failed")
	}
	if !oracle([]byte("abcd"), []byte("abce"), []Region{{Start: 3, End: 4}}) || oracle([]byte("abc"), []byte("abcd"), nil) {
		t.Fatal("oracle comparison failed")
	}
	if !errors.Is(incomplete(), ErrRedactionIncomplete) {
		t.Fatal("incomplete did not wrap ErrRedactionIncomplete")
	}
}
