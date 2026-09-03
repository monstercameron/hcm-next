package intentdefinitions

// Result is a deterministic checker result suitable for CI or a generated
// manifest. Digest is empty when validation fails.
type Result struct {
	Valid  bool     `json:"valid"`
	Digest string   `json:"digest,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// Check validates a descriptor set and computes its canonical digest.
func Check(ds []Descriptor) Result {
	if err := Validate(ds); err != nil {
		return Result{Errors: []string{err.Error()}}
	}
	d, err := Digest(ds)
	if err != nil {
		return Result{Errors: []string{err.Error()}}
	}
	return Result{Valid: true, Digest: d}
}
