// Package chat provides chat functionality with AI models through the VolcEngine ARK runtime.
// It supports both streaming and non-streaming chat completions, tool calling, and chat history management.
package chat

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/xyzj/llm/history"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/xyzj/toolbox/json"
)

type (
	// Opt contains options for individual chat requests.
	Opt struct {
		toolcalled []*model.ChatCompletionMessage // Previously called tool messages to include in the chat
		roleSystem []*model.ChatCompletionMessage // System role messages to include in the chat
		tools      []*model.Tool                  // Available tools for the chat completion
		writeFunc  func(data []byte) error        // Function to write streaming response data
		model      string                         // Model name to use for this specific request
		stream     bool                           // Whether to use streaming response
	}
	// Opts is a function type for configuring chat request options.
	Opts func(opt *Opt)

	// ChatOpt contains configuration options for creating a new Chat instance.
	ChatOpt struct {
		maxhistory int    // Maximum number of messages to keep in history
		apikey     string // API key for VolcEngine ARK runtime
	}
	// ChatOpts is a function type for configuring Chat creation options.
	ChatOpts func(opt *ChatOpt)
)

// WithMaxHistory sets the maximum number of messages to keep in chat history.
// When the limit is exceeded, older messages are automatically removed.
func WithMaxHistory(n int) ChatOpts {
	return func(opt *ChatOpt) {
		opt.maxhistory = n
	}
}

// WithAPIKey sets the API key for VolcEngine ARK runtime authentication.
func WithAPIKey(k string) ChatOpts {
	return func(opt *ChatOpt) {
		opt.apikey = k
	}
}

// WithRoleSystem sets the system role messages for the chat completion.
// System messages are used to set the behavior and context of the AI assistant.
// Multiple system messages can be provided and will be prepended to the conversation.
func WithRoleSystem(msg ...*model.ChatCompletionMessage) Opts {
	return func(opt *Opt) {
		opt.roleSystem = msg
	}
}

// WithToolCalled includes previously called tool messages in the chat request.
// This is used when continuing a conversation that involved tool calls.
func WithToolCalled(toolcalled []*model.ChatCompletionMessage) Opts {
	return func(opt *Opt) {
		opt.toolcalled = toolcalled
	}
}

// WithWriteFunc sets a custom function to handle streaming response data.
// The function is called with each chunk of data received during streaming.
func WithWriteFunc(f func(data []byte) error) Opts {
	return func(opt *Opt) {
		opt.writeFunc = f
	}
}

// WithModel overrides the default model for this specific chat request.
func WithModel(m string) Opts {
	return func(opt *Opt) {
		opt.model = m
	}
}

// WithStream enables or disables streaming mode for the chat request.
// When enabled, responses are streamed in real-time chunks.
func WithStream(stream bool) Opts {
	return func(opt *Opt) {
		opt.stream = stream
	}
}

// WithTools provides available tools that the AI model can call during the conversation.
func WithTools(tools []*model.Tool) Opts {
	return func(opt *Opt) {
		opt.tools = tools
	}
}

// New creates a new Chat instance with the specified ID and model name.
// The Chat instance manages conversation history and provides methods for
// interacting with AI models through the VolcEngine ARK runtime.
//
// Parameters:
//   - id: Unique identifier for this chat session
//   - modelName: Name of the AI model to use (e.g., "ep-20241224xxx-xxx")
//   - opts: Optional configuration functions to customize the chat behavior
//
// Returns a configured Chat instance ready for use.
func New(id, modelName string, opts ...ChatOpts) *Chat {
	co := &ChatOpt{
		maxhistory: 500,
		apikey:     "your_api_key",
	}
	for _, o := range opts {
		o(co)
	}
	return &Chat{
		locker:  sync.Mutex{},
		id:      id,
		apikey:  co.apikey,
		history: *history.New(co.maxhistory),
		model:   modelName,
		cli:     arkruntime.NewClientWithApiKey(co.apikey),
	}
}

// Chat represents a chat session with an AI model.
// It maintains conversation history, handles both streaming and non-streaming responses,
// and supports tool calling functionality.
type Chat struct {
	locker      sync.Mutex         // Mutex for thread-safe operations
	history     history.History    // Conversation history manager
	cli         *arkruntime.Client // VolcEngine ARK runtime client
	lastMessage time.Time          // Timestamp of the last message sent or received
	apikey      string             // API key for authentication
	model       string             // Default model name for this chat session
	id          string             // Unique identifier for this chat session
}

// ID returns the unique identifier of this chat session.
func (c *Chat) ID() string {
	return c.id
}

// LastMessage returns the timestamp of the last message sent or received in this chat.
// This can be used to determine chat activity and implement timeout logic.
func (c *Chat) LastMessage() time.Time {
	return c.lastMessage
}

// History returns a slice of all messages in the current conversation history.
// The returned slice contains both user and assistant messages in chronological order.
func (c *Chat) History() []*model.ChatCompletionMessage {
	return c.history.Slice()
}

// SetHistory replaces the current conversation history with the provided messages.
// This is useful for restoring a conversation from persistent storage or
// initializing a chat with predefined context.
func (c *Chat) SetHistory(h []*model.ChatCompletionMessage) {
	c.history.StoreMany(h...)
}

// Chat sends a message to the AI model and returns any tool calls made by the model.
// This is the main method for interacting with the AI model in a conversational manner.
//
// Parameters:
//   - message: The user's message to send to the AI model. Can be empty if only processing tool calls.
//   - opts: Optional configuration functions to customize this specific request.
//
// Returns:
//   - map[string]*model.ToolCall: A map of tool call IDs to their corresponding tool calls made by the model.
//   - error: Any error that occurred during the chat completion request.
//
// The method automatically:
//   - Updates the lastMessage timestamp
//   - Adds the user message to conversation history
//   - Handles both streaming and non-streaming responses based on configuration
//   - Processes tool calls if any are made by the model
//   - Manages conversation history including tool call results
func (c *Chat) Chat(message string, opts ...Opts) (map[string]*model.ToolCall, error) {
	defer func() {
		c.lastMessage = time.Now()
		c.locker.Unlock()
	}()
	c.locker.Lock()
	co := &Opt{
		stream:     false,
		writeFunc:  func(data []byte) error { return nil },
		model:      c.model,
		tools:      make([]*model.Tool, 0),
		roleSystem: make([]*model.ChatCompletionMessage, 0),
	}
	for _, o := range opts {
		o(co)
	}
	if len(message) > 0 {
		c.history.Store(&model.ChatCompletionMessage{
			Role: model.ChatMessageRoleUser,
			Content: &model.ChatCompletionMessageContent{
				StringValue: volcengine.String(message),
			},
		})
	}
	msgs := make([]*model.ChatCompletionMessage, 0, c.history.Len()+len(co.toolcalled)+1)
	req := model.CreateChatCompletionRequest{
		Model: co.model,
		// Messages: c.history.Slice(),
		Stream: &co.stream,
	}
	if len(co.roleSystem) > 0 {
		msgs = append(msgs, co.roleSystem...)
	}
	if len(co.toolcalled) > 0 {
		c.history.StoreMany(co.toolcalled...)
	} else {
		if len(co.tools) > 0 {
			req.Tools = co.tools
		}
	}
	msgs = append(msgs, c.history.Slice()...)
	req.Messages = msgs
	if co.stream {
		return c.doStream(req, co.writeFunc)
	}
	return c.do(req, co.writeFunc)
}

// doStream handles streaming chat completions from the LLM client. It sends each chunk of assistant response content
// to the provided writer callback `w` as it is received. The function also accumulates tool call information from the
// stream, mapping tool call IDs to their corresponding ToolCall objects, and handles the progressive filling of tool
// call arguments. Upon completion, it stores the assistant's full response message in the chat history if any content
// was received. Returns a map of tool call IDs to ToolCall objects, or an error if the streaming process fails.
//
// Parameters:
//   - req: The CreateChatCompletionRequest containing the chat prompt and options.
//   - w: A callback function that processes each chunk of assistant response content.
//
// Returns:
//   - map[string]*model.ToolCall: A map of tool call IDs to ToolCall objects extracted from the stream.
//   - error: An error if the streaming or processing fails, or nil on success.
func (c *Chat) doStream(req model.CreateChatCompletionRequest, w func(data []byte) error) (map[string]*model.ToolCall, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	stream, err := c.cli.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	toolCallMap := make(map[string]*model.ToolCall)
	var lastCallID string
	var message = strings.Builder{}
	for !stream.IsFinished {
		recv, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(recv.Choices) > 0 {
			if recv.Choices[0].Delta.Role == model.ChatMessageRoleAssistant && recv.Choices[0].Delta.Content != "" {
				err = w([]byte(recv.Choices[0].Delta.Content))
				if err != nil {
					return nil, err
				}
				message.WriteString(recv.Choices[0].Delta.Content)
			}
			if len(recv.Choices[0].Delta.ToolCalls) > 0 {
				for _, tc := range recv.Choices[0].Delta.ToolCalls {
					if tc.ID != "" {
						if toolCallMap[tc.ID] == nil {
							toolCallMap[tc.ID] = &model.ToolCall{
								ID:       tc.ID,
								Function: model.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
								Type:     tc.Type,
							}
						}
						lastCallID = tc.ID
					} else { // tc.ID == "" indicates we're filling arguments for the previous tool call ID
						toolCallMap[lastCallID].Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}
	}
	if message.Len() > 0 {
		c.history.Store(&model.ChatCompletionMessage{
			Role: model.ChatMessageRoleAssistant,
			Content: &model.ChatCompletionMessageContent{
				StringValue: volcengine.String(message.String()),
			},
		})
	}
	return toolCallMap, nil
}

// do sends a chat completion request using the provided model.CreateChatCompletionRequest,
// processes the response, and invokes the callback function 'w' with the assistant's message content.
// It returns a map of tool call IDs to ToolCall objects if any tool calls are present in the response.
// The function also stores the assistant's message in the chat history.
// If an error occurs during the request or callback execution, it returns the error.
func (c *Chat) do(req model.CreateChatCompletionRequest, w func(data []byte) error) (map[string]*model.ToolCall, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	resp, err := c.cli.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	toolCallMap := make(map[string]*model.ToolCall)
	if len(resp.Choices) > 0 {
		if resp.Choices[0].Message.Role == model.ChatMessageRoleAssistant && resp.Choices[0].Message.Content.StringValue != nil {
			err = w(json.Bytes(*resp.Choices[0].Message.Content.StringValue))
			if err != nil {
				return nil, err
			}
			c.history.Store(&model.ChatCompletionMessage{
				Role: resp.Choices[0].Message.Role,
				Content: &model.ChatCompletionMessageContent{
					StringValue: volcengine.String(*resp.Choices[0].Message.Content.StringValue),
				},
			})
		}
		if len(resp.Choices[0].Message.ToolCalls) > 0 {
			for _, tc := range resp.Choices[0].Message.ToolCalls {
				if tc.ID != "" {
					if toolCallMap[tc.ID] == nil {
						toolCallMap[tc.ID] = &model.ToolCall{
							ID:       tc.ID,
							Function: model.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments},
							Type:     tc.Type,
						}
					}
				}
			}
		}
	}
	return toolCallMap, nil
}
