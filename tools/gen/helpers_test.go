// Package gen contains generation-toolchain and generated-contract tests for
// the todos owned by the proto/gen generator: PROTO-001, PROTO-002,
// PROTO-005, TOOL-002, TOOL-003 and TOOL-010.
package gen

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	capabilitiesv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/capabilities/v1"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
)

// opaqueCursor derives a page cursor that looks nothing like a raw integer
// offset, matching the "opaque signed cursor" requirement in
// planning/specs/http-grpc-endpoint-contract.md.
func opaqueCursor(seed string) string {
	sum := sha256.Sum256([]byte("hcmnext-cursor:" + seed))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// dialBufconn starts an in-memory gRPC server with the given registration
// callback and returns a client connection to it over bufconn (no real
// network socket). The server and connection are torn down automatically.
func dialBufconn(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()

	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)

	server := grpc.NewServer()
	register(server)

	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(server.Stop)

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Force the lazy connection to establish so the first RPC in a test does
	// not race the listener starting to serve.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Connect()
	for {
		state := conn.GetState()
		if state.String() == "READY" {
			break
		}
		if !conn.WaitForStateChange(ctx, state) {
			t.Fatalf("bufconn client never became ready (last state %s)", state)
		}
	}

	return conn
}

// kernelFamilies lists the three stable execution families every fixture set
// below must cover. See schema/proto/hcmnext/intents/v1/business_intent.proto
// KernelFamily and planning/specs/business-intent-and-change-request.md.
var kernelFamilies = []intentsv1.KernelFamily{
	intentsv1.KernelFamily_KERNEL_FAMILY_CHANGE_REQUEST,
	intentsv1.KernelFamily_KERNEL_FAMILY_CALCULATION_REQUEST,
	intentsv1.KernelFamily_KERNEL_FAMILY_ANALYTICAL_REQUEST,
}

func kernelFamilyName(k intentsv1.KernelFamily) string {
	switch k {
	case intentsv1.KernelFamily_KERNEL_FAMILY_CHANGE_REQUEST:
		return "change_request"
	case intentsv1.KernelFamily_KERNEL_FAMILY_CALCULATION_REQUEST:
		return "calculation_request"
	case intentsv1.KernelFamily_KERNEL_FAMILY_ANALYTICAL_REQUEST:
		return "analytical_request"
	default:
		return "unspecified"
	}
}

// fixtureIntentDefinitions returns one IntentDefinition per kernel family.
func fixtureIntentDefinitions() []*intentsv1.IntentDefinition {
	defs := make([]*intentsv1.IntentDefinition, 0, len(kernelFamilies))
	for _, kf := range kernelFamilies {
		name := kernelFamilyName(kf)
		defs = append(defs, &intentsv1.IntentDefinition{
			Reference: &intentsv1.DefinitionReference{
				IntentTypeId: "fixture." + name,
				Version:      1,
			},
			DisplayName:       "Fixture " + name,
			Description:       "Golden fixture definition for the " + name + " kernel family.",
			OwnerPlane:        "PLATFORM",
			OwnerDomain:       "test-fixtures",
			KernelFamily:      kf,
			Maturity:          intentsv1.DefinitionMaturity_DEFINITION_MATURITY_CONTRACTED,
			SideEffectProfile: intentsv1.SideEffectProfile_SIDE_EFFECT_PROFILE_READ_ONLY,
			RiskClass:         "low",
			PhaseDepth:        "P1A",
			SubjectKinds:      []string{"worker"},
		})
	}
	return defs
}

// fixtureIntentInstances returns one IntentInstance per kernel family, each
// bound to the matching fixtureIntentDefinitions entry.
func fixtureIntentInstances() []*intentsv1.IntentInstance {
	insts := make([]*intentsv1.IntentInstance, 0, len(kernelFamilies))
	for _, kf := range kernelFamilies {
		name := kernelFamilyName(kf)
		insts = append(insts, &intentsv1.IntentInstance{
			IntentId: "intent-" + name,
			Definition: &intentsv1.DefinitionReference{
				IntentTypeId: "fixture." + name,
				Version:      1,
			},
			TenantId:            "tenant-1",
			OrganizationScopeId: "org-1",
			Initiator: &intentsv1.PrincipalReference{
				PrincipalId: "user-1",
				Kind:        intentsv1.InitiatorKind_INITIATOR_KIND_HUMAN,
			},
			Purpose:        "fixture-purpose",
			IdempotencyKey: "idem-" + name,
			CorrelationId:  "corr-" + name,
			TraceId:        "trace-" + name,
			Classification: "internal",
			RetentionClass: "standard",
			Lifecycle: &intentsv1.LifecycleDimensions{
				Request:     intentsv1.RequestState_REQUEST_STATE_DRAFT,
				Execution:   intentsv1.ExecutionState_EXECUTION_STATE_NOT_PLANNED,
				Business:    intentsv1.BusinessState_BUSINESS_STATE_NOT_STARTED,
				Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_NOT_APPLICABLE,
				Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_NOT_APPLICABLE,
			},
			ExecutionMode:   intentsv1.ExecutionMode_EXECUTION_MODE_SIMULATE,
			InstanceVersion: 1,
		})
	}
	return insts
}

func fixtureCapabilities() []*capabilitiesv1.CapabilityDefinition {
	return []*capabilitiesv1.CapabilityDefinition{
		{
			CapabilityId: "fixture.capability.read",
			Version:      1,
			OwnerDomain:  "test-fixtures",
			RequestSchema: &capabilitiesv1.SchemaRef{
				SchemaId: "fixture.request", Version: 1, ProtobufFullName: "hcmnext.gen.test.FixtureRequest",
			},
			ResponseSchema: &capabilitiesv1.SchemaRef{
				SchemaId: "fixture.response", Version: 1, ProtobufFullName: "hcmnext.gen.test.FixtureResponse",
			},
			SideEffectProfile: capabilitiesv1.CapabilitySideEffectProfile_CAPABILITY_SIDE_EFFECT_PROFILE_READ_ONLY,
			ReadData: &capabilitiesv1.DataDomainFieldSet{
				DataDomains: []string{"worker"},
			},
			RiskClass:     "low",
			AgentEligible: false,
		},
	}
}
