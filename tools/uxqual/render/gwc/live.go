package gwc

// LiveBinding is exactly what the live renderer needs to re-bind the tree it
// mounts to the workspace that served the document: the form's route, the
// request form's id, and the hidden inputs the server bound into that form
// (csrf, worker, locale). The workspace package produces it; the field names
// are the wire contract between the two.
type LiveBinding struct {
	Action string            `json:"action"`
	FormID string            `json:"form_id"`
	Hidden map[string]string `json:"hidden"`
}
