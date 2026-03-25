package mcp

import (
	"context"

	mcptypes "github.com/mark3labs/mcp-go/mcp"
)

// mockMCPClient implements mcpclient.MCPClient for testing.
type mockMCPClient struct {
	callResult *mcptypes.CallToolResult
	callErr    error
}

func (m *mockMCPClient) Initialize(_ context.Context, _ mcptypes.InitializeRequest) (*mcptypes.InitializeResult, error) {
	return &mcptypes.InitializeResult{}, nil
}
func (m *mockMCPClient) Ping(_ context.Context) error { return nil }
func (m *mockMCPClient) ListResourcesByPage(_ context.Context, _ mcptypes.ListResourcesRequest) (*mcptypes.ListResourcesResult, error) {
	return &mcptypes.ListResourcesResult{}, nil
}
func (m *mockMCPClient) ListResources(_ context.Context, _ mcptypes.ListResourcesRequest) (*mcptypes.ListResourcesResult, error) {
	return &mcptypes.ListResourcesResult{}, nil
}
func (m *mockMCPClient) ListResourceTemplatesByPage(_ context.Context, _ mcptypes.ListResourceTemplatesRequest) (*mcptypes.ListResourceTemplatesResult, error) {
	return &mcptypes.ListResourceTemplatesResult{}, nil
}
func (m *mockMCPClient) ListResourceTemplates(_ context.Context, _ mcptypes.ListResourceTemplatesRequest) (*mcptypes.ListResourceTemplatesResult, error) {
	return &mcptypes.ListResourceTemplatesResult{}, nil
}
func (m *mockMCPClient) ReadResource(_ context.Context, _ mcptypes.ReadResourceRequest) (*mcptypes.ReadResourceResult, error) {
	return &mcptypes.ReadResourceResult{}, nil
}
func (m *mockMCPClient) Subscribe(_ context.Context, _ mcptypes.SubscribeRequest) error {
	return nil
}
func (m *mockMCPClient) Unsubscribe(_ context.Context, _ mcptypes.UnsubscribeRequest) error {
	return nil
}
func (m *mockMCPClient) ListPromptsByPage(_ context.Context, _ mcptypes.ListPromptsRequest) (*mcptypes.ListPromptsResult, error) {
	return &mcptypes.ListPromptsResult{}, nil
}
func (m *mockMCPClient) ListPrompts(_ context.Context, _ mcptypes.ListPromptsRequest) (*mcptypes.ListPromptsResult, error) {
	return &mcptypes.ListPromptsResult{}, nil
}
func (m *mockMCPClient) GetPrompt(_ context.Context, _ mcptypes.GetPromptRequest) (*mcptypes.GetPromptResult, error) {
	return &mcptypes.GetPromptResult{}, nil
}
func (m *mockMCPClient) ListToolsByPage(_ context.Context, _ mcptypes.ListToolsRequest) (*mcptypes.ListToolsResult, error) {
	return &mcptypes.ListToolsResult{}, nil
}
func (m *mockMCPClient) ListTools(_ context.Context, _ mcptypes.ListToolsRequest) (*mcptypes.ListToolsResult, error) {
	return &mcptypes.ListToolsResult{}, nil
}
func (m *mockMCPClient) CallTool(_ context.Context, _ mcptypes.CallToolRequest) (*mcptypes.CallToolResult, error) {
	return m.callResult, m.callErr
}
func (m *mockMCPClient) SetLevel(_ context.Context, _ mcptypes.SetLevelRequest) error {
	return nil
}
func (m *mockMCPClient) Complete(_ context.Context, _ mcptypes.CompleteRequest) (*mcptypes.CompleteResult, error) {
	return &mcptypes.CompleteResult{}, nil
}
func (m *mockMCPClient) Close() error                                                     { return nil }
func (m *mockMCPClient) OnNotification(_ func(notification mcptypes.JSONRPCNotification)) {}
