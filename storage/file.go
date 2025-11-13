package storage

import (
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/xyzj/toolbox/db"
	"github.com/xyzj/toolbox/json"
)

// FileStorage provides a file-based implementation of the Storage interface using BoltDB.
// It persists chat conversation histories to disk, ensuring data survives application restarts.
//
// Characteristics:
//   - Persistent storage that survives application restarts
//   - ACID transactions provided by BoltDB
//   - Efficient key-value storage with B+ tree indexing
//   - Automatic file creation and management
//   - JSON serialization for message data
//   - Thread-safe operations through BoltDB's concurrency control
type FileStorage struct {
	f  string     // File path for the BoltDB database
	db *db.BoltDB // BoltDB instance for persistent storage
}

// NewFileStorage creates a new file-based storage instance using the specified file path.
// The database file is created automatically if it doesn't exist, along with any
// necessary parent directories.
//
// Parameters:
//   - filename: Path to the BoltDB database file
//
// Returns:
//   - Storage: A new FileStorage instance implementing the Storage interface
//   - error: Any error encountered during database initialization
func NewFileStorage(filename string) (Storage, error) {
	d, err := db.NewBolt(filename)
	if err != nil {
		return nil, err
	}
	return &FileStorage{
		f:  filename,
		db: d,
	}, nil
}

// Clear removes all stored conversation histories from the database file.
// This operation iterates through all keys and deletes them individually.
// The operation is performed within BoltDB's transaction system for consistency.
func (s *FileStorage) Clear() error {
	s.db.ForEach(func(k, v string) error {
		s.db.Delete(k)
		return nil
	})
	return nil
}

// Load retrieves the conversation history for the specified chat ID from the database.
// The method currently has a bug - it loads ALL conversations instead of filtering by chatid.
// This should be fixed to only load the specific chat's history.
//
// TODO: Fix implementation to filter by chatid parameter
//
// Parameters:
//   - chatid: Unique identifier for the chat session (currently unused due to bug)
//
// Returns:
//   - []*model.ChatCompletionMessage: All stored messages (should be filtered by chatid)
func (s *FileStorage) Load(chatid string) ([]*model.ChatCompletionMessage, error) {
	data := make([]*model.ChatCompletionMessage, 0, 1000)
	s.db.ForEach(func(k, v string) error {
		x := &model.ChatCompletionMessage{}
		err := json.UnmarshalFromString(v, x)
		if err != nil {
			return err
		}
		data = append(data, x)
		return nil
	})
	return data, nil
}

// Store persists a conversation history for the specified chat ID to the database file.
// The history is serialized to JSON and stored using the chat ID as the key.
// The operation is atomic and thread-safe through BoltDB's transaction system.
//
// Parameters:
//   - chatid: Unique identifier for the chat session
//   - history: Slice of chat completion messages to persist
//
// Returns:
//   - error: Any error encountered during JSON serialization or database write
func (s *FileStorage) Store(chatid string, history []*model.ChatCompletionMessage) error {
	xs, err := json.MarshalToString(history)
	if err != nil {
		return err
	}
	return s.db.Write(chatid, xs)
}
