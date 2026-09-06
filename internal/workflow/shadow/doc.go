// Package shadow runs a compiled workflow against an isolated copy-on-write
// view. It records the business terminal the real execution would have
// written, while refusing every external effect and keeping work items and
// timers in memory.
package shadow
