package service

import "encoding/json"

// UserInfo 是账号信息。
type UserInfo struct {
	Email   string
	OrgUUID string
}

type ClaudeAccount struct {
	EmailAddress string `json:"email_address"`
	Memberships  []struct {
		Organization struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
	} `json:"memberships"`
}

// SseEvent 是 completion SSE 事件。
type SseEvent struct {
	Type         string `json:"type"`
	ContentBlock struct {
		Type string `json:"type"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
		StopDetails struct {
			Category    string `json:"category"`
			Explanation string `json:"explanation"`
		} `json:"stop_details"`
	} `json:"delta"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Prompt 是协议适配层交给上游实现的输入。
type Prompt struct {
	Text        string
	Images      []string
	RawRequest  json.RawMessage
	ForceInline bool
	// LongContext provides an explicit attachment layout when Text exceeds the
	// configured inline limit. It lets protocol adapters keep their control
	// instructions inline while moving large catalogs and conversation history
	// into distinct attachments.
	LongContext *LongContextPlan
	// DisableNativeTools keeps Claude.ai web tools out of the completion when
	// the caller supplied its own tools. Native tool_use is not translated into
	// the client protocol and otherwise comes back as an empty 200.
	DisableNativeTools bool
	MaxTokens          int
	Temperature        *float64
	TopP               *float64
	Stop               []string
}

// TextAttachment is a text-only Claude.ai attachment assembled in memory.
// It is not written to the local filesystem.
type TextAttachment struct {
	FileName string
	FileType string
	Content  string
}

// LongContextPlan describes the replacement inline prompt and attachments to
// use when a prompt is too large for direct submission.
type LongContextPlan struct {
	InlineText        string
	CompactInlineText string // Drops optional hints when the configured limit is smaller.
	Attachments       []TextAttachment
}

type CompletionResult struct {
	Account    string
	StatusCode int
}

type CompletionError struct {
	StatusCode int
	Err        error
}

func (e *CompletionError) Error() string { return e.Err.Error() }
func (e *CompletionError) Unwrap() error { return e.Err }
