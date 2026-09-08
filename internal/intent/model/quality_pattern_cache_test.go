package model

import (
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func qualityCacheTestRule(pattern string) QualityRule {
	return QualityRule{RuleRef: "format", Kind: CheckFormat, Severity: SeverityBlocking, Paths: []string{"code"}, Pattern: pattern}
}

func qualityCacheTestFacts() map[string]QualityFact {
	return map[string]QualityFact{"code": {Present: true, Value: "ABC", Watermark: values.NewInstant(time.Unix(1, 0).UTC())}}
}

func TestQualityEvaluatorZeroAndNilReceivers(t *testing.T) {
	for name, evaluator := range map[string]*QualityEvaluator{"zero": {}, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			result, err := evaluator.EvaluateRule(qualityCacheTestRule(`^[A-Z]+$`), qualityCacheTestFacts(), values.NewInstant(time.Unix(2, 0).UTC()), 0, "owner")
			if err != nil {
				t.Fatalf("EvaluateRule: %v", err)
			}
			if result.Status != QualityPass {
				t.Fatalf("status = %s, want %s", result.Status, QualityPass)
			}
		})
	}
}

func TestQualityEvaluatorZeroAndNilReceiversRejectInvalidPatterns(t *testing.T) {
	for name, evaluator := range map[string]*QualityEvaluator{"zero": {}, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			_, err := evaluator.EvaluateRule(qualityCacheTestRule(`(`), qualityCacheTestFacts(), values.NewInstant(time.Unix(2, 0).UTC()), 0, "owner")
			if !errors.Is(err, ErrInvalidQualityRule) {
				t.Fatalf("error = %v, want ErrInvalidQualityRule", err)
			}
		})
	}
}

func TestEvaluateRuleCompilesFormatOncePerCall(t *testing.T) {
	var calls atomic.Int32
	compile := func(pattern string) (*regexp.Regexp, error) {
		calls.Add(1)
		return regexp.Compile(pattern)
	}
	result, err := evaluateRule(qualityCacheTestRule(`^[A-Z]+$`), qualityCacheTestFacts(), values.NewInstant(time.Unix(2, 0).UTC()), 0, "owner", compile)
	if err != nil {
		t.Fatalf("evaluateRule: %v", err)
	}
	if result.Status != QualityPass {
		t.Fatalf("status = %s, want %s", result.Status, QualityPass)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("compile calls = %d, want 1", got)
	}
}

func TestQualityEvaluatorMatchesPackageEvaluateRule(t *testing.T) {
	rule := qualityCacheTestRule(`^[A-Z]+$`)
	facts := qualityCacheTestFacts()
	asOf := values.NewInstant(time.Unix(2, 0).UTC())
	want, err := EvaluateRule(rule, facts, asOf, 0, "owner")
	if err != nil {
		t.Fatalf("package EvaluateRule: %v", err)
	}
	got, err := NewQualityEvaluator().EvaluateRule(rule, facts, asOf, 0, "owner")
	if err != nil {
		t.Fatalf("evaluator EvaluateRule: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluator result = %#v, want package result %#v", got, want)
	}
}

func BenchmarkQualityEvaluatorEvaluateRule(b *testing.B) {
	evaluator := NewQualityEvaluator()
	rule := qualityCacheTestRule(`^[A-Z]+$`)
	facts := qualityCacheTestFacts()
	asOf := values.NewInstant(time.Unix(2, 0).UTC())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := evaluator.EvaluateRule(rule, facts, asOf, 0, "owner"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCompileQualityPatternReusesCompiledPatterns(t *testing.T) {
	evaluator := NewQualityEvaluator()
	first, err := evaluator.compileQualityPattern(`^cache-[0-9]+$`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := evaluator.compileQualityPattern(`^cache-[0-9]+$`)
	if err != nil {
		t.Fatalf("compile again: %v", err)
	}
	if first != second {
		t.Fatal("the second compile of the same pattern returned a different object; the cache was bypassed")
	}
	if _, err := evaluator.compileQualityPattern(`(`); err == nil {
		t.Fatal("an invalid pattern compiled")
	}
	evaluator.mu.Lock()
	_, cached := evaluator.patterns[`(`]
	evaluator.mu.Unlock()
	if cached {
		t.Fatal("an invalid pattern was cached")
	}
}

func TestCompileQualityPatternCacheIsBounded(t *testing.T) {
	evaluator := NewQualityEvaluator()
	for i := 0; i < qualityPatternCacheCap+50; i++ {
		if _, err := evaluator.compileQualityPattern(`^bound-` + strconv.Itoa(i) + `$`); err != nil {
			t.Fatalf("compile %d: %v", i, err)
		}
	}
	evaluator.mu.Lock()
	size := len(evaluator.patterns)
	evaluator.mu.Unlock()
	if size > qualityPatternCacheCap {
		t.Fatalf("cache size = %d, exceeds the cap %d", size, qualityPatternCacheCap)
	}
	// Patterns beyond the cap still compile, just without being retained.
	re, err := evaluator.compileQualityPattern(`^beyond-the-cap$`)
	if err != nil || !re.MatchString("beyond-the-cap") {
		t.Fatalf("pattern beyond the cap: re=%v err=%v", re, err)
	}
}

func TestCompileQualityPatternCacheBoundedConcurrent(t *testing.T) {
	evaluator := NewQualityEvaluator()
	var wg sync.WaitGroup
	for i := 0; i < qualityPatternCacheCap*2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := evaluator.compileQualityPattern(`^concurrent-` + strconv.Itoa(i) + `$`); err != nil {
				t.Errorf("compile %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	evaluator.mu.Lock()
	size := len(evaluator.patterns)
	evaluator.mu.Unlock()
	if size > qualityPatternCacheCap {
		t.Fatalf("concurrent cache size = %d, exceeds cap %d", size, qualityPatternCacheCap)
	}
}
