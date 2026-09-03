// Package testcontainerskit records and checks LIB-009's test-only
// Testcontainers admission boundary. It deliberately has no Testcontainers
// import: the current decision is REJECT, so no runtime or test module graph
// is changed by this qualification fixture.
package testcontainerskit
