// Package cleanpkg is a compiler_test.go fixture: a package with a real Test
// function and no forbidden imports, used for the MISSING/STALE/OUT_OF_MANIFEST/OK
// compiler cases that are not about the effect-bearing check.
package cleanpkg

import "testing"

func TestCleanPackageProbe(t *testing.T) {}
