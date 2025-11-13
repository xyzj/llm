// Package llm provides functionalities related to large language models and chat operations.
package llm

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/xyzj/llm/chat"
	mcpcli "github.com/xyzj/llm/mcp"
	"github.com/xyzj/llm/storage"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/xyzj/toolbox/crypto"
	"github.com/xyzj/toolbox/logger"
	"github.com/xyzj/toolbox/loopfunc"
	"github.com/xyzj/toolbox/mapfx"
)

const (
	chatErrorFmt = "chat [%s] error: %v"
)

// NewChatsManager creates a new ChatsManager instance with the specified configuration options.
// The ChatsManager coordinates multiple chat sessions, handles MCP (Model Context Protocol) tools,
// and manages persistent storage of chat histories.
//
// Default configuration:
//   - Base URI: "http://127.0.0.1:11434" (for local Ollama-like services)
//   - Model: "qwen3:8b"
//   - Chat lifetime: 7 days
//   - Max history: 500 messages per chat
//   - Storage: File-based storage in default cache directory, fallback to memory
//
// The manager automatically starts a background goroutine that:
//   - Saves chat histories every 5 minutes
//   - Removes expired chat sessions based on configurable lifetime
//   - Performs cleanup to prevent memory leaks
//
// Parameters:
//   - opts: Optional configuration functions to customize behavior
//
// Returns a fully initialized and ready-to-use ChatsManager instance.
func NewChatsManager(opts ...Opts) *ChatsManager {
	opt := &Opt{
		baseURI:      "http://127.0.0.1:11434",
		modelName:    "qwen3:8b",
		apiKey:       "your_api_key",
		chatLifeTime: 7 * 24 * time.Hour,
		maxHistory:   500,
		dataStorage:  storage.NewMemoryStorage(),
		roleSystem:   make([]*model.ChatCompletionMessage, 0),
		logg:         logger.NewNilLogger(),
	}
	for _, o := range opts {
		o(opt)
	}
	cm := &ChatsManager{
		chats:  mapfx.NewStructMap[string, chat.Chat](),
		mcpCli: mcpcli.New(),
		cnf:    opt,
	}
	// Start background goroutine for periodic chat history persistence and cleanup
	go loopfunc.LoopFunc(func(params ...any) {
		t := time.NewTicker(time.Minute * 5)
		defer t.Stop()
		for range t.C {
			// Periodically save chat histories and remove expired chats
			cm.chats.ForEach(func(key string, value *chat.Chat) bool {
				if time.Since(value.LastMessage()) > cm.cnf.chatLifeTime {
					cm.chats.Delete(key)
					cm.cnf.logg.Warning(fmt.Sprintf("chat [%s] expired and removed", key))
					return true
				}
				cm.cnf.dataStorage.Store(key, value.History())
				return true
			})
		}
	}, "save history", io.Discard)
	return cm
}

// ChatsManager manages multiple chat sessions and coordinates their interactions
// with AI models and MCP (Model Context Protocol) tools.
//
// Key responsibilities:
//   - Managing concurrent chat sessions with unique identifiers
//   - Coordinating tool calls through MCP clients
//   - Persisting and restoring chat histories
//   - Handling chat session lifecycle (creation, expiration, cleanup)
//   - Providing thread-safe access to chat operations
type ChatsManager struct {
	chats  *mapfx.StructMap[string, chat.Chat] // Thread-safe map of active chat sessions
	mcpCli *mcpcli.McpClient                   // MCP client for tool calling capabilities
	cnf    *Opt                                // Configuration options for the manager
}

// InitMcp initializes MCP (Model Context Protocol) clients with the provided URIs.
// Each URI represents an MCP server that provides tools for the AI model to use.
// Failed initializations are logged but don't prevent other URIs from being processed.
//
// Parameters:
//   - mcpuri: Variable number of MCP server URIs to connect to
//
// Example URIs:
//   - "stdio://path/to/mcp-server"
//   - "tcp://localhost:8080"
func (cm *ChatsManager) InitMcp(mcpuri ...string) {
	for _, u := range mcpuri {
		err := cm.mcpCli.AddTools(u)
		if err != nil {
			cm.cnf.logg.Error(fmt.Sprintf("init mcp client [%s] error: %v", u, err))
			continue
		}
	}
}

// History retrieves the conversation history for a specific chat session.
// Returns an empty slice if the chat session doesn't exist or has no history.
//
// Parameters:
//   - id: Unique identifier of the chat session
//
// Returns:
//   - []*model.ChatCompletionMessage: Slice of messages in chronological order
func (cm *ChatsManager) History(id string) []*model.ChatCompletionMessage {
	var his []*model.ChatCompletionMessage
	if ch, ok := cm.chats.Load(id); ok {
		his = ch.History()
	}
	return his
}

// Chat processes a message in the specified chat session and handles any resulting tool calls.
// This is the main method for interacting with AI models through the ChatsManager.
//
// The method performs the following operations:
//  1. Creates or retrieves the chat session (using SHA1 hash of ID for storage key)
//  2. Restores chat history from persistent storage if available
//  3. Sends the user message to the AI model with available MCP tools
//  4. Processes any tool calls made by the model through MCP clients
//  5. Sends tool results back to the model for final response generation
//  6. Streams responses through the provided write function
//
// Parameters:
//   - id: Unique identifier for the chat session (will be hashed for internal storage)
//   - message: User's message to send to the AI model
//   - w: Write function called with streaming response data chunks
//
// Error handling:
//   - Errors are logged but don't propagate to prevent cascading failures
//   - Failed tool calls are logged and skipped, allowing conversation to continue
//   - Chat session remains valid even if individual operations fail
func (cm *ChatsManager) Chat(id, message string, w func(data []byte) error) {
	keyid := crypto.GetSHA1(id)
	var ok bool
	var ch *chat.Chat
	if ch, ok = cm.chats.LoadForUpdate(keyid); !ok {
		// Create new chat session
		ch = chat.New(keyid, cm.cnf.modelName,
			chat.WithAPIKey(cm.cnf.apiKey),
			chat.WithMaxHistory(cm.cnf.maxHistory),
		)
		// Load chat history from persistent storage
		his, err := cm.cnf.dataStorage.Load(keyid)
		if err != nil {
			cm.cnf.logg.Error(fmt.Sprintf("load chat history error: %v", err))
		}
		if len(his) > 0 {
			ch.SetHistory(his)
		}
		cm.chats.Store(id, ch)
	}
	// Send message to AI model with available tools
	toolcall, err := ch.Chat(message,
		chat.WithTools(cm.mcpCli.Tools()),
		chat.WithWriteFunc(w),
		chat.WithStream(cm.mcpCli.ToolCount() == 0), // enable streaming if tools are not available
	)
	if err != nil {
		cm.cnf.logg.Error(fmt.Sprintf(chatErrorFmt, ch.ID(), err))
		return
	}
	// Process any tool calls made by the model
	if l := len(toolcall); l > 0 {
		wg := sync.WaitGroup{}
		wg.Add(l)
		msgs := make([]*model.ChatCompletionMessage, 0)
		chanMsgs := make(chan *model.ChatCompletionMessage, l)
		ctxdone, cancel := context.WithCancel(context.Background())
		loopfunc.GoFunc(func(params ...any) {
			for msg := range chanMsgs {
				if msg.Role == "shut me down" {
					cancel()
					return
				}
				msgs = append(msgs, msg)
			}
		}, "recv tool msg", nil)
		for _, v := range toolcall {
			wg.Go(func() {
				msg, err := cm.mcpCli.Call(v, mcpcli.WithTimeout(60*time.Second))
				if err != nil {
					cm.cnf.logg.Error(fmt.Sprintf("mcp call %s error: %v", v.Function.Name, err))
					return
				}
				chanMsgs <- msg
			})
		}
		wg.Wait()
		chanMsgs <- &model.ChatCompletionMessage{Role: "shut me down"}
		<-ctxdone.Done()
		// Close the channel to signal completion
		close(chanMsgs)
		// Send tool results back to model for final response
		if len(msgs) > 0 {
			_, err = ch.Chat("",
				chat.WithToolCalled(msgs),
				chat.WithStream(true),
				chat.WithWriteFunc(w),
				chat.WithRoleSystem(cm.cnf.roleSystem...),
			)
			if err != nil {
				cm.cnf.logg.Error(fmt.Sprintf(chatErrorFmt, ch.ID(), err))
				return
			}
		}
	}
}
