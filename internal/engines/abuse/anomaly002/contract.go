package anomaly002

// Version and Explain provide the minimal ARCH-GO-009 shape for this
// subpackage. The package-level abuse engine remains the governed contract;
// this legacy-compatible evaluator is a pure implementation detail with its
// own explicit contract marker.
func Version() int { return 1 }

type Explanation struct {
	DetectorID string
	Version    string
	ActivityID string
	Inputs     []string
	Applicable bool
	Reason     string
}

func Explain(a Activity, d Definition) Explanation {
	exp := Explanation{
		DetectorID: d.ID, Version: d.Version, ActivityID: a.ID,
		Inputs: []string{"hour", "location", "volume", "scope", "sequence", "approved_work", "incident_work"},
	}
	if err := d.Validate(); err != nil {
		exp.Reason = "DEFINITION_INVALID"
		return exp
	}
	if err := a.Validate(); err != nil {
		exp.Reason = "ACTIVITY_INVALID"
		return exp
	}
	exp.Applicable = true
	exp.Reason = "DECLARED_ACTIVITY"
	return exp
}
