package storage

import (
	"sync"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// MemoryStorage provides an in-memory implementation of the Storage interface.
// It stores chat conversation histories in memory using a thread-safe map structure.
//
// Characteristics:
//   - Fast read/write operations with O(1) access time
//   - Data is lost when the application terminates
//   - Thread-safe operations using read-write mutex
//   - Suitable for temporary storage or testing scenarios
//   - Memory usage grows with the number and size of stored conversations
type MemoryStorage struct {
	locker sync.RWMutex                              // Read-write mutex for thread safety
	data   map[string][]*model.ChatCompletionMessage // In-memory storage map
}

// NewMemoryStorage creates a new in-memory storage instance.
// The storage is ready for immediate use and provides thread-safe operations.
//
// Returns:
//   - Storage: A new MemoryStorage instance implementing the Storage interface
func NewMemoryStorage() Storage {
	return &MemoryStorage{
		data:   make(map[string][]*model.ChatCompletionMessage),
		locker: sync.RWMutex{},
	}
}

// Clear removes all stored conversation histories from memory.
// This operation acquires a write lock and is thread-safe.
// The operation is immediate and irreversible.
func (s *MemoryStorage) Clear() error {
	s.locker.Lock()
	defer s.locker.Unlock()
	s.data = make(map[string][]*model.ChatCompletionMessage)
	return nil
}

// Store saves a conversation history for the specified chat ID.
// The operation replaces any existing history for the given chat ID.
// This method is thread-safe and acquires a write lock during operation.
//
// Parameters:
//   - chatid: Unique identifier for the chat session
//   - msg: Slice of chat completion messages to store
//
// Returns:
//   - error: Always returns nil for in-memory storage (kept for interface compliance)
func (s *MemoryStorage) Store(chatid string, msg []*model.ChatCompletionMessage) error {
	s.locker.Lock()
	defer s.locker.Unlock()
	if _, ok := s.data[chatid]; !ok {
		s.data[chatid] = make([]*model.ChatCompletionMessage, 0)
	}
	s.data[chatid] = msg
	return nil
}

// Load retrieves the conversation history for the specified chat ID.
// Returns an empty slice if no history exists for the given chat ID.
// This method is thread-safe and acquires a read lock during operation.
//
// Parameters:
//   - chatid: Unique identifier for the chat session
//
// Returns:
//   - []*model.ChatCompletionMessage: Retrieved conversation history or empty slice
func (s *MemoryStorage) Load(chatid string) ([]*model.ChatCompletionMessage, error) {
	s.locker.RLock()
	defer s.locker.RUnlock()
	if _, ok := s.data[chatid]; !ok {
		return make([]*model.ChatCompletionMessage, 0), nil
	}
	return s.data[chatid], nil
}
