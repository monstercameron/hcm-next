// Package ephemeralenv is the TOOL-014 test-only integration environment
// harness. It gives one test run private filesystem/object, in-memory queue,
// and fake-provider namespaces, records cleanup evidence, and deletes only
// the run directory it created.
//
// It intentionally uses no production adapters or external services. A test
// that needs PostgreSQL continues to use internal/data/pgtest; this package
// closes the object, queue, and provider-fake half of the same isolation
// contract. PreserveOnFailure is the sole opt-in diagnostic retention policy.
package ephemeralenv
