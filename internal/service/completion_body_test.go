package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientToolsDisableNativeTools(t *testing.T) {
	spec := ResolveModel("claude-sonnet-5-5-thinking-high")
	withTools := inlineCompletionBody(spec, Prompt{Text: "hi", DisableNativeTools: true})
	if n := toolCount(withTools["tools"]); n != 0 {
		t.Fatalf("client tools should send no native tools, got %d (%#v)", n, withTools["tools"])
	}
	plain := inlineCompletionBody(spec, Prompt{Text: "hi"})
	if n := toolCount(plain["tools"]); n == 0 {
		t.Fatalf("plain chat should keep native tools")
	}
	legacy := legacyCompletionBody("claude-sonnet-4-5", Prompt{DisableNativeTools: true})
	if n := toolCount(legacy["tools"]); n != 0 {
		t.Fatalf("legacy client tools should send no native tools, got %d", n)
	}
}

func toolCount(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return -1
	}
	var items []any
	if json.Unmarshal(raw, &items) != nil {
		return -1
	}
	return len(items)
}

func TestRefusalWithoutTextIsError(t *testing.T) {
	raw := "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\",\"stop_details\":{\"category\":\"reasoning_extraction\"}}}\n\n"
	err := parseCompletionSSE(strings.NewReader(raw), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "reasoning_extraction") {
		t.Fatalf("got %v", err)
	}
}
