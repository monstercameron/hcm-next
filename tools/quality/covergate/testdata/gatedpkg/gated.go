// Package gatedpkg is the fixture the covergate tests measure, so the gate
// never has to run its own test package recursively.
package gatedpkg

// Double returns twice n.
func Double(n int) int { return n * 2 }
