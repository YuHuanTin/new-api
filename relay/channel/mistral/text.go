package mistral

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strconv"
	"strings"

	"github.com/MoeForge/moego/regexp"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

const mistralToolCallIDChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	messages := make([]dto.Message, 0, len(request.Messages))
	idMap := make(map[string]string)
	used := make(map[string]struct{})
	rewriteToolCallID := func(orig string) string {
		if mistralToolCallIdRegexp.MatchString(orig) {
			used[orig] = struct{}{}
			return orig
		}
		if mapped, ok := idMap[orig]; ok {
			return mapped
		}
		for attempt := 0; ; attempt++ {
			input := orig
			if attempt > 0 {
				input = orig + "#" + strconv.Itoa(attempt)
			}
			sum := sha256.Sum256([]byte(input))
			val := binary.BigEndian.Uint64(sum[:8])
			var buf [9]byte
			for i := range buf {
				buf[i] = mistralToolCallIDChars[val%62]
				val /= 62
			}
			candidate := string(buf[:])
			if _, exists := used[candidate]; !exists {
				idMap[orig] = candidate
				used[candidate] = struct{}{}
				return candidate
			}
		}
	}
	for _, message := range request.Messages {
		// Mistral 只接受 system/user/assistant/tool 四种角色
		if message.Role == "developer" {
			message.Role = "system"
		}

		// 1. tool_calls.id
		toolCalls := message.ParseToolCalls()
		if toolCalls != nil {
			for i := range toolCalls {
				toolCalls[i].ID = rewriteToolCallID(toolCalls[i].ID)
			}
			message.SetToolCalls(toolCalls)
		}

		// 2. tool_call_id
		if message.ToolCallId != "" {
			message.ToolCallId = rewriteToolCallID(message.ToolCallId)
		}

		// 原生内容块必须在通用解析前保留，否则 thinking 会被丢弃
		if message.Role == "system" || message.Role == "assistant" {
			content, err := mistralThinkingContent(message)
			if err != nil {
				return nil, err
			}
			if blocks, ok := content.([]mistralContentBlock); ok && message.Role == "assistant" && strings.HasPrefix(request.Model, "deepseek-") {
				// 托管 DS 要求思考先于正文，保留同类块的原始顺序
				slices.SortStableFunc(blocks, func(a, b mistralContentBlock) int {
					if a.Type == b.Type {
						return 0
					}
					if a.Type == "thinking" {
						return -1
					}
					return 1
				})
			}
			message.SetNullContent()
			message.Content = content
		} else {
			mediaMessages := message.ParseContent()
			for j, mediaMessage := range mediaMessages {
				if mediaMessage.Type == dto.ContentTypeImageURL {
					imageUrl := mediaMessage.GetImageMedia()
					mediaMessage.ImageUrl = imageUrl.Url
				}
				// Mistral 的内容块不接受 cache_control
				mediaMessage.CacheControl = nil
				mediaMessages[j] = mediaMessage
			}
			message.SetMediaContent(mediaMessages)
		}
		messages = append(messages, dto.Message{
			Role:       message.Role,
			Content:    message.Content,
			Prefix:     message.Prefix,
			ToolCalls:  message.ToolCalls,
			ToolCallId: message.ToolCallId,
		})
	}
	out := &dto.GeneralOpenAIRequest{
		Model:          request.Model,
		Stream:         request.Stream,
		Messages:       messages,
		Temperature:    request.Temperature,
		TopP:           request.TopP,
		Tools:          request.Tools,
		ToolChoice:     request.ToolChoice,
		ResponseFormat: request.ResponseFormat,
		PromptCacheKey: request.PromptCacheKey,
		// 推理强度必须转发，否则客户端设置的 effort 会被静默丢弃
		ReasoningEffort: mistralReasoningEffort(request.Model, request.ReasoningEffort),
	}
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil {
		maxTokens := request.GetMaxTokens()
		out.MaxTokens = &maxTokens
	}
	return out, nil
}
