// Package uncheckederror is a TOOL-011 fixture: it discards the result of a
// call vet's default unusedresult analyzer requires be used.
package uncheckederror

import "errors"

func DoWork() {
	errors.New("something went wrong")
}
