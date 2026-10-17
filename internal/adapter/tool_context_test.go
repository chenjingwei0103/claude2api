package adapter

import (
	"strings"
	"testing"
)

func TestBuildToolPromptSeparatesLargeToolCatalogFromConversation(t *testing.T) {
	tools := []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":        "read_workspace_file",
			"description": strings.Repeat("d", 20_000),
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		},
	}}
	prompt := buildToolPrompt([]Message{{Role: "user", Content: "读取 README"}}, tools, true)

	if prompt.LongContext == nil {
		t.Fatal("工具提示应提供 LongContext 附件计划")
	}
	if !strings.Contains(prompt.LongContext.InlineText, "tool-catalog.txt") {
		t.Fatalf("内联控制提示应声明工具目录附件，得到 %q", prompt.LongContext.InlineText)
	}
	if strings.Contains(prompt.LongContext.InlineText, strings.Repeat("d", 100)) {
		t.Fatal("内联控制提示不应包含大型工具说明")
	}

	contents := map[string]string{}
	for _, attachment := range prompt.LongContext.Attachments {
		contents[attachment.FileName] = attachment.Content
	}
	if !strings.Contains(contents["tool-catalog.txt"], "read_workspace_file") {
		t.Fatalf("工具目录缺少工具定义: %#v", contents)
	}
	if !strings.Contains(contents["conversation.txt"], "读取 README") {
		t.Fatalf("对话附件缺少用户任务: %#v", contents)
	}
}

func TestCompactToolNameIndexStaysWithinBudget(t *testing.T) {
	tools := []map[string]any{
		{"name": "abcdef"},
		{"name": "g"},
		{"name": "h"},
	}
	index := compactToolNameIndex(tools, 10)
	if len(index) > 10 {
		t.Fatalf("工具名索引超过字节预算: %q (%d bytes)", index, len(index))
	}
}
