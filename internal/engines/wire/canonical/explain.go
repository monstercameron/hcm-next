package canonical

import (
	"sort"

	"google.golang.org/protobuf/proto"
)

// Contribution records one field that contributed bytes to a canonical stream.
type Contribution struct {
	// Index is the emission order within the stream.
	Index int
	// Path is the dotted material path. Repeated and map elements share the
	// path of the field that holds them.
	Path string
	// Kind is the canonical value kind that was emitted, e.g. "string",
	// "instant", "set", "absent".
	Kind string
	// Length is the byte length of the emitted value, including its kind tag.
	Length int
}

// Explanation answers "what did this digest actually bind?" without exposing
// the canonical bytes themselves, which may carry sensitive source data.
type Explanation struct {
	ProfileID      string
	ProfileVersion uint32
	SchemaID       string
	SchemaVersion  uint32
	MessageName    string
	// MaterialPaths is the profile's declared include list, sorted.
	MaterialPaths []string
	// Contributions lists the fields that were actually present and emitted,
	// in stream order.
	Contributions []Contribution
	// CanonicalLength is the length of the full canonical byte stream.
	CanonicalLength int
}

// ContributingPaths returns the distinct material paths that emitted bytes,
// sorted. A material path with no contribution was absent from the message.
func (x Explanation) ContributingPaths() []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(x.Contributions))
	for _, c := range x.Contributions {
		if c.Kind == "absent" || seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		out = append(out, c.Path)
	}
	sort.Strings(out)
	return out
}

// Explain encodes msg and reports which material paths contributed. It is the
// implementation of the canonicalize.explain capability.
func Explain(msg proto.Message, profile Profile) (Explanation, []byte, error) {
	plan, err := Compile(profile)
	if err != nil {
		return Explanation{}, nil, err
	}
	return plan.Explain(msg)
}

// Explain runs a precompiled plan and reports its contributions.
func (c *Plan) Explain(msg proto.Message) (Explanation, []byte, error) {
	out, trace, err := c.encode(msg, true)
	if err != nil {
		return Explanation{}, nil, err
	}
	return Explanation{
		ProfileID:       c.profile.ID,
		ProfileVersion:  c.profile.Version,
		SchemaID:        c.profile.SchemaID,
		SchemaVersion:   c.profile.SchemaVersion,
		MessageName:     string(c.profile.MessageName),
		MaterialPaths:   c.profile.MaterialPaths(),
		Contributions:   trace,
		CanonicalLength: len(out),
	}, out, nil
}
