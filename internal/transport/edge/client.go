package edge

import (
	"context"

	"connectrpc.com/connect"

	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	registryv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/registry/v1"
)

// IntentClient calls the BusinessIntent lifecycle surface through the HTTP
// edge. It is hand-written rather than generated because the repository's
// generation toolchain (buf, protoc-gen-go, protoc-gen-go-grpc) does not
// include a connect generator; the shape is the one a generator would emit,
// so replacing it later is a deletion rather than a migration.
//
// Callers pass a *connect.Request so they control the outbound headers,
// including the credential. There is no convenience overload that sets a
// credential implicitly: a call that forgets to authenticate should fail
// loudly at the edge, not silently pick up an ambient identity.
type IntentClient struct {
	createIntent       *connect.Client[intentsv1.CreateIntentRequest, intentsv1.CreateIntentResponse]
	getIntent          *connect.Client[intentsv1.GetIntentRequest, intentsv1.GetIntentResponse]
	listIntents        *connect.Client[intentsv1.ListIntentsRequest, intentsv1.ListIntentsResponse]
	simulateIntent     *connect.Client[intentsv1.SimulateIntentRequest, intentsv1.SimulateIntentResponse]
	executeIntent      *connect.Client[intentsv1.ExecuteIntentRequest, intentsv1.ExecuteIntentResponse]
	submitIntent       *connect.Client[intentsv1.SubmitIntentRequest, intentsv1.SubmitIntentResponse]
	cancelIntent       *connect.Client[intentsv1.CancelIntentRequest, intentsv1.CancelIntentResponse]
	supersedeIntent    *connect.Client[intentsv1.SupersedeIntentRequest, intentsv1.SupersedeIntentResponse]
	explainIntent      *connect.Client[intentsv1.ExplainIntentRequest, intentsv1.ExplainIntentResponse]
	listIntentTimeline *connect.Client[intentsv1.ListIntentTimelineRequest, intentsv1.ListIntentTimelineResponse]
}

// NewIntentClient builds a client for the IntentService procedures published
// at baseURL, for example "http://127.0.0.1:8080".
func NewIntentClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *IntentClient {
	return &IntentClient{
		createIntent:       connect.NewClient[intentsv1.CreateIntentRequest, intentsv1.CreateIntentResponse](httpClient, baseURL+ProcedureCreateIntent, opts...),
		getIntent:          connect.NewClient[intentsv1.GetIntentRequest, intentsv1.GetIntentResponse](httpClient, baseURL+ProcedureGetIntent, opts...),
		listIntents:        connect.NewClient[intentsv1.ListIntentsRequest, intentsv1.ListIntentsResponse](httpClient, baseURL+ProcedureListIntents, opts...),
		simulateIntent:     connect.NewClient[intentsv1.SimulateIntentRequest, intentsv1.SimulateIntentResponse](httpClient, baseURL+ProcedureSimulateIntent, opts...),
		executeIntent:      connect.NewClient[intentsv1.ExecuteIntentRequest, intentsv1.ExecuteIntentResponse](httpClient, baseURL+ProcedureExecuteIntent, opts...),
		submitIntent:       connect.NewClient[intentsv1.SubmitIntentRequest, intentsv1.SubmitIntentResponse](httpClient, baseURL+ProcedureSubmitIntent, opts...),
		cancelIntent:       connect.NewClient[intentsv1.CancelIntentRequest, intentsv1.CancelIntentResponse](httpClient, baseURL+ProcedureCancelIntent, opts...),
		supersedeIntent:    connect.NewClient[intentsv1.SupersedeIntentRequest, intentsv1.SupersedeIntentResponse](httpClient, baseURL+ProcedureSupersedeIntent, opts...),
		explainIntent:      connect.NewClient[intentsv1.ExplainIntentRequest, intentsv1.ExplainIntentResponse](httpClient, baseURL+ProcedureExplainIntent, opts...),
		listIntentTimeline: connect.NewClient[intentsv1.ListIntentTimelineRequest, intentsv1.ListIntentTimelineResponse](httpClient, baseURL+ProcedureListIntentTimeline, opts...),
	}
}

// CreateIntent calls IntentService.CreateIntent through the edge.
func (c *IntentClient) CreateIntent(ctx context.Context, req *connect.Request[intentsv1.CreateIntentRequest]) (*connect.Response[intentsv1.CreateIntentResponse], error) {
	return c.createIntent.CallUnary(ctx, req)
}

// GetIntent calls IntentService.GetIntent through the edge.
func (c *IntentClient) GetIntent(ctx context.Context, req *connect.Request[intentsv1.GetIntentRequest]) (*connect.Response[intentsv1.GetIntentResponse], error) {
	return c.getIntent.CallUnary(ctx, req)
}

// ListIntents calls IntentService.ListIntents through the edge.
func (c *IntentClient) ListIntents(ctx context.Context, req *connect.Request[intentsv1.ListIntentsRequest]) (*connect.Response[intentsv1.ListIntentsResponse], error) {
	return c.listIntents.CallUnary(ctx, req)
}

// SimulateIntent calls IntentService.SimulateIntent through the edge.
func (c *IntentClient) SimulateIntent(ctx context.Context, req *connect.Request[intentsv1.SimulateIntentRequest]) (*connect.Response[intentsv1.SimulateIntentResponse], error) {
	return c.simulateIntent.CallUnary(ctx, req)
}

// ExecuteIntent calls IntentService.ExecuteIntent through the edge.
func (c *IntentClient) ExecuteIntent(ctx context.Context, req *connect.Request[intentsv1.ExecuteIntentRequest]) (*connect.Response[intentsv1.ExecuteIntentResponse], error) {
	return c.executeIntent.CallUnary(ctx, req)
}

// SubmitIntent calls IntentService.SubmitIntent through the edge.
func (c *IntentClient) SubmitIntent(ctx context.Context, req *connect.Request[intentsv1.SubmitIntentRequest]) (*connect.Response[intentsv1.SubmitIntentResponse], error) {
	return c.submitIntent.CallUnary(ctx, req)
}

// CancelIntent calls IntentService.CancelIntent through the edge.
func (c *IntentClient) CancelIntent(ctx context.Context, req *connect.Request[intentsv1.CancelIntentRequest]) (*connect.Response[intentsv1.CancelIntentResponse], error) {
	return c.cancelIntent.CallUnary(ctx, req)
}

// SupersedeIntent calls IntentService.SupersedeIntent through the edge.
func (c *IntentClient) SupersedeIntent(ctx context.Context, req *connect.Request[intentsv1.SupersedeIntentRequest]) (*connect.Response[intentsv1.SupersedeIntentResponse], error) {
	return c.supersedeIntent.CallUnary(ctx, req)
}

// ExplainIntent calls IntentService.ExplainIntent through the edge.
func (c *IntentClient) ExplainIntent(ctx context.Context, req *connect.Request[intentsv1.ExplainIntentRequest]) (*connect.Response[intentsv1.ExplainIntentResponse], error) {
	return c.explainIntent.CallUnary(ctx, req)
}

// ListIntentTimeline calls IntentService.ListIntentTimeline through the edge.
func (c *IntentClient) ListIntentTimeline(ctx context.Context, req *connect.Request[intentsv1.ListIntentTimelineRequest]) (*connect.Response[intentsv1.ListIntentTimelineResponse], error) {
	return c.listIntentTimeline.CallUnary(ctx, req)
}

// RegistryClient calls the discovery surface through the HTTP edge.
type RegistryClient struct {
	listIntentDefinitions *connect.Client[registryv1.ListIntentDefinitionsRequest, registryv1.ListIntentDefinitionsResponse]
	getIntentDefinition   *connect.Client[registryv1.GetIntentDefinitionRequest, registryv1.GetIntentDefinitionResponse]
	listCapabilities      *connect.Client[registryv1.ListCapabilitiesRequest, registryv1.ListCapabilitiesResponse]
	getCapability         *connect.Client[registryv1.GetCapabilityRequest, registryv1.GetCapabilityResponse]
}

// NewRegistryClient builds a client for the RegistryService procedures
// published at baseURL.
func NewRegistryClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *RegistryClient {
	return &RegistryClient{
		listIntentDefinitions: connect.NewClient[registryv1.ListIntentDefinitionsRequest, registryv1.ListIntentDefinitionsResponse](httpClient, baseURL+ProcedureListIntentDefinitions, opts...),
		getIntentDefinition:   connect.NewClient[registryv1.GetIntentDefinitionRequest, registryv1.GetIntentDefinitionResponse](httpClient, baseURL+ProcedureGetIntentDefinition, opts...),
		listCapabilities:      connect.NewClient[registryv1.ListCapabilitiesRequest, registryv1.ListCapabilitiesResponse](httpClient, baseURL+ProcedureListCapabilities, opts...),
		getCapability:         connect.NewClient[registryv1.GetCapabilityRequest, registryv1.GetCapabilityResponse](httpClient, baseURL+ProcedureGetCapability, opts...),
	}
}

// ListIntentDefinitions calls RegistryService.ListIntentDefinitions.
func (c *RegistryClient) ListIntentDefinitions(ctx context.Context, req *connect.Request[registryv1.ListIntentDefinitionsRequest]) (*connect.Response[registryv1.ListIntentDefinitionsResponse], error) {
	return c.listIntentDefinitions.CallUnary(ctx, req)
}

// GetIntentDefinition calls RegistryService.GetIntentDefinition.
func (c *RegistryClient) GetIntentDefinition(ctx context.Context, req *connect.Request[registryv1.GetIntentDefinitionRequest]) (*connect.Response[registryv1.GetIntentDefinitionResponse], error) {
	return c.getIntentDefinition.CallUnary(ctx, req)
}

// ListCapabilities calls RegistryService.ListCapabilities.
func (c *RegistryClient) ListCapabilities(ctx context.Context, req *connect.Request[registryv1.ListCapabilitiesRequest]) (*connect.Response[registryv1.ListCapabilitiesResponse], error) {
	return c.listCapabilities.CallUnary(ctx, req)
}

// GetCapability calls RegistryService.GetCapability.
func (c *RegistryClient) GetCapability(ctx context.Context, req *connect.Request[registryv1.GetCapabilityRequest]) (*connect.Response[registryv1.GetCapabilityResponse], error) {
	return c.getCapability.CallUnary(ctx, req)
}
