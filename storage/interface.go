// Package storage provides abstraction for different storage backends used to persist
// chat conversation histories. It defines the Storage interface and provides
// implementations for file-based and in-memory storage systems.
package storage

import (
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// Storage defines the interface for persisting and retrieving chat conversation histories.
// Implementations must provide thread-safe operations and handle serialization of
// chat completion messages.
//
// The interface supports:
//   - Per-chat storage with unique identifiers
//   - Efficient retrieval of conversation histories
//   - Bulk clearing of all stored data
//   - Error handling for storage operations
type Storage interface {
	// Store persists a chat conversation history for the specified chat ID.
	// The history slice contains messages in chronological order.
	//
	// Parameters:
	//   - chatid: Unique identifier for the chat session
	//   - history: Slice of messages to store
	//
	// Returns:
	//   - error: Any error encountered during storage operation
	Store(chatid string, history []*model.ChatCompletionMessage) error

	// Load retrieves the conversation history for the specified chat ID.
	// Returns an empty slice if no history exists for the given ID.
	//
	// Parameters:
	//   - chatid: Unique identifier for the chat session
	//
	// Returns:
	//   - []*model.ChatCompletionMessage: Retrieved messages in chronological order
	Load(chatid string) ([]*model.ChatCompletionMessage, error)

	// Clear removes all stored conversation histories from the storage backend.
	// This operation is irreversible and should be used with caution.
	Clear() error
}
