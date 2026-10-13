package adapter

import (
	"strings"
	"testing"
)

// feedAll 把整段文本喂入流式解析器并收尾，返回全部事件。
func feedAll(t *testing.T, chunks ...string) []TaggedStreamEvent {
	t.Helper()
	p := NewTaggedStreamParser()
	var all []TaggedStreamEvent
	for _, c := range chunks {
		ev, err := p.Feed(c)
		if err != nil {
			t.Fatalf("Feed 不应返回错误: %v", err)
		}
		all = append(all, ev...)
	}
	ev, err := p.Finish()
	if err != nil {
		t.Fatalf("Finish 不应返回错误: %v", err)
	}
	return append(all, ev...)
}

func collectText(ev []TaggedStreamEvent) string {
	var s string
	for _, e := range ev {
		if e.Type == EventBlockDelta {
			s += e.Text
		}
	}
	return s
}

func hasToolCall(ev []TaggedStreamEvent) bool {
	for _, e := range ev {
		if e.Type == EventToolCall {
			return true
		}
	}
	return false
}

// 上游完全不遵守协议：纯文本应原样降级为文本，不报错。
func TestStreamPlainTextDegrade(t *testing.T) {
	ev := feedAll(t, "Hello, this is plain prose without any tags.")
	if got := collectText(ev); got != "Hello, this is plain prose without any tags." {
		t.Fatalf("文本不匹配: %q", got)
	}
	if hasToolCall(ev) {
		t.Fatal("不应产生工具调用")
	}
}

// 流在标签中途截断：半截标签当普通文本吐出，不报错。
func TestStreamIncompleteTagDegrade(t *testing.T) {
	ev := feedAll(t, "partial answer <tool_cal")
	txt := collectText(ev)
	if txt != "partial answer <tool_cal" {
		t.Fatalf("文本不匹配: %q", txt)
	}
}

// 正常的 final_answer 协议。
func TestStreamFinalAnswer(t *testing.T) {
	ev := feedAll(t, "<think>reasoning</think><final_answer>done</final_answer>")
	if got := collectText(ev); got != "reasoningdone" {
		t.Fatalf("文本不匹配: %q", got)
	}
}

// 正常的工具调用协议（分片喂入）。
func TestStreamToolCallSplit(t *testing.T) {
	ev := feedAll(t, `<tool_ca`, `lls>[{"name":"Read",`, `"arguments":{"path":"a"}}]</tool_calls>`)
	if !hasToolCall(ev) {
		t.Fatal("应产生工具调用")
	}
}

// 工具块内 JSON 非法：降级为文本，不报错。
func TestStreamBadToolJSONDegrade(t *testing.T) {
	ev := feedAll(t, `<tool_calls>not json</tool_calls>`)
	if hasToolCall(ev) {
		t.Fatal("非法 JSON 不应产生工具调用")
	}
	if got := collectText(ev); got == "" {
		t.Fatal("非法 JSON 应降级为文本")
	}
}

// 非流式容错解析：纯文本降级为 final answer。
func TestParseTolerantPlainText(t *testing.T) {
	out := ParseTaggedOutputTolerant("just plain text")
	if !out.IsFinalAnswer() || out.FinalAnswer != "just plain text" {
		t.Fatalf("应降级为 final answer, 得到 %+v", out)
	}
	if out.IsToolCall() {
		t.Fatal("不应有工具调用")
	}
}

// 非流式容错解析：合法协议仍正常解析。
func TestParseTolerantValid(t *testing.T) {
	out := ParseTaggedOutputTolerant(`<tool_calls>[{"name":"Read","arguments":{"p":"x"}}]</tool_calls>`)
	if !out.IsToolCall() || len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "Read" {
		t.Fatalf("应解析出工具调用, 得到 %+v", out)
	}
}

func TestOutputOptions(t *testing.T) {
	f := outputFilter{stop: []string{"STOP"}, maxBytes: 100}
	if got := f.push("hello ST", false) + f.push("OP hidden", false) + f.push("", true); got != "hello " {
		t.Fatalf("stop 过滤失败: %q", got)
	}
	if got := toolChoiceInstruction([]byte(`{"type":"function","function":{"name":"Read"}}`)); !strings.Contains(got, "Read") {
		t.Fatalf("tool_choice 未生效: %q", got)
	}
	f = outputFilter{maxBytes: 4}
	if got := f.push("123456", false); got != "1234" {
		t.Fatalf("max_tokens 过滤失败: %q", got)
	}
}

func toolArgs(ev []TaggedStreamEvent) map[string]any {
	for _, e := range ev {
		if e.Type == EventToolCall {
			return e.Arguments
		}
	}
	return nil
}

func TestParsePreambleAndWindowsPath(t *testing.T) {
	raw := "先看一下这个文件\n" + `<tool_calls>[{"name":"Read","arguments":{"path":"C:\imports\data.json"}}]</tool_calls>`
	out := ParseTaggedOutputTolerant(raw)
	if !out.IsToolCall() || len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "Read" {
		t.Fatalf("should parse tool call, got %+v", out)
	}
	if got := out.ToolCalls[0].Arguments["path"]; got != "C:\\imports\\data.json" {
		t.Fatalf("path = %#v", got)
	}
}

func TestStreamWindowsPathToolCall(t *testing.T) {
	ev := feedAll(t, "先看一下\n", `<tool_calls>[{"name":"Bash","arguments":{"command":"dir C:\imports"}}]</tool_calls>`)
	if !hasToolCall(ev) {
		t.Fatalf("should parse tool call, text=%q", collectText(ev))
	}
	if got := toolArgs(ev)["command"]; got != "dir C:\\imports" {
		t.Fatalf("command = %#v", got)
	}
}

func TestRepairKeepsValidJSONEscape(t *testing.T) {
	out, err := ParseTaggedOutput(`<tool_call>{"name":"Bash","arguments":{"command":"echo hi\nthere"}}</tool_call>`)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.ToolCalls[0].Arguments["command"]; got != "echo hi\nthere" {
		t.Fatalf("command = %#v", got)
	}
}

func TestFlattenedToolArguments(t *testing.T) {
	raw := `<tool_calls>[{"name":"Grep","arguments":{"pattern":"arcStaffName"}},{"name":"Grep","pattern":"arcStaffName","path":"C:\\Temp"},{"name":"Glob","pattern":"shi_*","path":"C:\\Temp"}]</tool_calls>`
	out, err := ParseTaggedOutput("先确认字段。\n" + raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ToolCalls) != 3 {
		t.Fatalf("got %+v", out.ToolCalls)
	}
	if out.ToolCalls[1].Arguments["pattern"] != "arcStaffName" || out.ToolCalls[1].Arguments["path"] != `C:\Temp` {
		t.Fatalf("flattened = %#v", out.ToolCalls[1].Arguments)
	}
	if out.ToolCalls[2].Name != "Glob" || out.ToolCalls[2].Arguments["pattern"] != "shi_*" {
		t.Fatalf("glob = %#v", out.ToolCalls[2])
	}
	ev := feedAll(t, "先确认字段。\n"+raw)
	var names []string
	for _, e := range ev {
		if e.Type == EventToolCall {
			names = append(names, e.Name)
		}
	}
	if strings.Join(names, ",") != "Grep,Grep,Glob" {
		t.Fatalf("stream names = %v text=%q", names, collectText(ev))
	}
}
