// Package survey owns the domain model for survey campaigns, question banks,
// and responses. Nothing in this package stores, sends, or archives data —
// it only defines the immutable revision structures for configuration and
// policy. Storage, transmission, and retention are the responsibility of
// external systems.
//
// Semantic owner: BI/Experience. Phase: P1A.
package survey
