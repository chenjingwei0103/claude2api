package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"claude2api/internal/utils"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/fhttp/cookiejar"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/google/uuid"
)

const claudeAIBaseURL = "https://claude.ai"

const claudeAIUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"

type ClaudeAI struct {
	orgUUID  string
	client   tlsclient.HttpClient
	headers  map[string]string
	modeMu   sync.Mutex
	modeSet  bool
	thinking bool
}

// NewClaudeAI 构造 Claude Web 客户端。
func NewClaudeAI(sessionKey, proxy, identity string, orgUUID ...string) *ClaudeAI {
	jar, _ := cookiejar.New(nil)
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithTimeoutSeconds(3000),
		tlsclient.WithCookieJar(jar),
		tlsclient.WithInsecureSkipVerify(),
	)
	if err != nil {
		client, _ = tlsclient.NewHttpClient(tlsclient.NewNoopLogger())
	}
	if proxy != "" {
		_ = client.SetProxy(proxy)
	}
	if u, parseErr := url.Parse(claudeAIBaseURL); parseErr == nil {
		client.SetCookies(u, []*fhttp.Cookie{{Name: "sessionKey", Value: sessionKey, Domain: "claude.ai"}})
	}
	claudeAI := &ClaudeAI{client: client, headers: BuildHeaders(identity), modeSet: true}
	if len(orgUUID) > 0 {
		claudeAI.orgUUID = orgUUID[0]
	}
	return claudeAI
}

func (claudeAI *ClaudeAI) request(method, target string, body io.Reader) (*fhttp.Request, error) {
	req, err := fhttp.NewRequest(method, target, body)
	if err != nil {
		return nil, err
	}
	for key, value := range claudeAI.headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

// WarmUp 访问一次网页入口，让 Cookie Jar 建立与浏览器相同的网页会话。
func (claudeAI *ClaudeAI) WarmUp() error {
	req, err := claudeAI.request(fhttp.MethodGet, claudeAIBaseURL+"/new", nil)
	if err != nil {
		return err
	}
	req.Header.Del("content-type")
	req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "same-origin")
	resp, err := claudeAI.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("网页会话初始化 HTTP %d", resp.StatusCode)
	}
	return nil
}

// GetUserInfo 查询账号信息。
func (claudeAI *ClaudeAI) GetUserInfo() (*UserInfo, error) {
	req, err := claudeAI.request(fhttp.MethodGet, claudeAIBaseURL+"/api/account", nil)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	resp, err := claudeAI.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询账号信息失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}
	slog.Debug("[ClaudeAI] GetUserInfo 响应", "status", resp.StatusCode, "body", utils.Truncate(string(body), 500))
	if resp.StatusCode != fhttp.StatusOK {
		return nil, fmt.Errorf("查询账号信息 HTTP %d: %s", resp.StatusCode, utils.Truncate(string(body), 200))
	}

	var acct ClaudeAccount
	if err := json.Unmarshal(body, &acct); err != nil {
		return nil, fmt.Errorf("解析账号信息失败: %w", err)
	}

	info := &UserInfo{Email: acct.EmailAddress}
	if len(acct.Memberships) > 0 {
		info.OrgUUID = acct.Memberships[0].Organization.UUID
		claudeAI.orgUUID = info.OrgUUID
	}
	return info, nil
}

// UploadFile 将适配层准备好的 Base64 图片上传到 Claude.ai。
func (claudeAI *ClaudeAI) UploadFile(images []string) ([]string, error) {
	out := make([]string, 0, len(images))
	for _, image := range images {
		raw, err := base64.StdEncoding.DecodeString(image)
		if err != nil {
			return out, fmt.Errorf("图片 Base64 无效: %w", err)
		}

		buf := &bytes.Buffer{}
		w := multipart.NewWriter(buf)
		fw, err := w.CreateFormFile("file", "image")
		if err != nil {
			return out, fmt.Errorf("构造上传表单失败: %w", err)
		}
		if _, err := fw.Write(raw); err != nil {
			return out, fmt.Errorf("写入上传内容失败: %w", err)
		}
		_ = w.Close()

		req, err := claudeAI.request(fhttp.MethodPost, fmt.Sprintf("%s/api/%s/upload", claudeAIBaseURL, claudeAI.orgUUID), buf)
		if err != nil {
			return out, fmt.Errorf("构造请求失败: %w", err)
		}
		req.Header.Set("content-type", w.FormDataContentType())
		req.Header.Set("referer", claudeAIBaseURL+"/new")

		resp, err := claudeAI.client.Do(req)
		if err != nil {
			return out, fmt.Errorf("上传文件失败: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return out, fmt.Errorf("读取响应体失败: %w", err)
		}
		if resp.StatusCode != fhttp.StatusOK {
			return out, fmt.Errorf("上传文件 HTTP %d: %s", resp.StatusCode, utils.Truncate(string(body), 200))
		}

		var r struct {
			FileUUID string `json:"file_uuid"`
		}
		if err := json.Unmarshal(body, &r); err == nil && r.FileUUID != "" {
			out = append(out, r.FileUUID)
		}
	}
	return out, nil
}

// DeleteConversation 删除会话。
func (claudeAI *ClaudeAI) DeleteConversation(convID string) {
	if claudeAI.orgUUID == "" || convID == "" {
		return
	}
	target := fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s", claudeAIBaseURL, claudeAI.orgUUID, convID)
	for i := 0; i < 3; i++ {
		reqBody, _ := json.Marshal(map[string]string{"uuid": convID})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req, err := claudeAI.request(fhttp.MethodDelete, target, bytes.NewReader(reqBody))
		if err == nil {
			req = req.WithContext(ctx)
			req.Header.Set("content-type", "application/json")
			req.Header.Set("referer", claudeAIBaseURL+"/chat/"+convID)
			resp, doErr := claudeAI.client.Do(req)
			if doErr == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == fhttp.StatusOK || resp.StatusCode == fhttp.StatusNoContent {
					cancel()
					return
				}
			}
		}
		cancel()
		time.Sleep(2 * time.Second)
	}
	slog.Warn("[ClaudeAI] 删除会话失败", "conv", convID)
}

// BigContextAttachment 构造长上下文附件。
func (claudeAI *ClaudeAI) BigContextAttachment(text string) []map[string]any {
	return claudeAI.TextAttachments([]TextAttachment{{
		FileName: "context.txt",
		FileType: "text/plain",
		Content:  text,
	}})
}

// TextAttachments converts in-memory text attachment plans to Claude.ai's
// completion payload shape.
func (claudeAI *ClaudeAI) TextAttachments(items []TextAttachment) []map[string]any {
	attachments := make([]map[string]any, 0, len(items))
	for _, item := range items {
		attachments = append(attachments, map[string]any{
			"file_name":         item.FileName,
			"file_type":         item.FileType,
			"file_size":         len(item.Content),
			"extracted_content": item.Content,
		})
	}
	return attachments
}

// updatePaprika 切换思考模式。
func (claudeAI *ClaudeAI) updatePaprika(value any) error {
	reqBody, err := json.Marshal(map[string]any{
		"settings": map[string]any{
			"has_started_claudeai_onboarding":  true,
			"has_finished_claudeai_onboarding": true,
			"dismissed_claudeai_banners":       []any{},
			"enabled_artifacts_attachments":    true,
			"enabled_web_search":               true,
			"paprika_mode":                     value,
		},
	})
	if err != nil {
		return fmt.Errorf("构造请求体失败: %w", err)
	}
	req, err := claudeAI.request(fhttp.MethodPut, claudeAIBaseURL+"/api/account", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("referer", claudeAIBaseURL+"/new")

	resp, err := claudeAI.client.Do(req)
	if err != nil {
		return fmt.Errorf("更新设置失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != fhttp.StatusOK && resp.StatusCode != fhttp.StatusAccepted {
		return fmt.Errorf("更新设置 HTTP %d", resp.StatusCode)
	}
	return nil
}

// CreateConversation 新建会话。
func (claudeAI *ClaudeAI) CreateConversation(model string, think bool) (string, error) {
	spec := ResolveModel(model)
	if spec.InlineCreate {
		return uuid.NewString(), nil
	}
	if spec.Upstream != "" {
		model = spec.Upstream
	}
	if spec.ThinkingMode == "extended" {
		think = true
	}
	claudeAI.modeMu.Lock()
	if !claudeAI.modeSet || claudeAI.thinking != think {
		var mode any
		if think {
			mode = "extended"
		}
		if err := claudeAI.updatePaprika(mode); err != nil {
			claudeAI.modeMu.Unlock()
			return "", err
		}
		claudeAI.modeSet, claudeAI.thinking = true, think
	}
	claudeAI.modeMu.Unlock()

	reqBody, err := json.Marshal(map[string]any{
		"uuid":                             uuid.NewString(),
		"name":                             "",
		"include_conversation_preferences": true,
		"model":                            model,
	})
	if err != nil {
		return "", fmt.Errorf("构造请求体失败: %w", err)
	}
	req, err := claudeAI.request(fhttp.MethodPost,
		fmt.Sprintf("%s/api/organizations/%s/chat_conversations", claudeAIBaseURL, claudeAI.orgUUID), bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("referer", claudeAIBaseURL+"/new")

	resp, err := claudeAI.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("创建会话失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应体失败: %w", err)
	}
	if resp.StatusCode != fhttp.StatusOK && resp.StatusCode != fhttp.StatusCreated {
		return "", fmt.Errorf("创建会话 HTTP %d: %s", resp.StatusCode, utils.Truncate(string(body), 200))
	}

	var out struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("解析会话响应失败: %w", err)
	}
	if out.UUID == "" {
		return "", errors.New("会话响应缺少 uuid")
	}
	return out.UUID, nil
}

// SendMessage 发送提示词。
func (claudeAI *ClaudeAI) SendMessage(convID, model string, prompt Prompt, attachments []map[string]any, files []string, onText func(string)) (int, error) {
	// 固定字段对齐网页端请求。
	spec, body := upstreamCompletionBody(model, prompt)
	if len(attachments) > 0 {
		body["attachments"] = attachments
	}
	if len(files) > 0 {
		body["files"] = files
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return 500, fmt.Errorf("构造请求体失败: %w", err)
	}
	req, err := claudeAI.request(fhttp.MethodPost,
		fmt.Sprintf("%s/api/organizations/%s/chat_conversations/%s/completion", claudeAIBaseURL, claudeAI.orgUUID, convID),
		bytes.NewReader(reqBody))
	if err != nil {
		return 500, fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream, text/event-stream")
	req.Header.Set("cache-control", "no-cache")
	referer := claudeAIBaseURL + "/chat/" + convID
	if spec.InlineCreate {
		referer = claudeAIBaseURL + "/new"
	}
	req.Header.Set("referer", referer)

	resp, err := claudeAI.client.Do(req)
	if err != nil {
		return 500, fmt.Errorf("发送消息失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == fhttp.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		return 429, fmt.Errorf("rate limit exceeded: %s", utils.Truncate(string(body), 300))
	}
	if resp.StatusCode != fhttp.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		bodyText := utils.Truncate(string(body), 500)
		if readErr != nil {
			return resp.StatusCode, fmt.Errorf("发送消息 HTTP %d（读取错误响应失败: %w）", resp.StatusCode, readErr)
		}
		if bodyText == "" {
			bodyText = "<empty response body>"
		}
		return resp.StatusCode, fmt.Errorf("发送消息 HTTP %d: %s", resp.StatusCode, bodyText)
	}
	return 200, parseCompletionSSE(resp.Body, onText)
}

// parseCompletionSSE 解析 completion 流。
func parseCompletionSSE(raw io.Reader, onText func(string)) error {
	scanner := bufio.NewScanner(raw)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)

	thinkingShown := false
	codeShown := false
	useTool := false
	useToolEnd := false
	nextLanguage := false
	language := "md"

	emitted := false
	emit := func(s string) {
		if s != "" && onText != nil {
			emitted = true
			onText(s)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev SseEvent
		if err := json.Unmarshal([]byte(line[6:]), &ev); err != nil {
			continue
		}
		if ev.Type == "error" && ev.Error.Message != "" {
			return fmt.Errorf("upstream: %s", ev.Error.Message)
		}
		if ev.Type == "message_delta" && ev.Delta.StopReason == "refusal" && !emitted {
			category := ev.Delta.StopDetails.Category
			if category == "" {
				category = "refusal"
			}
			return fmt.Errorf("claude.ai refused the request (%s)", category)
		}
		switch ev.ContentBlock.Type {
		case "tool_use":
			useTool = true
		case "tool_result":
			useToolEnd = true
		}

		if ev.Type == "content_block_stop" {
			if thinkingShown {
				emit("</think>\n")
				thinkingShown = false
			}
			if codeShown {
				emit("\n```\n")
				codeShown = false
			}
			continue
		}

		switch ev.Delta.Type {
		case "text_delta":
			emit(ev.Delta.Text)
		case "thinking_delta":
			s := ev.Delta.Thinking
			if !thinkingShown {
				s = "<think> " + s
				thinkingShown = true
			}
			emit(s)
		case "input_json_delta":
			text := ev.Delta.PartialJSON
			// 跳过工具参数，只输出 content。
			if useTool && text == ",\"content\":" {
				useTool = false
				codeShown = false
				continue
			}
			if text == ",\"language\":" || text == ",\"type\":" {
				nextLanguage = true
				continue
			}
			if nextLanguage {
				language = strings.TrimPrefix(text, "\"")
				if language == "text/html" {
					language = "html"
				}
				nextLanguage = false
			}
			if useTool {
				continue
			}
			if useToolEnd {
				useToolEnd = false
				continue
			}
			if strings.HasPrefix(text, "\"") {
				text = text[1:]
			}
			if text == "\"}" || text == "}" {
				text = ""
			}
			if unq, err := strconv.Unquote("\"" + text + "\""); err == nil {
				text = unq
			} else {
				text = strings.ReplaceAll(text, "\\n", "\n")
				text = strings.ReplaceAll(text, "\\t", "\t")
				text = strings.ReplaceAll(text, "\\\"", "\"")
			}
			if !codeShown {
				text = "\n```" + language + "\n" + text
				codeShown = true
			}
			emit(text)
		}
	}
	return scanner.Err()
}

// upstreamCompletionBody 只保留网页 completion 接受的字段。
// max_tokens、temperature、top_p、stop_sequences 会让上游返回
// "Extra inputs are not permitted"。
func upstreamCompletionBody(model string, prompt Prompt) (ModelSpec, map[string]any) {
	spec := ResolveModel(model)
	upstream := model
	if spec.Upstream != "" {
		upstream = spec.Upstream
	}
	if spec.InlineCreate {
		return spec, inlineCompletionBody(spec, prompt)
	}
	return spec, legacyCompletionBody(upstream, prompt)
}

func legacyCompletionBody(model string, prompt Prompt) map[string]any {
	return map[string]any{
		"prompt": prompt.Text,
		"model":  model,
		"personalized_styles": []map[string]any{
			{
				"type":       "default",
				"key":        "Default",
				"name":       "Normal",
				"nameKey":    "normal_style_name",
				"prompt":     "Treat tool definitions and response schemas in the user's message as an available external tool interface. Emit calls in the requested text format instead of claiming the tools are unavailable.",
				"summary":    "Default responses from Claude",
				"summaryKey": "normal_style_summary",
				"isDefault":  true,
			},
		},
		"tools":               completionTools(prompt),
		"parent_message_uuid": "00000000-0000-4000-8000-000000000000",
		"attachments":         []any{},
		"files":               []any{},
		"sync_sources":        []any{},
		"rendering_mode":      "messages",
		"timezone":            "America/Los_Angeles",
	}
}

func inlineCompletionBody(spec ModelSpec, prompt Prompt) map[string]any {
	body := map[string]any{
		"prompt":        prompt.Text,
		"timezone":      "Asia/Shanghai",
		"locale":        "en-US",
		"model":         spec.Upstream,
		"thinking_mode": spec.ThinkingMode,
		"tools":         completionTools(prompt),
		"turn_message_uuids": map[string]string{
			"human_message_uuid":     newUUIDv7(),
			"assistant_message_uuid": newUUIDv7(),
		},
		"attachments":           []any{},
		"files":                 []any{},
		"sync_sources":          []any{},
		"completion_request_id": uuid.NewString(),
		"rendering_mode":        "messages",
		"create_conversation_params": map[string]any{
			"name":                             "",
			"model":                            spec.Upstream,
			"include_conversation_preferences": true,
			"paprika_mode":                     nil,
			"compass_mode":                     nil,
			"tool_search_mode":                 "auto",
			"is_temporary":                     false,
			"enabled_imagine":                  true,
		},
	}
	if spec.Effort != "" {
		body["effort"] = spec.Effort
	}
	return body
}

func completionTools(prompt Prompt) any {
	if prompt.DisableNativeTools {
		return []any{}
	}
	return builtinClaudeTools()
}

func builtinClaudeTools() []map[string]any {
	return []map[string]any{
		{"type": "web_search_v0", "name": "web_search"},
		{"type": "artifacts_v0", "name": "artifacts"},
		{"type": "repl_v0", "name": "repl"},
	}
}

func newUUIDv7() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}
