// Package impossiblebranch is a TOOL-011 fixture: it contains dead code
// after an unconditional return, caught by vet's default unreachable
// analyzer.
package impossiblebranch

func Compute(x int) int {
	return x * 2
	x = x + 1
	return x
}
