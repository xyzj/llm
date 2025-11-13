package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

const chatHistoryPrefix = "llm_chats_histories_"

type (
	Opt struct {
		historySuffix string // Suffix for chat history keys in storage
	}
	// Opts is a function type for configuring ChatsManager options.
	Opts func(opt *Opt)
)

// WithHistorySuffix returns an Opts function that sets the history suffix for the Redis storage.
// The suffix parameter specifies a custom suffix to be appended to history-related keys.
// This is useful for organizing or namespacing history data in Redis.
func WithHistorySuffix(suffix string) Opts {
	return func(opt *Opt) {
		opt.historySuffix = suffix
	}
}

type RedisStorage struct {
	cnf        *Opt
	db         *redis.Client // Redis client for persistent storage
	historyKey string
}

// Clear removes the chat history from Redis storage by deleting the key
// associated with this storage instance. It uses a 3-second timeout context
// for the operation. Returns an error if the deletion fails.
func (s *RedisStorage) Clear() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	return s.db.Del(ctx, chatHistoryPrefix+s.cnf.historySuffix).Err()
}

// Load retrieves the chat history for a given chat ID from Redis storage.
// It fetches the serialized message history from a Redis hash and deserializes
// it into a slice of ChatCompletionMessage pointers.
//
// Parameters:
//   - chatid: The unique identifier for the chat session
//
// Returns:
//   - []*model.ChatCompletionMessage: A slice of chat completion messages if successful
//   - error: An error if the Redis operation fails, the chat ID doesn't exist,
//     or JSON deserialization fails
//
// The function uses a 3-second timeout context for the Redis operation.
func (s *RedisStorage) Load(chatid string) ([]*model.ChatCompletionMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	val, err := s.db.HGet(ctx, s.historyKey, chatid).Result()
	if err != nil {
		return nil, err
	}
	var messages []*model.ChatCompletionMessage
	err = json.Unmarshal([]byte(val), &messages)
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// Store saves chat completion messages to Redis storage by marshaling the messages
// to JSON and storing them in a hash set with the given chat ID as the key.
// It returns an error if JSON marshaling fails or if the Redis operation fails.
// The operation has a timeout of 3 seconds.
func (s *RedisStorage) Store(chatid string, messages []*model.ChatCompletionMessage) error {
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	return s.db.HSet(ctx, s.historyKey, chatid, data).Err()
}

// NewRedisStorage creates a new Redis-based storage implementation for managing chat data.
// It accepts a Redis client and optional configuration options to customize the storage behavior.
//
// Parameters:
//   - cli: A *redis.Client instance used for Redis operations
//   - opts: Variadic Opts functions to configure the storage (e.g., history suffix)
//
// The function initializes a RedisStorage with:
//   - A default history suffix of "default" if not specified
//   - A history key constructed from chatHistoryPrefix and the configured suffix
//
// Returns:
//   - Storage: A Storage interface implementation backed by Redis
//
// Example:
//
//	storage := NewRedisStorage(redisClient, WithHistorySuffix("session123"))
func NewRedisStorage(cli *redis.Client, opts ...Opts) Storage {
	opt := &Opt{
		historySuffix: "default",
	}
	for _, o := range opts {
		o(opt)
	}
	return &RedisStorage{
		db:         cli,
		cnf:        opt,
		historyKey: chatHistoryPrefix + opt.historySuffix,
	}
}
