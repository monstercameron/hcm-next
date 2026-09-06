package main

import (
	"reflect"
	"testing"
	"time"
)

type fakeDebounceTimer struct{ stopped bool }

func (t *fakeDebounceTimer) Stop() bool {
	wasActive := !t.stopped
	t.stopped = true
	return wasActive
}

type scheduledDebounce struct {
	delay    time.Duration
	callback func()
	timer    *fakeDebounceTimer
}

func TestNavigationDebouncerUses250MillisecondsAndRejectsStaleCallbacks(t *testing.T) {
	var calls []string
	var scheduled []*scheduledDebounce
	debouncer := newNavigationDebouncerWithScheduler(func(href string) {
		calls = append(calls, href)
	}, func(delay time.Duration, callback func()) debounceTimer {
		entry := &scheduledDebounce{delay: delay, callback: callback, timer: &fakeDebounceTimer{}}
		scheduled = append(scheduled, entry)
		return entry.timer
	})

	debouncer.Schedule("/workspace/app/home?menu_q=w")
	debouncer.Schedule("/workspace/app/home?menu_q=work")
	if len(scheduled) != 2 || scheduled[0].delay != 250*time.Millisecond || scheduled[1].delay != 250*time.Millisecond {
		t.Fatalf("scheduled debounce = %+v", scheduled)
	}
	if !scheduled[0].timer.stopped {
		t.Fatal("second input did not stop the first timer")
	}
	scheduled[0].callback()
	if len(calls) != 0 {
		t.Fatalf("stale callback navigated: %v", calls)
	}
	scheduled[1].callback()
	if !reflect.DeepEqual(calls, []string{"/workspace/app/home?menu_q=work"}) {
		t.Fatalf("navigation calls = %v", calls)
	}

	debouncer.Schedule("/workspace/app/home?menu_q=people")
	latest := scheduled[len(scheduled)-1]
	debouncer.Cancel()
	latest.callback()
	if !latest.timer.stopped || len(calls) != 1 {
		t.Fatalf("cancelled timer state=%+v calls=%v", latest.timer, calls)
	}
}

func TestProductRouteFocusTargetKeepsMenuInputForQueryOnlyChanges(t *testing.T) {
	selector, caret := productRouteFocusTarget(
		"/workspace/app/home?favorites=people",
		"/workspace/app/home?favorites=people&menu_q=work",
	)
	if selector != menuFilterFocusSelector || !caret {
		t.Fatalf("menu-filter target = %q caret=%t", selector, caret)
	}

	for _, pair := range [][2]string{
		{"/workspace/app/home?menu_q=work", "/workspace/app/people?menu_q=work"},
		{"/workspace/app/home?menu_q=work", "/workspace/app/home?menu_q=work&nav=collapsed"},
	} {
		selector, caret = productRouteFocusTarget(pair[0], pair[1])
		if selector != productPageFocusSelector || caret {
			t.Fatalf("ordinary route %q -> %q target=%q caret=%t", pair[0], pair[1], selector, caret)
		}
	}
}
