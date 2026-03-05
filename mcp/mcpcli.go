// Package mcpcli provides Model Context Protocol (MCP) client functionality for integrating
// external tools and services with AI language models. It handles tool discovery,
// execution, and result formatting for seamless AI-tool interactions.
//
// The MCP protocol allows AI models to interact with external tools and services
// in a standardized way, enabling capabilities like file system access, API calls,
// database queries, and more through a unified interface.
package mcpcli

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/xyzj/toolbox/crypto"
	"github.com/xyzj/toolbox/json"
	"github.com/xyzj/toolbox/mapfx"
)

type (
	Opt struct {
		timeout time.Duration
		header  map[string]string
		reload  bool
	}
	Opts func(opt *Opt)
)

func WithReload(reload bool) Opts {
	return func(opt *Opt) {
		opt.reload = reload
	}
}
func WithTimeout(t time.Duration) Opts {
	return func(opt *Opt) {
		opt.timeout = t
	}
}

func WithHeader(m map[string]string) Opts {
	return func(opt *Opt) {
		opt.header = m
	}
}

type customTool struct {
	locker sync.RWMutex
	data   map[string]func(*model.ToolCall) (*model.ChatCompletionMessage, error)
}

func (ct *customTool) Store(name string, f func(tc *model.ToolCall) (*model.ChatCompletionMessage, error)) {
	ct.locker.Lock()
	ct.data[name] = f
	ct.locker.Unlock()
}

func (ct *customTool) Delete(name string) {
	ct.locker.Lock()
	delete(ct.data, name)
	ct.locker.Unlock()
}
func (ct *customTool) Load(name string) (func(tc *model.ToolCall) (*model.ChatCompletionMessage, error), bool) {
	ct.locker.RLock()
	if f, ok := ct.data[name]; ok {
		ct.locker.RUnlock()
		return f, ok
	}
	ct.locker.RUnlock()
	return nil, false
}
func (ct *customTool) Len() int {
	ct.locker.RLock()
	defer ct.locker.RUnlock()
	return len(ct.data)
}

// New creates a new McpClient instance for managing MCP server connections and tools.
// The client can connect to multiple MCP servers and aggregate their tools into
// a unified interface for AI models to use.
//
// Returns a new McpClient ready to connect to MCP servers and manage tools.
func New() *McpClient {
	return &McpClient{
		clis:  make(map[string]*mclient),
		idx:   make(map[string]string),
		tools: mapfx.NewUniqueSlice[*model.Tool](),
		customTool: &customTool{
			locker: sync.RWMutex{},
			data:   make(map[string]func(*model.ToolCall) (*model.ChatCompletionMessage, error)),
		},
	}
}

// mclient represents a connection to a single MCP server.
// It maintains the server URI and the active client connection.
type mclient struct {
	uri string         // URI of the MCP server
	cli *client.Client // Active client connection to the MCP server
}

// McpClient manages multiple MCP server connections and provides a unified
// interface for tool discovery, execution, and management.
//
// Key features:
//   - Multiple MCP server support with connection pooling
//   - Automatic tool discovery and schema conversion
//   - Tool call routing to appropriate MCP servers
//   - Deduplication of tools across servers
//   - Connection lifecycle management with timeouts
type McpClient struct {
	clis       map[string]*mclient             // Map of MCP server connections (keyed by SHA1 hash of URI)
	idx        map[string]string               // Tool name to server key mapping for routing
	tools      *mapfx.UniqueSlice[*model.Tool] // Deduplicated collection of available tools
	customTool *customTool
}

// Call executes a tool call through the appropriate MCP server and returns the result
// formatted as a chat completion message. The method handles argument parsing,
// server routing, execution, and response formatting.
//
// Process:
//  1. Parse tool call arguments from JSON
//  2. Route to appropriate MCP server based on tool name
//  3. Execute tool call with timeout protection
//  4. Format result as chat completion message for AI model consumption
//
// Parameters:
//   - tc: Tool call containing function name, arguments, and call ID
//
// Returns:
//   - *model.ChatCompletionMessage: Formatted tool result message
//   - error: Any error during argument parsing, routing, or execution
func (m *McpClient) Call(tc *model.ToolCall, opts ...Opts) (*model.ChatCompletionMessage, error) {
	if f, ok := m.customTool.Load(tc.Function.Name); ok {
		return f(tc)
	}
	co := Opt{
		timeout: 60 * time.Second,
		header:  map[string]string{},
	}
	for _, o := range opts {
		o(&co)
	}
	var arg = make(map[string]any)
	err := json.UnmarshalFromString(tc.Function.Arguments, &arg)
	if err != nil {
		return &model.ChatCompletionMessage{
			Role:       model.ChatMessageRoleTool,
			Content:    &model.ChatCompletionMessageContent{StringValue: volcengine.String(checkCallToolResult(nil, err))},
			ToolCallID: tc.ID,
		}, err
	}

CALLTOOL:
	request := mcp.CallToolRequest{
		Header: http.Header{},
		Params: mcp.CallToolParams{
			Name:      tc.Function.Name,
			Arguments: arg,
		},
	}
	for k, v := range co.header {
		request.Header.Set(k, v)
	}
	if co.reload {
		uri := m.clis[m.idx[tc.Function.Name]].uri
		m.loadMCPTools(uri)
	}
	ctx, cancel := context.WithTimeout(context.Background(), co.timeout)
	result, err := m.clis[m.idx[tc.Function.Name]].cli.CallTool(ctx, request)
	cancel()
	if err != nil {
		if !co.reload {
			if strings.Contains(err.Error(), "Invalid session ID") {
				co.reload = true
				goto CALLTOOL
			}
		}
	}
	s := checkCallToolResult(result, err)
	return &model.ChatCompletionMessage{
		Role:       model.ChatMessageRoleTool,
		Content:    &model.ChatCompletionMessageContent{StringValue: volcengine.String(s)},
		ToolCallID: tc.ID,
	}, err
}

// Tools returns all available tools from connected MCP servers.
// The tools are deduplicated and formatted for use with AI language models.
//
// Returns:
//   - []*model.Tool: Slice of all available tools across all connected MCP servers
func (m *McpClient) Tools() []*model.Tool {
	return m.tools.Slice()
}

// ToolCount returns the number of elements in the tools collection managed by the McpClient.
func (m *McpClient) ToolCount() int {
	return m.tools.Len()
}

// AddMCPTools connects to an MCP server at the specified URI and loads its available tools.
// The tools are automatically integrated into the client's unified tool collection.
// Empty URIs are ignored without error.
//
// Parameters:
//   - mcpUri: URI of the MCP server to connect to (e.g., "stdio://path/to/server")
//
// Returns:
//   - error: Any error encountered during connection or tool loading
func (m *McpClient) AddMCPTools(mcpUri string) error {
	if mcpUri == "" {
		return nil
	}
	_, err := m.loadMCPTools(mcpUri)
	return err
}

func (m *McpClient) AddCustomTool(tool *model.Tool, f func(tc *model.ToolCall) (*model.ChatCompletionMessage, error)) {
	m.tools.Store(tool)
	m.customTool.Store(tool.Function.Name, f)
}

// ReloadTools refreshes the tool list from all connected MCP servers.
// This is useful when MCP servers have been updated or when tool availability changes.
// The method clears the current tool collection and rebuilds it from all active connections.
//
// Returns:
//   - []*model.Tool: Updated list of all available tools
//   - error: Any error encountered during tool reloading (individual server failures are ignored)
func (m *McpClient) ReloadMCPTools() ([]*model.Tool, error) {
	if m.tools != nil {
		m.tools.Clear()
	} else {
		m.tools = mapfx.NewUniqueSlice[*model.Tool]()
	}
	for _, cli := range m.clis {
		mt, err := m.loadMCPTools(cli.uri)
		if err == nil {
			m.tools.StoreMany(mt...)
		}
	}
	return m.Tools(), nil
}

// loadTools establishes a connection to an MCP server and loads its available tools.
// This method handles the complete MCP connection lifecycle including:
//   - Connection establishment and initialization
//   - Protocol version negotiation
//   - Tool discovery and schema conversion
//   - Tool registration and routing setup
//
// The method converts MCP tool schemas to the format expected by AI language models
// and maintains routing information for tool call execution.
//
// Parameters:
//   - mcpUri: URI of the MCP server to connect to
//
// Returns:
//   - []*model.Tool: List of tools loaded from the server
//   - error: Any error during connection, initialization, or tool loading
func (m *McpClient) loadMCPTools(mcpUri string) ([]*model.Tool, error) {
	var err error
	clikey := crypto.GetSHA1(mcpUri)
	cli, ok := m.clis[clikey]
	if !ok {
		cli = &mclient{
			uri: mcpUri,
			cli: &client.Client{},
		}
	}
	cli.cli, err = client.NewSSEMCPClient(mcpUri)
	if err != nil {
		return nil, err
	}
	err = cli.cli.Start(context.TODO())
	if err != nil {
		return nil, err
	}
	// Initialize MCP connection with protocol negotiation
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "aiagent-cli",
		Version: "1.0.0",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = cli.cli.Initialize(ctx, initRequest)
	if err != nil {
		return nil, err
	}
	m.clis[clikey] = cli
	// Discover available tools from the MCP server
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	toolsRequest := mcp.ListToolsRequest{}
	listToolsResult, err := cli.cli.ListTools(ctx, toolsRequest)
	if err != nil {
		return nil, err
	}
	// Convert MCP tool schemas to AI model tool format
	for _, mcptool := range listToolsResult.Tools {
		var param = map[string]any{
			"type":       "object",
			"properties": mcptool.InputSchema.Properties,
		}
		vt := &model.Tool{
			Type: model.ToolTypeFunction,
			Function: &model.FunctionDefinition{
				Name:        mcptool.Name,
				Description: mcptool.Description,
				Parameters:  param,
			},
		}
		m.idx[mcptool.Name] = clikey
		m.tools.Store(vt)
	}
	return m.tools.Slice(), nil
}

func checkCallToolResult(result *mcp.CallToolResult, err error) string {
	if err != nil {
		if result == nil {
			result = &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{
					mcp.TextContent{
						Type: "text",
						Text: err.Error(),
					},
				},
			}
		}
	}
	s, _ := json.MarshalToString(result)
	return s
}
