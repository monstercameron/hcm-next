package migrate

import (
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/migrationpreview"
)

// PreviewRecord is the immutable evidence one [migrationpreview.Preview] call
// produced, sealed together with the exact compiled-plan digests it was
// classified against. An [Approval] names this record's own [PreviewRecord.
// Digest]; [Migrate] refuses the moment either captured digest no longer
// matches the plan a caller presents ([CodeStalePreview]), which is what "the
// preview went stale" means in a system where a definition's identity is
// nothing but its own content digest.
type PreviewRecord struct {
	SourceWorkflowID string
	SourceVersion    uint32
	SourceDigest     string
	TargetWorkflowID string
	TargetVersion    uint32
	TargetDigest     string
	Result           migrationpreview.Result

	digest string
}

// Digest is the record's content identity.
func (p PreviewRecord) Digest() string { return p.digest }

type previewRecordIdentity struct {
	SourceWorkflowID string
	SourceVersion    uint32
	SourceDigest     string
	TargetWorkflowID string
	TargetVersion    uint32
	TargetDigest     string
	Result           migrationpreview.Result
}

// CapturePreview seals one [migrationpreview.Result] together with the exact
// source/target compiled-plan digests it was computed from. It never reruns
// [migrationpreview.Preview] itself: this package trusts a caller that
// already ran the preview (WF-RUN-017) and hands back its own result, and
// only pins what a later, separate [Migrate] call must be able to detect has
// drifted.
func CapturePreview(source, target *workflow.CompiledWorkflow, result migrationpreview.Result) (PreviewRecord, error) {
	if source == nil || target == nil {
		return PreviewRecord{}, refuse(CodeInvalidRequest, "", "source and target compiled plans are required to capture a preview")
	}
	if result.SourceWorkflowID != source.WorkflowID || result.TargetWorkflowID != target.WorkflowID {
		return PreviewRecord{}, refuse(CodePlanMismatch, "",
			"preview result names workflows %q/%q; supplied plans name %q/%q",
			result.SourceWorkflowID, result.TargetWorkflowID, source.WorkflowID, target.WorkflowID)
	}
	if result.SourceVersion != source.Version || result.TargetVersion != target.Version {
		return PreviewRecord{}, refuse(CodePlanMismatch, "",
			"preview result names versions %d/%d; supplied plans name %d/%d",
			result.SourceVersion, result.TargetVersion, source.Version, target.Version)
	}

	rec := PreviewRecord{
		SourceWorkflowID: source.WorkflowID, SourceVersion: source.Version, SourceDigest: source.Digest(),
		TargetWorkflowID: target.WorkflowID, TargetVersion: target.Version, TargetDigest: target.Digest(),
		Result: result,
	}
	rec.digest = canonicalDigest(previewRecordDigestProfile, previewRecordIdentity{
		SourceWorkflowID: rec.SourceWorkflowID, SourceVersion: rec.SourceVersion, SourceDigest: rec.SourceDigest,
		TargetWorkflowID: rec.TargetWorkflowID, TargetVersion: rec.TargetVersion, TargetDigest: rec.TargetDigest,
		Result: rec.Result,
	})
	return rec, nil
}

// Assessment returns the classification the captured preview reached for one
// live instance, or false when the preview never named it: an instance a
// preview never assessed carries no migration authority to stand on.
func (p PreviewRecord) Assessment(instanceID string) (migrationpreview.Assessment, bool) {
	for _, a := range p.Result.Assessments {
		if a.InstanceID == instanceID {
			return a, true
		}
	}
	return migrationpreview.Assessment{}, false
}
