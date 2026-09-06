package main

import (
	"sync"
	"time"
)

const navigationFilterDebounceDelay = 250 * time.Millisecond

type debounceTimer interface {
	Stop() bool
}

type debounceScheduler func(time.Duration, func()) debounceTimer

// navigationDebouncer keeps rapid menu-filter input from writing stale address
// state. It is independent of the DOM so cancellation and timing can be tested
// natively while the production adapter uses the browser event-loop timer.
type navigationDebouncer struct {
	mu         sync.Mutex
	generation uint64
	timer      debounceTimer
	navigate   func(string)
	schedule   debounceScheduler
}

func newNavigationDebouncer(navigate func(string)) *navigationDebouncer {
	return newNavigationDebouncerWithScheduler(navigate, func(delay time.Duration, callback func()) debounceTimer {
		return time.AfterFunc(delay, callback)
	})
}

func newNavigationDebouncerWithScheduler(navigate func(string), schedule debounceScheduler) *navigationDebouncer {
	return &navigationDebouncer{navigate: navigate, schedule: schedule}
}

func (d *navigationDebouncer) Schedule(href string) {
	if d == nil || d.schedule == nil {
		return
	}
	d.mu.Lock()
	d.generation++
	generation := d.generation
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = d.schedule(navigationFilterDebounceDelay, func() {
		d.mu.Lock()
		if generation != d.generation {
			d.mu.Unlock()
			return
		}
		d.timer = nil
		navigate := d.navigate
		d.mu.Unlock()
		if navigate != nil {
			navigate(href)
		}
	})
	d.mu.Unlock()
}

func (d *navigationDebouncer) Cancel() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.generation++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()
}
