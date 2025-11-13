# LLM Chat Manager

A Go package for managing multiple concurrent chat sessions with Large Language Models (LLMs), featuring Model Context Protocol (MCP) integration, persistent storage, and automatic history management.

## Features

- **Multi-Session Management**: Handle multiple concurrent chat sessions with unique identifiers
- **MCP Tool Integration**: Connect to MCP servers for AI model tool calling capabilities
- **Persistent Storage**: File-based and in-memory storage backends for conversation histories
- **Streaming Support**: Real-time streaming responses from AI models
- **Automatic Cleanup**: Background process for chat session expiration and history persistence
- **Thread-Safe Operations**: Concurrent access to chat sessions with proper synchronization
- **History Management**: Circular buffer implementation with configurable message limits

## Installation

```bash
go get 192.168.51.60/llm
```

## Quick Start

```go
package main

import (
    "192.168.51.60/llm"
    "time"
)

func main() {
    // Create a new chat manager
    manager := llm.NewChatsManager(
        llm.WithModelName("qwen3:8b"),
        llm.WithAPIKey("your-api-key"),
        llm.WithBaseURI("http://127.0.0.1:11434"),
        llm.WithChatLifeTime(7 * 24 * time.Hour),
        llm.WithMaxHistory(500),
    )

    // Optional: Initialize MCP tools
    manager.InitMcp("stdio://path/to/mcp-server")

    // Start a chat session
    manager.Chat("user-123", "Hello, how are you?", func(data []byte) error {
        // Handle streaming response
        println(string(data))
        return nil
    })

    // Retrieve chat history
    history := manager.History("user-123")
}
```

## Architecture

### Core Components

#### ChatsManager
Central coordinator for managing multiple chat sessions, MCP tool integration, and storage.

**Key Features:**
- Manages concurrent chat sessions with SHA1-hashed IDs
- Coordinates tool calls through MCP clients
- Periodic history persistence (every 5 minutes)
- Automatic expiration of inactive sessions

#### Chat
Individual chat session with an AI model, managing conversation history and request handling.

**Capabilities:**
- Streaming and non-streaming response modes
- Tool calling support
- Conversation history management
- Thread-safe operations with mutex locking

#### History
Circular buffer implementation for efficient message storage with automatic overflow handling.

**Benefits:**
- Fixed memory footprint
- Automatic oldest message removal
- JSON serialization support
- Thread-safe operations

#### Storage
Abstract interface with multiple backend implementations.

**Available Backends:**
- **FileStorage**: BoltDB-based persistent storage
- **MemoryStorage**: Fast in-memory storage (non-persistent)

#### MCP Client
Model Context Protocol client for integrating external tools with AI models.

**Features:**
- Multiple MCP server support
- Automatic tool discovery and schema conversion
- Tool call routing and execution
- Connection pooling and lifecycle management

## Configuration Options

### ChatsManager Options

```go
// Set custom storage backend
llm.WithStorage(customStorage)

// Configure chat lifetime (auto-cleanup after inactivity)
llm.WithChatLifeTime(24 * time.Hour)

// Set maximum history per chat
llm.WithMaxHistory(1000)

// Configure custom logger
llm.WithLogger(myLogger)

// Set LLM service endpoint
llm.WithBaseURI("http://localhost:11434")

// Configure AI model
llm.WithModelName("qwen3:8b")

// Set API authentication
llm.WithAPIKey("your-api-key")

// Configure system role messages
llm.WithRoleSystem(&model.ChatCompletionMessage{
    Role: model.ChatMessageRoleSystem,
    Content: &model.ChatCompletionMessageContent{
        StringValue: volcengine.String("You are a helpful assistant."),
    },
})
```

### Chat Options

```go
// Enable/disable streaming
chat.WithStream(true)

// Set custom model for specific request
chat.WithModel("different-model")

// Provide available tools
chat.WithTools(toolsList)

// Add system role messages
chat.WithRoleSystem(systemMessages...)

// Include tool call results
chat.WithToolCalled(toolResults)

// Custom write function for streaming
chat.WithWriteFunc(func(data []byte) error {
    return processData(data)
})
```

## MCP Integration

The package supports the Model Context Protocol for tool calling:

```go
manager := llm.NewChatsManager()

// Add MCP servers
manager.InitMcp(
    "stdio://path/to/filesystem-server",
    "stdio://path/to/database-server",
)

// Tools are automatically discovered and made available to the AI model
// Tool calls are handled transparently during chat operations
```

## Storage Backends

### File Storage (BoltDB)

```go
fileStorage, err := storage.NewFileStorage("/path/to/chats.db")
if err != nil {
    log.Fatal(err)
}

manager := llm.NewChatsManager(
    llm.WithStorage(fileStorage),
)
```

### Memory Storage

```go
memStorage := storage.NewMemoryStorage()

manager := llm.NewChatsManager(
    llm.WithStorage(memStorage),
)
```

### Custom Storage

Implement the `Storage` interface:

```go
type Storage interface {
    Store(chatid string, history []*model.ChatCompletionMessage) error
    Load(chatid string) []*model.ChatCompletionMessage
    Clear()
}
```

## Package Structure

```
llm/
├── chats_manager.go    # Main chat manager implementation
├── opt.go              # Configuration options
├── chat/
│   └── chat.go         # Individual chat session logic
├── history/
│   └── history.go      # Circular buffer history management
├── mcp/
│   └── mcpcli.go       # MCP client implementation
└── storage/
    ├── interface.go    # Storage interface definition
    ├── file.go         # BoltDB file storage
    └── memory.go       # In-memory storage
```

## Key Concepts

### Chat Lifecycle

1. **Creation**: Chat created on first message with unique ID
2. **Active**: Chat processes messages and maintains history
3. **Persistence**: History saved every 5 minutes automatically
4. **Expiration**: Inactive chats removed after configured lifetime
5. **Restoration**: Chat history restored from storage when resumed

### Tool Calling Flow

1. User sends message to chat
2. AI model receives message with available tools
3. Model decides to call one or more tools
4. MCP client routes tool calls to appropriate servers
5. Tool results returned to AI model
6. Model generates final response incorporating tool results
7. Response streamed back to user

### History Management

- Uses circular buffer with fixed capacity
- Oldest messages automatically removed when full
- JSON serialization for persistence
- Thread-safe concurrent access
- Configurable maximum context size

## Dependencies

- `github.com/volcengine/volcengine-go-sdk` - VolcEngine ARK runtime for AI models
- `github.com/mark3labs/mcp-go` - Model Context Protocol implementation
- `github.com/xyzj/toolbox` - Utility functions and data structures
- BoltDB (via toolbox) - Embedded key-value database

## Performance Considerations

- **Memory Usage**: Controlled by `maxHistory` setting per chat
- **Storage I/O**: History persisted every 5 minutes (configurable via code)
- **Concurrent Sessions**: Thread-safe with minimal contention
- **Tool Calls**: Executed in parallel with timeout protection (60s default)
- **Cleanup**: Background goroutine handles expired session removal

## Best Practices

1. **Chat IDs**: Use meaningful, unique identifiers (user IDs, session tokens)
2. **History Limits**: Balance context window with memory usage
3. **Storage Selection**: Use file storage for production, memory for testing
4. **Error Handling**: Check returned errors from Chat operations
5. **MCP Timeouts**: Configure appropriate timeouts based on tool complexity
6. **Logging**: Enable logging in production for debugging and monitoring

## License

This package is part of the internal github.com/xyzj/microframework project.

## Support

For issues, questions, or contributions, please contact the internal development team.
