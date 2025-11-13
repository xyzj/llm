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
	"fmt"
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
	}
	Opts func(opt *Opt)
)

func WithTimeout(t time.Duration) Opts {
	return func(opt *Opt) {
		opt.timeout = t
	}
}

// New creates a new McpClient instance for managing MCP server connections and tools.
// The client can connect to multiple MCP servers and aggregate their tools into
// a unified interface for AI models to use.
//
// Returns a new McpClient ready to connect to MCP servers and manage tools.
func New() *McpClient {
	return &McpClient{
		clis:  make(map[string]*mclient),
		tools: mapfx.NewUniqueSlice[*model.Tool](),
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
	clis  map[string]*mclient             // Map of MCP server connections (keyed by SHA1 hash of URI)
	idx   map[string]string               // Tool name to server key mapping for routing
	tools *mapfx.UniqueSlice[*model.Tool] // Deduplicated collection of available tools
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
	co := Opt{
		timeout: 60 * time.Second,
	}
	for _, o := range opts {
		o(&co)
	}
	var arg = make(map[string]any)
	err := json.UnmarshalFromString(tc.Function.Arguments, &arg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), co.timeout)
	defer cancel()
	request := mcp.CallToolRequest{}
	request.Params.Name = tc.Function.Name
	request.Params.Arguments = arg
	result, err := m.clis[m.idx[tc.Function.Name]].cli.CallTool(ctx, request)
	if err != nil {
		return nil, err
	}
	return &model.ChatCompletionMessage{
		Role:       model.ChatMessageRoleTool,
		Content:    &model.ChatCompletionMessageContent{StringValue: volcengine.String(fmt.Sprint(result.Content))},
		ToolCallID: tc.ID,
	}, nil
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

// AddTools connects to an MCP server at the specified URI and loads its available tools.
// The tools are automatically integrated into the client's unified tool collection.
// Empty URIs are ignored without error.
//
// Parameters:
//   - mcpUri: URI of the MCP server to connect to (e.g., "stdio://path/to/server")
//
// Returns:
//   - error: Any error encountered during connection or tool loading
func (m *McpClient) AddTools(mcpUri string) error {
	if mcpUri == "" {
		return nil
	}
	_, err := m.loadTools(mcpUri)
	return err
}

// ReloadTools refreshes the tool list from all connected MCP servers.
// This is useful when MCP servers have been updated or when tool availability changes.
// The method clears the current tool collection and rebuilds it from all active connections.
//
// Returns:
//   - []*model.Tool: Updated list of all available tools
//   - error: Any error encountered during tool reloading (individual server failures are ignored)
func (m *McpClient) ReloadTools() ([]*model.Tool, error) {
	if m.tools != nil {
		m.tools.Clear()
	} else {
		m.tools = mapfx.NewUniqueSlice[*model.Tool]()
	}
	for _, cli := range m.clis {
		mt, err := m.loadTools(cli.uri)
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
func (m *McpClient) loadTools(mcpUri string) ([]*model.Tool, error) {
	var err error
	clikey := crypto.GetSHA1(mcpUri)
	cli, ok := m.clis[clikey]
	if !ok {
		cli.cli, err = client.NewSSEMCPClient(cli.uri)
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
	}
	// Discover available tools from the MCP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
