// Package llm provides core configuration options for large language model operations.
// This file contains the configuration structures and option functions used throughout
// the LLM package to customize chat managers and their behavior.
package llm

import (
	"time"

	"github.com/xyzj/llm/storage"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
	"github.com/xyzj/toolbox/logger"
)

type (
	// Opt contains configuration options for the ChatsManager.
	// These options control various aspects of chat behavior including
	// storage, model selection, API authentication, and chat lifecycle management.
	Opt struct {
		dataStorage  storage.Storage                // Storage backend for persisting chat history
		chatLifeTime time.Duration                  // Maximum idle time before a chat session expires
		logg         logger.Logger                  // Logger instance for debugging and monitoring
		roleSystem   []*model.ChatCompletionMessage // System role message template
		baseURI      string                         // Base URI for the LLM service endpoint
		modelName    string                         // Name of the AI model to use for chat completions
		apiKey       string                         // API key for authenticating with the LLM service
		maxHistory   int                            // Maximum number of messages to retain in chat history
	}
	// Opts is a function type for configuring ChatsManager options.
	Opts func(opt *Opt)
)

// WithRoleSystem configures the Opt to use the given system role messages.
// The provided messages replace any existing roleSystem messages on the Opt.
// Passing zero messages clears the roleSystem (sets it to nil). The returned
// Opts function applies this configuration to the target *Opt.
func WithRoleSystem(msg ...*model.ChatCompletionMessage) Opts {
	return func(opt *Opt) {
		opt.roleSystem = msg
	}
}

// WithMaxHistory sets the maximum number of messages to keep in each chat's history.
// When this limit is exceeded, older messages are automatically removed to prevent
// memory issues and maintain reasonable context windows for the AI model.
func WithMaxHistory(n int) Opts {
	return func(opt *Opt) {
		opt.maxHistory = n
	}
}

// WithStorage sets the storage backend for persisting chat histories.
// This allows chat conversations to be restored after application restarts.
// Supported storage types include file-based and in-memory storage.
func WithStorage(s storage.Storage) Opts {
	return func(opt *Opt) {
		opt.dataStorage = s
	}
}

// WithChatLifeTime sets the maximum idle time before a chat session expires.
// Inactive chat sessions older than this duration will be automatically
// removed from memory to prevent resource leaks.
func WithChatLifeTime(t time.Duration) Opts {
	return func(opt *Opt) {
		opt.chatLifeTime = t
	}
}

// WithLogger sets a custom logger instance for the ChatsManager.
// This logger will be used for debugging, error reporting, and monitoring
// chat operations throughout the system.
func WithLogger(l logger.Logger) Opts {
	return func(opt *Opt) {
		opt.logg = l
	}
}

// WithBaseURI sets the base URI for the LLM service endpoint.
// This is typically used when connecting to self-hosted or custom
// language model services instead of cloud-based APIs.
func WithBaseURI(u string) Opts {
	return func(opt *Opt) {
		opt.baseURI = u
	}
}

// WithModelName sets the default AI model name to use for chat completions.
// This can be overridden on a per-chat basis if needed.
// Examples: "qwen3:8b", "ep-20241224xxx-xxx"
func WithModelName(n string) Opts {
	return func(opt *Opt) {
		opt.modelName = n
	}
}

// WithAPIKey sets the API key for authenticating with the LLM service.
// This key is required for most cloud-based language model providers.
func WithAPIKey(k string) Opts {
	return func(opt *Opt) {
		opt.apiKey = k
	}
}
