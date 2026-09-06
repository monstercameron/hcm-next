// Package libqualification records library and tool qualification decisions
// (LIB-* and TOOL-* todos) with their conformance tests.
//
// This package owns:
// - LIB-005 (CEL-Go deferral): Expression backend deferred pending bounded rule DSL
// - LIB-009 (Testcontainers rejection): Docker unavailable; pgtest covers ephemeral needs
// - LIB-010 (OIDC/OAuth2 deferral): go-oidc and x/oauth2 confined to federation adapter
// - LIB-011 (JOSE/JWK gating): no additional JOSE module admitted without demonstrated need and conformance
// - LIB-012 (Standard library preference): Go stdlib logging, crypto, networking, testing; third-party frameworks forbidden without decision
// - LIB-014 (Dependency replacement safety): every infrastructure dependency has proven upgrade/rollback/replacement path with documented procedure
// - LIB-019 (Buf and Protovalidate qualification): Buf adopted; Protovalidate rejected
// - TOOL-022 (Toxiproxy rejection): Docker unavailable; in-process fault injection covers testing
//
// Related qualification records that cannot live in existing firewall or
// dependency-manifest packages.
package libqualification
