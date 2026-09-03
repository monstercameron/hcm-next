package hrcase

import "strings"

// HRRequest is the authoritative intake envelope. Caller supplied values are
// explicit; no requester, subject, purpose or classification is inferred.
type HRRequest struct {
	CaseID, Type, TypeVersion, Service string
	Requester, Subject, Purpose        string
	Classification, Retention          string
	Participants                       []Participant
}
type Request = HRRequest

func NewHRRequest(r HRRequest) (HRRequest, error) {
	if err := r.Validate(); err != nil {
		return HRRequest{}, err
	}
	r.Participants = append([]Participant(nil), r.Participants...)
	return r, nil
}

func (r HRRequest) Validate() error {
	for _, x := range []struct{ n, v string }{{"case_id", r.CaseID}, {"type", r.Type}, {"type_version", r.TypeVersion}, {"service", r.Service}, {"requester", r.Requester}, {"subject", r.Subject}, {"purpose", r.Purpose}, {"classification", r.Classification}, {"retention", r.Retention}} {
		if strings.TrimSpace(x.v) == "" {
			return invalidRequest(x.n)
		}
	}
	return nil
}
func invalidRequest(field string) error { return &requestError{field: field} }

type requestError struct{ field string }

func (e *requestError) Error() string { return "hrcase: missing or blank " + e.field }
func (e *requestError) Unwrap() error { return ErrInvalidRequest }

func (r HRRequest) Definition() CaseDefinition {
	return CaseDefinition{Type: r.Type, Version: r.TypeVersion, Service: r.Service, Purpose: r.Purpose, Classification: r.Classification, Retention: r.Retention}
}
func (r HRRequest) Revision() (CaseRevision, error) {
	if err := r.Validate(); err != nil {
		return CaseRevision{}, err
	}
	return NewCaseRevision(CaseRevision{CaseID: r.CaseID, Revision: 1, Definition: r.Definition(), Requester: r.Requester, Subject: r.Subject, Purpose: r.Purpose, Classification: r.Classification, Retention: r.Retention, Participants: append([]Participant(nil), r.Participants...), State: Draft})
}
