package mistral

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var mistralEffortLadder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// 按模型能力折算，不能用 API 通用枚举推断模型支持范围
// https://docs.mistral.ai/inference/conversations/reasoning
var mistralSupportedEfforts = []struct {
	prefix  string
	efforts []string
}{
	// DS 托管接口保留现有约定，不套用 DeepSeek 原生 API 的映射
	{prefix: "deepseek", efforts: []string{"none", "low", "high", "xhigh", "max"}},
	{prefix: "zai-glm-5-3", efforts: []string{"low", "high", "max"}},
	{prefix: "mistral-large-latest", efforts: []string{"low", "medium", "high"}},
	{prefix: "mistral-large-4", efforts: []string{"low", "medium", "high"}},
	{prefix: "mistral-large", efforts: nil},
	{prefix: "mistral-small-2603", efforts: []string{"none", "high"}},
	{prefix: "mistral-small-latest", efforts: []string{"none", "high"}},
	{prefix: "mistral-medium-3-5", efforts: []string{"none", "high"}},
	{prefix: "mistral-medium-latest", efforts: []string{"none", "high"}},
	{prefix: "magistral-", efforts: nil},
}

// 不支持的档位就近折算，同距离时取偏强档位
func mistralReasoningEffort(model, effort string) string {
	effort = strings.ToLower(strings.TrimSpace(effort))
	index := slices.Index(mistralEffortLadder, effort)
	if index < 0 {
		return ""
	}
	// 仅折算当前官方别名，不改变发给上游的模型名
	switch model {
	case "zai-glm-5", "zai-glm-latest":
		model = "zai-glm-5-3"
	case "mistral-medium-3":
		model = "mistral-medium-3-5"
	}
	supported := mistralEffortLadder
	for _, entry := range mistralSupportedEfforts {
		if strings.HasPrefix(model, entry.prefix) {
			supported = entry.efforts
			break
		}
	}
	if slices.Contains(supported, effort) {
		return effort
	}
	nearest, nearestDistance := "", len(mistralEffortLadder)
	for _, candidate := range supported {
		distance := slices.Index(mistralEffortLadder, candidate) - index
		if distance < 0 {
			distance = -distance
		}
		if distance <= nearestDistance {
			nearest, nearestDistance = candidate, distance
		}
	}
	return nearest
}

type mistralTextChunk struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mistralContentBlock struct {
	Type      string            `json:"type"`
	Text      *string           `json:"text,omitempty"`
	Thinking  common.RawMessage `json:"thinking,omitempty"`
	Signature common.RawMessage `json:"signature,omitempty"`
	Closed    *bool             `json:"closed,omitempty"`
}

func mistralThinkingChunk(parts []mistralTextChunk) (mistralContentBlock, error) {
	thinking, err := common.Marshal(parts)
	return mistralContentBlock{Type: "thinking", Thinking: thinking}, err
}

func hasReplayThinking(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && text != "[reasoning unavailable]"
}

// 思考放进原生 ThinkChunk，普通文本保持原有合并行为
func mistralThinkingContent(message dto.Message) (any, error) {
	var blocks []mistralContentBlock
	switch content := message.Content.(type) {
	case nil:
	case string:
		if message.Role != "assistant" || !hasReplayThinking(message.GetReasoningContent()) {
			return content, nil
		}
		blocks = []mistralContentBlock{{Type: "text", Text: new(content)}}
	case []mistralContentBlock:
		blocks = content
	case []dto.MediaContent:
		for _, part := range content {
			if part.Type == "text" {
				blocks = append(blocks, mistralContentBlock{Type: "text", Text: new(part.Text)})
			}
		}
	default:
		encoded, err := common.Marshal(content)
		if err != nil {
			return nil, fmt.Errorf("invalid mistral message content: %w", err)
		}
		err = common.Unmarshal(encoded, &blocks)
		if err != nil {
			return nil, fmt.Errorf("invalid mistral message content: %w", err)
		}
	}
	var text strings.Builder
	content := make([]mistralContentBlock, 0, len(blocks)+1)
	hasThinking := false
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != nil {
				text.WriteString(*block.Text)
				content = append(content, mistralContentBlock{Type: "text", Text: block.Text})
			}
		case "thinking":
			var original []common.RawMessage
			if err := common.Unmarshal(block.Thinking, &original); err != nil {
				return nil, fmt.Errorf("invalid mistral thinking chunk: %w", err)
			}
			parts := make([]common.RawMessage, 0, len(original))
			for _, raw := range original {
				part := ojson.ParseBytes(raw)
				switch part.Get("type").String() {
				case "text":
					if value := part.Get("text"); value.Type == ojson.String && hasReplayThinking(value.Str) {
						parts = append(parts, raw)
					}
				case "reference", "tool_reference":
					parts = append(parts, raw)
				}
			}
			if len(parts) > 0 {
				if len(parts) != len(original) {
					var err error
					block.Thinking, err = common.Marshal(parts)
					if err != nil {
						return nil, err
					}
				}
				hasThinking = true
				content = append(content, block)
			}
		}
	}
	// 原生块优先，避免客户端同时回传 reasoning_content 时重复思考
	if reasoning := message.GetReasoningContent(); message.Role == "assistant" && !hasThinking && hasReplayThinking(reasoning) {
		block, err := mistralThinkingChunk([]mistralTextChunk{{Type: "text", Text: reasoning}})
		if err != nil {
			return nil, err
		}
		hasThinking = true
		content = slices.Insert(content, 0, block)
	}
	if !hasThinking {
		return text.String(), nil
	}
	return content, nil
}

// 保留 assistant 原始思考，空文本保证共享转换器不会漏掉纯思考轮次
func prepareClaudeThinkingReplay(request *dto.ClaudeRequest) (*dto.ClaudeRequest, []any, error) {
	prepared := *request
	prepared.Messages = slices.Clone(request.Messages)
	var contents []any
	for i, message := range request.Messages {
		if message.Role != "assistant" {
			continue
		}
		var parts []dto.ClaudeMediaMessage
		if message.IsStringContent() {
			parts = []dto.ClaudeMediaMessage{{Type: "text", Text: new(message.GetStringContent())}}
		} else {
			var err error
			parts, err = message.ParseContent()
			if err != nil {
				return nil, nil, err
			}
		}
		var blocks []mistralContentBlock
		for _, part := range parts {
			switch part.Type {
			case "text", "input_text":
				blocks = append(blocks, mistralContentBlock{Type: "text", Text: part.Text})
			case "thinking":
				if part.Thinking != nil {
					block, err := mistralThinkingChunk([]mistralTextChunk{{Type: "text", Text: *part.Thinking}})
					if err != nil {
						return nil, nil, err
					}
					blocks = append(blocks, block)
				}
			}
		}
		content, err := mistralThinkingContent(dto.Message{Role: "assistant", Content: blocks})
		if err != nil {
			return nil, nil, err
		}
		contents = append(contents, content)
		prepared.Messages[i].Content = append(slices.Clone(parts), dto.ClaudeMediaMessage{Type: "text", Text: new("")})
	}
	return &prepared, contents, nil
}

// 把 Responses 推理条目并入对应 assistant 轮次，工具调用继续复用共享转换
func mistralResponsesThinkingInput(input common.RawMessage) (common.RawMessage, error) {
	if common.GetJsonType(input) != "array" {
		return input, nil
	}
	// 普通请求直接复用原文，转义字段交给完整解析
	if !bytes.Contains(input, []byte(`"reasoning"`)) && !bytes.Contains(input, []byte(`\u`)) {
		return input, nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return nil, err
	}
	out := make([]common.RawMessage, 0, len(items))
	var pending []mistralTextChunk
	changed := false
	for _, raw := range items {
		item := ojson.ParseBytes(raw)
		kind := strings.TrimSpace(item.Get("type").String())
		content := item.Get("content")
		if kind == "reasoning" {
			changed = true
			var parts []mistralTextChunk
			if content.Exists() && content.Type != ojson.Null {
				if err := common.Unmarshal([]byte(content.Raw), &parts); err != nil {
					return nil, err
				}
			}
			start := len(pending)
			for _, part := range parts {
				if part.Type == "reasoning_text" && hasReplayThinking(part.Text) {
					pending = append(pending, mistralTextChunk{Type: "text", Text: part.Text})
				}
			}
			if len(pending) == start {
				var summary []mistralTextChunk
				if value := item.Get("summary"); value.Exists() && value.Type != ojson.Null {
					if err := common.Unmarshal([]byte(value.Raw), &summary); err != nil {
						return nil, err
					}
				}
				for _, part := range summary {
					if part.Type == "summary_text" && hasReplayThinking(part.Text) {
						pending = append(pending, mistralTextChunk{Type: "text", Text: part.Text})
					}
				}
			}
			continue
		}
		if len(pending) > 0 {
			block, err := mistralThinkingChunk(pending)
			if err != nil {
				return nil, err
			}
			thinking, err := common.Marshal(block)
			if err != nil {
				return nil, err
			}
			blocks := []common.RawMessage{thinking}
			if strings.TrimSpace(item.Get("role").String()) == "assistant" && (kind == "message" || kind == "") {
				if content.Type == ojson.String {
					encoded, err := common.Marshal(mistralTextChunk{Type: "text", Text: content.Str})
					if err != nil {
						return nil, err
					}
					blocks = append(blocks, encoded)
				} else if content.IsArray() {
					var original []common.RawMessage
					if err := common.Unmarshal([]byte(content.Raw), &original); err != nil {
						return nil, err
					}
					blocks = append(blocks, original...)
				} else if content.Exists() && content.Type != ojson.Null {
					return nil, fmt.Errorf("invalid mistral assistant content: expected string or array")
				}
				doc := ojson.NewDoc(raw)
				if err := relaycommon.SetJSONMember(doc, "", "content", blocks); err != nil {
					return nil, err
				}
				raw = doc.Bytes()
			} else {
				message, err := common.Marshal(dto.Message{Role: "assistant", Content: blocks})
				if err != nil {
					return nil, err
				}
				out = append(out, message)
			}
			pending = nil
		}
		out = append(out, raw)
	}
	if len(pending) > 0 {
		block, err := mistralThinkingChunk(pending)
		if err != nil {
			return nil, err
		}
		message, err := common.Marshal(dto.Message{Role: "assistant", Content: []mistralContentBlock{block}})
		if err != nil {
			return nil, err
		}
		out = append(out, message)
	}
	if !changed {
		return input, nil
	}
	return common.Marshal(out)
}

// Responses 同轮的正文、思考和工具调用必须组成一条 assistant 消息
func mergeMistralAssistantTurns(messages []dto.Message) ([]dto.Message, error) {
	out := messages[:0]
	for i := 0; i < len(messages); {
		message := messages[i]
		end := i + 1
		if message.Role == "assistant" {
			for end < len(messages) && messages[end].Role == "assistant" {
				end++
			}
		}
		if end == i+1 {
			out = append(out, message)
			i = end
			continue
		}
		var blocks []mistralContentBlock
		var calls []dto.ToolCallRequest
		for _, part := range messages[i:end] {
			content, err := mistralThinkingContent(part)
			if err != nil {
				return nil, err
			}
			switch value := content.(type) {
			case string:
				if value != "" {
					blocks = append(blocks, mistralContentBlock{Type: "text", Text: new(value)})
				}
			case []mistralContentBlock:
				blocks = append(blocks, value...)
			}
			calls = append(calls, part.ParseToolCalls()...)
		}
		message.SetNullContent()
		message.Content = blocks
		if len(calls) > 0 {
			message.SetToolCalls(calls)
		}
		out = append(out, message)
		i = end
	}
	return out, nil
}
