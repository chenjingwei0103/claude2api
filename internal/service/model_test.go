package service

import "testing"

func TestPublicModelsMatchAccountStrip(t *testing.T) {
	want := []string{
		"claude-sonnet-5-5",
		"claude-sonnet-5-5-thinking",
		"claude-sonnet-5-5-thinking-low",
		"claude-sonnet-5-5-thinking-medium",
		"claude-sonnet-5-5-thinking-high",
		"claude-sonnet-5-5-thinking-xhigh",
		"claude-sonnet-5-5-thinking-max",
		"claude-sonnet-5",
		"claude-sonnet-5-thinking",
		"claude-sonnet-5-thinking-low",
		"claude-sonnet-5-thinking-medium",
		"claude-sonnet-5-thinking-high",
		"claude-sonnet-5-thinking-xhigh",
		"claude-sonnet-5-thinking-max",
		"claude-sonnet-4-6",
		"claude-sonnet-4-6-thinking",
		"claude-sonnet-4-6-thinking-low",
		"claude-sonnet-4-6-thinking-medium",
		"claude-sonnet-4-6-thinking-high",
		"claude-sonnet-4-6-thinking-max",
		"claude-haiku-4-5-20251001",
		"claude-haiku-4-5-20251001-thinking",
	}
	got := PublicModels()
	if len(got) != len(want) {
		t.Fatalf("len %d != %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d got %s want %s", i, got[i], want[i])
		}
	}
}

func TestResolveModelFromHAR(t *testing.T) {
	spec := ResolveModel("claude-sonnet-5.5")
	if spec.Upstream != "claude-sonnet-5-5" || spec.ThinkingMode != "auto" || spec.Effort != "medium" || !spec.InlineCreate {
		t.Fatalf("base alias: %+v", spec)
	}
	high := ResolveModel("claude-sonnet-5-5-thinking-xhigh")
	if high.Upstream != "claude-sonnet-5-5" || high.ThinkingMode != "auto" || high.Effort != "xhigh" {
		t.Fatalf("xhigh: %+v", high)
	}
	sonnet46 := ResolveModel("claude-sonnet-4-6")
	if sonnet46.ThinkingMode != "off" || sonnet46.Effort != "low" {
		t.Fatalf("sonnet 4.6 base: %+v", sonnet46)
	}
	thinking := ResolveModel("claude-sonnet-4-6-thinking-max")
	if thinking.ThinkingMode != "auto" || thinking.Effort != "max" {
		t.Fatalf("sonnet 4.6 max: %+v", thinking)
	}
	haiku := ResolveModel("claude-haiku-4-5-20251001-thinking")
	if haiku.Upstream != "claude-haiku-4-5-20251001" || haiku.ThinkingMode != "extended" || haiku.Effort != "" {
		t.Fatalf("haiku: %+v", haiku)
	}
}
