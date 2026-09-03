// Package unsafeconversion is a TOOL-011 fixture: it converts an arbitrary
// uintptr to unsafe.Pointer, caught by vet's default unsafeptr analyzer.
package unsafeconversion

import "unsafe"

func BadConversion(x uintptr) unsafe.Pointer {
	return unsafe.Pointer(x)
}
