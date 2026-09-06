package transformation

import (
	"fmt"
	"strings"
)

// Version reports this engine's own contract version (ARCH-GO-009): every
// internal/engines/<name> package exposes its own Version(), independent of
// whatever version a particular definition happens to declare.
func Version() int { return ContractVersion }

// Explain renders a human-readable narrative of a transformation contract:
// its identity, owner, phase, source/destination schemas, and its bounded
// operation list. It is audit-log and review-screen shaped, not for
// programmatic branching -- branch on Validate()'s error instead.
func (d TransformationDefinition) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "transformation %s v%d (owner=%s phase=%s)", d.Name, d.Version, d.Owner, d.Phase)
	fmt.Fprintf(&b, "\n  source: %s@%d", d.Source.Name, d.Source.Version)
	fmt.Fprintf(&b, "\n  destination: %s@%d", d.Destination.Name, d.Destination.Version)
	fmt.Fprintf(&b, "\n  operations (%d, max %d):", len(d.Operations), d.Limits.MaxOperations)
	for i, op := range d.Operations {
		fmt.Fprintf(&b, "\n    %d. %s", i+1, explainOperation(op))
	}
	return b.String()
}

func explainOperation(op Operation) string {
	switch op.Kind {
	case OpCopy, OpRename:
		return fmt.Sprintf("%s %s.%s:%s -> %s.%s:%s", op.Kind, op.Source.Schema, op.Source.Field, op.Source.Type, op.Destination.Schema, op.Destination.Field, op.Destination.Type)
	case OpConvert:
		return fmt.Sprintf("convert %s.%s:%s -> %s.%s:%s (as %s)", op.Source.Schema, op.Source.Field, op.Source.Type, op.Destination.Schema, op.Destination.Field, op.Destination.Type, op.TargetType)
	case OpDefault:
		return fmt.Sprintf("default %s.%s:%s = %q", op.Destination.Schema, op.Destination.Field, op.Destination.Type, op.Literal)
	case OpConcat:
		parts := make([]string, len(op.Sources))
		for i, p := range op.Sources {
			parts[i] = fmt.Sprintf("%s.%s", p.Schema, p.Field)
		}
		return fmt.Sprintf("concat [%s] -> %s.%s:%s", strings.Join(parts, ", "), op.Destination.Schema, op.Destination.Field, op.Destination.Type)
	default:
		return fmt.Sprintf("%s -> %s.%s", op.Kind, op.Destination.Schema, op.Destination.Field)
	}
}
