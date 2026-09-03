// Package racefixture is a TOOL-012 fixture pair: RacyCounter has a
// deliberate unsynchronized shared-state race (detected by
// `go test -race`), and SynchronizedCounter performs the identical
// operation with a mutex, proving the race detector does not false-positive
// on correct code. Neither type ships in the release image; this package
// lives under testdata so `go build/vet/test ./...` skip it automatically.
package racefixture

import "sync"

// RacyCounter increments an int from two goroutines with no synchronization
// at all.
func RacyCounter() int {
	counter := 0
	done := make(chan struct{})

	go func() {
		counter++
		close(done)
	}()

	counter++
	<-done

	return counter
}

// SynchronizedCounter performs the same increment from two goroutines, but
// guards the shared counter with a mutex.
func SynchronizedCounter() int {
	var mu sync.Mutex
	counter := 0
	done := make(chan struct{})

	go func() {
		mu.Lock()
		counter++
		mu.Unlock()
		close(done)
	}()

	mu.Lock()
	counter++
	mu.Unlock()
	<-done

	mu.Lock()
	defer mu.Unlock()
	return counter
}
