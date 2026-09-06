// Package knowledge holds the vocabulary for versioned knowledge articles that
// are published with effective intervals, audience scope, classification, and
// citations. A knowledge article must declare its owner, authority, audience,
// jurisdiction, and review lifecycle; the article body is immutable and stored
// as an artifact reference, never inline. Supersession links let older articles
// become stale without deletion.
//
// Semantic owner: domains (shared). Phase: PHASE_3.
//
// The whole point of a knowledge article is that it carries explicit binding:
// classification, effective dates, audience scope, source authority, and
// citation evidence. An article that cannot declare these is refused.
package knowledge
