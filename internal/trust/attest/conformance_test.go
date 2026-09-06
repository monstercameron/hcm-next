package attest

import (
	"errors"
	"testing"
)

func conformanceResponse() Response {
	return Response{
		Status: ResponseAccepted, Kind: AssertionResponse, Digest: "response-digest", BindingDigest: "binding-digest",
		RecordedAt: testTrustedAt,
	}
}

func TestProveConformance_BoundariesAndErrors(t *testing.T) {
	cases := []struct {
		name        string
		cases       []ConformanceCase
		wantValid   bool
		wantInvalid string
	}{
		{name: "empty set", cases: nil, wantInvalid: "domains"},
		{name: "unknown domain", cases: []ConformanceCase{{Domain: AcknowledgementDomain("OTHER"), Claim: ClaimLegalFact, Response: conformanceResponse()}}, wantInvalid: "cases[0]"},
		{name: "blank claim", cases: []ConformanceCase{{Domain: DomainTime, Response: conformanceResponse()}}, wantInvalid: "cases[0]"},
		{name: "invalid status", cases: []ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: Response{Status: ResponseStatus("BOGUS"), Digest: "d", BindingDigest: "b", RecordedAt: testTrustedAt}}}, wantInvalid: "cases[0]"},
		{name: "missing digest", cases: []ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: Response{Status: ResponseAccepted, BindingDigest: "b", RecordedAt: testTrustedAt}}}, wantInvalid: "cases[0]"},
		{name: "missing binding", cases: []ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: Response{Status: ResponseAccepted, Digest: "d", RecordedAt: testTrustedAt}}}, wantInvalid: "cases[0]"},
		{name: "untrusted recorded time", cases: []ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: Response{Status: ResponseAccepted, Digest: "d", BindingDigest: "b"}}}, wantInvalid: "cases[0]"},
		{name: "duplicate domain", cases: []ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: conformanceResponse()}, {Domain: DomainTime, Claim: ClaimMealBreak, Response: conformanceResponse()}}, wantInvalid: "duplicate domain"},
		{name: "missing required domain", cases: []ConformanceCase{{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: conformanceResponse()}, {Domain: DomainPayroll, Claim: ClaimPayrollInputCompleteness, Response: conformanceResponse()}}, wantInvalid: "domains"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProveConformance(tc.cases)
			if tc.wantValid {
				if err != nil || !got.Valid || got.Cases != 3 || len(got.Domains) != 3 {
					t.Fatalf("report=%+v err=%v", got, err)
				}
				return
			}
			if err == nil || !errors.Is(err, ErrConformance) || got.Invalid != tc.wantInvalid || got.Valid {
				t.Fatalf("report=%+v err=%v, want invalid %q and ErrConformance", got, err, tc.wantInvalid)
			}
		})
	}

	got, err := ProveConformance([]ConformanceCase{
		{Domain: DomainTime, Claim: ClaimTimecardAccuracy, Response: conformanceResponse()},
		{Domain: DomainPayroll, Claim: ClaimPayrollInputCompleteness, Response: conformanceResponse()},
		{Domain: DomainLegal, Claim: ClaimLegalFact, Response: conformanceResponse()},
	})
	if err != nil || !got.Valid || !got.SharedEvidence || !got.DistinctFromApproval || !got.DistinctFromSignature || !got.DistinctFromForm {
		t.Fatalf("valid conformance report=%+v err=%v", got, err)
	}
}
