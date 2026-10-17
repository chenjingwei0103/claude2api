package service

import (
	"strings"
	"testing"
)

func TestPreparePromptForUpstreamUsesStructuredPlanDespiteForceInline(t *testing.T) {
	prompt := Prompt{
		Text:        strings.Repeat("x", 13),
		ForceInline: true,
		LongContext: &LongContextPlan{
			InlineText: "tool controller",
			Attachments: []TextAttachment{
				{FileName: "tool-catalog.txt", FileType: "text/plain", Content: "Read(path: string)"},
				{FileName: "conversation.txt", FileType: "text/plain", Content: "**User**: read README"},
			},
		},
	}

	text, attachments, moved := preparePromptForUpstream(prompt, 12)
	if !moved {
		t.Fatal("超阈值工具提示应改用分离附件")
	}
	if text != "tool controller" {
		t.Fatalf("inline text = %q", text)
	}
	if len(attachments) != 2 || attachments[0].FileName != "tool-catalog.txt" || attachments[1].FileName != "conversation.txt" {
		t.Fatalf("attachments = %#v", attachments)
	}
}

func TestPreparePromptForUpstreamKeepsUnplannedForceInlinePrompt(t *testing.T) {
	prompt := Prompt{Text: strings.Repeat("x", 13), ForceInline: true}

	text, attachments, moved := preparePromptForUpstream(prompt, 12)
	if moved || len(attachments) != 0 || text != prompt.Text {
		t.Fatalf("unplanned ForceInline prompt changed: text=%q attachments=%#v moved=%v", text, attachments, moved)
	}
}

func TestPreparePromptForAttemptPreservesLongContextPlanForRetry(t *testing.T) {
	originalText := strings.Repeat("x", 13)
	prompt := Prompt{
		Text: originalText,
		LongContext: &LongContextPlan{
			InlineText: "tool controller",
			Attachments: []TextAttachment{
				{FileName: "tool-catalog.txt", FileType: "text/plain", Content: "Read(path: string)"},
				{FileName: "conversation.txt", FileType: "text/plain", Content: "**User**: read README"},
			},
		},
	}

	first, firstAttachments, firstMoved := preparePromptForAttempt(prompt, 12)
	if !firstMoved || first.Text != "tool controller" || len(firstAttachments) != 2 {
		t.Fatalf("first attempt = text=%q attachments=%#v moved=%v", first.Text, firstAttachments, firstMoved)
	}
	if prompt.Text != originalText {
		t.Fatalf("preparing an attempt mutated the retry source: %q", prompt.Text)
	}

	second, secondAttachments, secondMoved := preparePromptForAttempt(prompt, 12)
	if !secondMoved || second.Text != "tool controller" || len(secondAttachments) != 2 {
		t.Fatalf("retry lost the long-context plan: text=%q attachments=%#v moved=%v", second.Text, secondAttachments, secondMoved)
	}
}

func TestPreparePromptForUpstreamUsesCompactControllerWhenNeeded(t *testing.T) {
	prompt := Prompt{
		Text: strings.Repeat("x", 21),
		LongContext: &LongContextPlan{
			InlineText:        "tool controller with optional name index",
			CompactInlineText: "tool controller",
			Attachments: []TextAttachment{
				{FileName: "tool-catalog.txt", FileType: "text/plain", Content: "Read(path: string)"},
			},
		},
	}

	text, attachments, moved := preparePromptForUpstream(prompt, 20)
	if !moved || text != "tool controller" || len(attachments) != 1 {
		t.Fatalf("compact plan = text=%q attachments=%#v moved=%v", text, attachments, moved)
	}
}
