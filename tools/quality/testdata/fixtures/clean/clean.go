// Package clean is a TOOL-011 fixture: well-formatted, checks its errors,
// contains no dead code and no unsafe conversion. gofmt, go vet and
// staticcheck must all report nothing for it.
package clean

import "errors"

// Add returns a plus b.
func Add(a, b int) int {
	return a + b
}

// DoWork returns an error instead of discarding one.
func DoWork() error {
	return errors.New("something went wrong")
}
