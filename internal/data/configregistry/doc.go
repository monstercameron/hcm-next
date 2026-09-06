// Package configregistry is the PostgreSQL adapter behind
// internal/platform/configregistry.Store (owner: DataOps/control-plane;
// phase: P1B minimal depth; CP-001).
//
// It is a separate package from internal/platform/configregistry on
// purpose, the same reason internal/intent/app/pgstore is separate from
// internal/intent/app: the platform package may depend on ports and never
// on a concrete adapter, and putting the SQL here is what makes that a
// compile-time fact rather than a convention. A composition root is the one
// place that names both packages.
//
// # What is here
//
// [Store] implements [configregistry.Store] over migrations/00027_config_object.sql's
// two tables:
//
//   - config_object — one immutable row per published revision.
//   - config_object_activation — one immutable row per governed activation
//     act, ordered by a caller-computed, gap-free activation_sequence per
//     (tenant, cell, kind, object_id) group.
//
// Every method opens its own short transaction, calls
// internal/data/tenancy.WithTenant to satisfy migration 00027's row level
// security policy, runs its statement(s), and commits — matching the
// pattern internal/data/governance's own tests exercise
// (inTenantTxErr). [configregistry.Scope.TenantID] must be the string form
// of the tenant's uuid (matching the tenant table's tenant_id); a value that
// does not parse as a uuid is refused rather than silently treated as "no
// tenant".
//
// # Context
//
// [configregistry.Store] deliberately carries no context.Context parameter
// -- internal/workflow/version's own Store port (CP-001's explicit shape
// reference) makes the identical choice, since the package it serves is
// meant to remain persistence-agnostic. This adapter therefore issues every
// query against context.Background() internally. A context-aware variant of
// the port is a deliberate future widening of
// internal/platform/configregistry.Store itself, not something this
// adapter can quietly invent on its own.
package configregistry
