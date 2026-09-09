package garnishment

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func garnishmentDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func releasedPayrollRun(t *testing.T) payroll.PayrollRun {
	t.Helper()
	run, err := payroll.NewPayrollRun("run-garn-005", "monthly",
		payroll.PeriodRef{ID: "period", Version: "v1", Digest: "sha256:period"},
		payroll.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "sha256:population"},
		"sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Calculate("sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Release("sha256:release")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func remittanceSpec(t *testing.T) RemittanceSpec {
	t.Helper()
	return RemittanceSpec{
		BatchID:                  "garn-batch-1",
		PayrollRunRef:            releasedPayrollRun(t).CanonicalDigest,
		PayrollWithholdingDigest: "sha256:withholding-manifest-1",
		PayrollWithholdingTotal:  garnishmentDecimal(t, "125.00"),
		Currency:                 "USD",
		Lines: []Withholding{
			{TenantID: "tenant-1", ID: "withholding-2", OrderRef: "order-2", PayeeRef: "payee:state", DestinationRef: "destination:state", PayrollLineRef: "payroll-line-2", Amount: garnishmentDecimal(t, "25.00"), Currency: "USD"},
			{TenantID: "tenant-1", ID: "withholding-1", OrderRef: "order-1", PayeeRef: "payee:state", DestinationRef: "destination:state", PayrollLineRef: "payroll-line-1", Amount: garnishmentDecimal(t, "100.00"), Currency: "USD"},
		},
	}
}

func remittanceBatch(t *testing.T) RemittanceBatch {
	t.Helper()
	batch, err := Generate(remittanceSpec(t))
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func settlementInstruction(t *testing.T, run payroll.PayrollRun, destination string) SettlementInstruction {
	t.Helper()
	instruction, err := NewSettlementInstruction("tenant-1", "instruction-garn-005", run.CanonicalDigest, "payee:state", destination, garnishmentDecimal(t, "125.00"), "USD")
	if err != nil {
		t.Fatal(err)
	}
	return instruction
}

func garnishmentPrincipal(t *testing.T, subject, session string) *trust.Principal {
	t.Helper()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("tenant-1"), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: session, IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(time.Hour),
		CredentialDigest: "sha256:" + strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func garnishmentAuthorizationRequest(t *testing.T, batch RemittanceBatch, instruction SettlementInstruction, requester, approver *trust.Principal) AuthorizationRequest {
	t.Helper()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	proof, err := NewStepUpProof("tenant-1", trust.AssuranceHigh, requester.Subject(), batch.CanonicalDigest, instruction.CanonicalDigest, at.Add(-time.Minute), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return AuthorizationRequest{TenantID: "tenant-1", Instruction: instruction, Requester: requester, Approver: approver, StepUpProof: proof, AuthorizationRef: "authz:settle-003:1", At: at}
}

// TestTodo_GARN_005 is the primary contract: remittance lines reconcile to
// payroll withholding and retain exact order/payee evidence.
func TestTodo_GARN_005(t *testing.T) {
	batch := remittanceBatch(t)
	if err := batch.Validate(); err != nil {
		t.Fatal(err)
	}
	if !batch.Total.Equal(garnishmentDecimal(t, "125.00")) || batch.PayeeRef != "payee:state" || batch.DestinationRef != "destination:state" {
		t.Fatalf("batch = %+v", batch)
	}
	if len(batch.Lines) != 2 || batch.Lines[0].OrderRef != "order-1" || batch.Lines[1].OrderRef != "order-2" {
		t.Fatalf("lines were not deterministically ordered: %+v", batch.Lines)
	}
	mixed := remittanceSpec(t)
	mixed.Lines[1].DestinationRef = "destination:other"
	if _, err := Generate(mixed); !errors.Is(err, ErrRemittanceRejected) {
		t.Fatalf("mixed destination error = %v, want ErrRemittanceRejected", err)
	}

	instruction := settlementInstruction(t, releasedPayrollRun(t), batch.DestinationRef)
	authorization, err := Authorize(batch, garnishmentAuthorizationRequest(t, batch, instruction, garnishmentPrincipal(t, "payroll-maker", "session-maker"), garnishmentPrincipal(t, "payroll-reviewer", "session-reviewer")))
	if err != nil {
		t.Fatal(err)
	}
	if err := authorization.ValidateFor(batch, instruction); err != nil {
		t.Fatalf("authorization validation = %v", err)
	}
	if authorization.BatchDigest != batch.CanonicalDigest || authorization.InstructionDigest != instruction.CanonicalDigest {
		t.Fatalf("authorization = %+v", authorization)
	}
	if Explain(batch) == "" || authorization.Explain() == "" {
		t.Fatal("explanations must be present")
	}
}

// TestTodo_GARN_005_Mutation proves approval is invalidated by a changed
// batch or a destination change and that dual control is fail-closed.
func TestTodo_GARN_005_Mutation(t *testing.T) {
	batch := remittanceBatch(t)
	run := releasedPayrollRun(t)
	instruction := settlementInstruction(t, run, batch.DestinationRef)
	authorization, err := Authorize(batch, garnishmentAuthorizationRequest(t, batch, instruction, garnishmentPrincipal(t, "maker", "session-maker"), garnishmentPrincipal(t, "reviewer", "session-reviewer")))
	if err != nil {
		t.Fatal(err)
	}

	changedBatch := batch
	changedBatch.Lines = append([]Withholding(nil), batch.Lines...)
	changedBatch.Lines[0].Amount = garnishmentDecimal(t, "99.00")
	if err := authorization.ValidateFor(changedBatch, instruction); !errors.Is(err, ErrBatchChanged) {
		t.Fatalf("changed batch error = %v, want ErrBatchChanged", err)
	}

	wrongDestination := settlementInstruction(t, run, "destination:other")
	if _, err := Authorize(batch, garnishmentAuthorizationRequest(t, batch, wrongDestination, garnishmentPrincipal(t, "maker", "session-maker"), garnishmentPrincipal(t, "reviewer", "session-reviewer"))); !errors.Is(err, ErrDestinationMismatch) {
		t.Fatalf("wrong destination error = %v, want ErrDestinationMismatch", err)
	}
	requester := garnishmentPrincipal(t, "maker", "session-maker")
	if _, err := Authorize(batch, garnishmentAuthorizationRequest(t, batch, instruction, requester, requester)); !errors.Is(err, ErrDualControlRequired) {
		t.Fatalf("self approval error = %v, want ErrDualControlRequired", err)
	}
	missing := garnishmentAuthorizationRequest(t, batch, instruction, requester, garnishmentPrincipal(t, "reviewer", "session-reviewer"))
	missing.StepUpProof = StepUpProof{}
	if _, err := Authorize(batch, missing); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("missing step-up error = %v, want ErrStepUpRequired", err)
	}
}

func TestGarnishmentTenantBindingAndStepUpProofFailures(t *testing.T) {
	spec := remittanceSpec(t)
	spec.Lines[1].TenantID = "tenant-2"
	if _, err := NewRemittance(spec); !errors.Is(err, ErrRemittanceRejected) {
		t.Fatalf("mixed-tenant batch error = %v", err)
	}
	batch := remittanceBatch(t)
	run := releasedPayrollRun(t)
	instruction, err := NewSettlementInstruction("tenant-2", "instruction-other-tenant", run.CanonicalDigest, "payee:state", batch.DestinationRef, garnishmentDecimal(t, "125.00"), "USD")
	if err != nil {
		t.Fatal(err)
	}
	requester := garnishmentPrincipal(t, "maker", "session-maker")
	approver := garnishmentPrincipal(t, "reviewer", "session-reviewer")
	request := garnishmentAuthorizationRequest(t, batch, instruction, requester, approver)
	request.TenantID = "tenant-2"
	if _, err := Authorize(batch, request); !errors.Is(err, ErrAuthorizationInvalid) {
		t.Fatalf("cross-tenant authorization error = %v", err)
	}
	instruction = settlementInstruction(t, run, batch.DestinationRef)
	request = garnishmentAuthorizationRequest(t, batch, instruction, requester, approver)
	request.StepUpProof.Subject = "forged-subject"
	if _, err := Authorize(batch, request); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("forged proof error = %v", err)
	}
	request = garnishmentAuthorizationRequest(t, batch, instruction, requester, approver)
	request.At = request.StepUpProof.ExpiresAt
	if _, err := Authorize(batch, request); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("expired proof error = %v", err)
	}
}

func TestGarnishmentAliasesAndValidationBoundaries(t *testing.T) {
	spec := remittanceSpec(t)
	fromGenerate, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	fromAlias, err := NewRemittanceBatch(spec)
	if err != nil || fromGenerate.CanonicalDigest != fromAlias.CanonicalDigest {
		t.Fatalf("constructor aliases generate=%+v alias=%+v err=%v", fromGenerate, fromAlias, err)
	}
	if Version() != 1 || Explain(fromGenerate) == "" {
		t.Fatalf("package metadata missing: version=%d explanation=%q", Version(), Explain(fromGenerate))
	}
	if digest, err := fromGenerate.Digest(); err != nil || digest != fromGenerate.CanonicalDigest {
		t.Fatalf("batch digest=%q err=%v", digest, err)
	}
	run := releasedPayrollRun(t)
	instruction := settlementInstruction(t, run, fromGenerate.DestinationRef)
	if err := instruction.Validate(); err != nil {
		t.Fatal(err)
	}
	request := garnishmentAuthorizationRequest(t, fromGenerate, instruction, garnishmentPrincipal(t, "maker", "session-maker"), garnishmentPrincipal(t, "reviewer", "session-reviewer"))
	authorization, err := AuthorizeRemittance(fromGenerate, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := authorization.Validate(); err != nil || authorization.ValidateFor(fromGenerate, instruction) != nil || authorization.Revalidate(fromGenerate, instruction) != nil {
		t.Fatalf("authorization validation err=%v validate_for=%v revalidate=%v", err, authorization.ValidateFor(fromGenerate, instruction), authorization.Revalidate(fromGenerate, instruction))
	}
	if authorization.Explain() == "" || authorization.StepUpProofDigest == "" || authorization.TenantID != "tenant-1" {
		t.Fatalf("authorization evidence = %+v", authorization)
	}
	badInstruction, err := NewSettlementInstruction("tenant-1", "", run.CanonicalDigest, "payee:state", fromGenerate.DestinationRef, garnishmentDecimal(t, "125.00"), "USD")
	if err == nil || !errors.Is(err, ErrInvalidRemittance) || !errors.Is(badInstruction.Validate(), ErrInvalidRemittance) {
		t.Fatalf("malformed instruction err=%v value=%+v", err, badInstruction)
	}
}
