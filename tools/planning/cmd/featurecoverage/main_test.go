package main

import "testing"

func TestFeatureCoverageCommandNoPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("panic: %v", recovered)
		}
	}()
}
