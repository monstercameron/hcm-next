package model

import (
	"strconv"
	"testing"
)

func TestCompileQualityPatternReusesCompiledPatterns(t *testing.T) {
	first, err := compileQualityPattern(`^cache-[0-9]+$`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	second, err := compileQualityPattern(`^cache-[0-9]+$`)
	if err != nil {
		t.Fatalf("compile again: %v", err)
	}
	if first != second {
		t.Fatal("the second compile of the same pattern returned a different object; the cache was bypassed")
	}
	if _, err := compileQualityPattern(`(`); err == nil {
		t.Fatal("an invalid pattern compiled")
	}
	if _, cached := qualityPatternCache.Load(`(`); cached {
		t.Fatal("an invalid pattern was cached")
	}
}

func TestCompileQualityPatternCacheIsBounded(t *testing.T) {
	for i := 0; i < qualityPatternCacheCap+50; i++ {
		if _, err := compileQualityPattern(`^bound-` + strconv.Itoa(i) + `$`); err != nil {
			t.Fatalf("compile %d: %v", i, err)
		}
	}
	if size := qualityPatternCacheSize.Load(); size > qualityPatternCacheCap {
		t.Fatalf("cache size = %d, exceeds the cap %d", size, qualityPatternCacheCap)
	}
	// Patterns beyond the cap still compile, just without being retained.
	re, err := compileQualityPattern(`^beyond-the-cap$`)
	if err != nil || !re.MatchString("beyond-the-cap") {
		t.Fatalf("pattern beyond the cap: re=%v err=%v", re, err)
	}
}
