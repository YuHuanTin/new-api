package mistral

import (
	"regexp"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	messages := make([]dto.Message, 0, len(request.Messages))
	idMap := make(map[string]string)
	for _, message := range request.Messages {
		// Mistral 只接受 system/user/assistant/tool 四种角色
		if message.Role == "developer" {
			message.Role = "system"
		}

		// 1. tool_calls.id
		toolCalls := message.ParseToolCalls()
		if toolCalls != nil {
			for i := range toolCalls {
				if !mistralToolCallIdRegexp.MatchString(toolCalls[i].ID) {
					if newId, ok := idMap[toolCalls[i].ID]; ok {
						toolCalls[i].ID = newId
					} else {
						newId, err := common.GenerateRandomCharsKey(9)
						if err == nil {
							idMap[toolCalls[i].ID] = newId
							toolCalls[i].ID = newId
						}
					}
				}
			}
			message.SetToolCalls(toolCalls)
		}

		// 2. tool_call_id
		if message.ToolCallId != "" {
			if newId, ok := idMap[message.ToolCallId]; ok {
				message.ToolCallId = newId
			} else {
				if !mistralToolCallIdRegexp.MatchString(message.ToolCallId) {
					newId, err := common.GenerateRandomCharsKey(9)
					if err == nil {
						idMap[message.ToolCallId] = newId
						message.ToolCallId = newId
					}
				}
			}
		}

		// 原生内容块必须在通用解析前保留，否则 thinking 会被丢弃
		if message.Role == "system" || message.Role == "assistant" {
			content, err := mistralThinkingContent(message)
			if err != nil {
				return nil, err
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
