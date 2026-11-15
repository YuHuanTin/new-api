package mistral

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	prepared, contents, err := prepareClaudeThinkingReplay(request)
	if err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	// 共享转换只处理通用协议，Mistral 思考历史在转换后恢复
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, prepared)
	if err != nil {
		return nil, err
	}
	openAIRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	messages := openAIRequest.Messages[:0]
	assistant := 0
	for _, message := range openAIRequest.Messages {
		if message.Role == "assistant" {
			if assistant >= len(contents) {
				return nil, errors.New("mistral assistant history mismatch")
			}
			message.SetNullContent()
			message.Content = contents[assistant]
			assistant++
			if message.Content == "" && len(message.ToolCalls) == 0 {
				continue
			}
		}
		messages = append(messages, message)
	}
	if assistant != len(contents) {
		return nil, errors.New("mistral assistant history mismatch")
	}
	openAIRequest.Messages = messages
	key := resolveAffinityKey(c, metadataAffinity(request.Metadata))
	// Claude 协议没有 prompt_cache_key，用客户端会话标识作为缓存亲和键
	openAIRequest.PromptCacheKey = key
	// 再按 Mistral 的字段限制裁剪
	return a.ConvertOpenAIRequest(c, info, openAIRequest)
}

// affinityHeaders 是客户端可能携带会话标识的请求头，按优先级排列
// x-affinity 是 Mistral 官方 CLI（mistral-vibe）使用的缓存亲和头
// x-client-request-id 是 pi-ai Responses 路径承载会话 id 的头（值就是 sessionId），dsh 走 Responses 时只带它
var affinityHeaders = []string{
	"x-affinity",
	"x-session-affinity",
	"x-deepseek-harness-session-id",
	"x-claude-code-session-id",
	"session-id",
	"x-session-id",
	"x-client-request-id",
}

const affinityContextKey = "mistral_affinity_key"

// resolveAffinityKey 先取客户端的会话请求头，再按顺序取协议体里的会话字段
// 两者都没有时返回空串，网关不自行编造缓存键
func resolveAffinityKey(c *gin.Context, bodyValues ...string) string {
	if c != nil && c.Request != nil {
		for _, name := range affinityHeaders {
			if key := sanitizeAffinityKey(c.Request.Header.Get(name)); key != "" {
				return key
			}
		}
	}
	for _, value := range bodyValues {
		if key := sanitizeAffinityKey(value); key != "" {
			return key
		}
	}
	return ""
}

// maxAffinityKeyLength 对齐 OpenAI prompt_cache_key 的 64 字符上限
const maxAffinityKeyLength = 64

// sanitizeAffinityKey 去空白、丢弃含控制字符的值并截断到上游能接受的长度
// 返回值要同时进请求体（prompt_cache_key）和请求头（x-affinity），必须能安全编码
func sanitizeAffinityKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	if len(value) > maxAffinityKeyLength {
		value = strings.ToValidUTF8(value[:maxAffinityKeyLength], "")
	}
	return value
}

// rememberAffinityKey 记录本次请求的亲和键，供 SetupRequestHeader 转发成 x-affinity
func rememberAffinityKey(c *gin.Context, key string) {
	if c != nil {
		c.Set(affinityContextKey, key)
	}
}

func affinityKeyForRequest(c *gin.Context) string {
	if c != nil {
		if value, ok := c.Get(affinityContextKey); ok {
			key, _ := value.(string)
			return key
		}
	}
	return resolveAffinityKey(c)
}

// rawJSONString 把字符串类型的 JSON 字段解成原值
func rawJSONString(raw common.RawMessage) string {
	if common.GetJsonType(raw) != "string" {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

// metadataAffinity 从 metadata 里取会话标识；Claude Code 把 session_id 以 JSON 字符串塞在 user_id 里
func metadataAffinity(raw common.RawMessage) string {
	if common.GetJsonType(raw) != "object" {
		return ""
	}
	var metadata struct {
		SessionID      string `json:"session_id"`
		ConversationID string `json:"conversation_id"`
		UserID         string `json:"user_id"`
	}
	if err := common.Unmarshal(raw, &metadata); err != nil {
		return ""
	}
	if metadata.SessionID != "" {
		return metadata.SessionID
	}
	if metadata.ConversationID != "" {
		return metadata.ConversationID
	}
	var nested struct {
		SessionID string `json:"session_id"`
	}
	if err := common.UnmarshalJsonStr(metadata.UserID, &nested); err == nil {
		return nested.SessionID
	}
	return ""
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// Claude 和 Responses 请求已转为 OpenAI chat 格式，上游路径改为 /v1/chat/completions
	if info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatOpenAIResponses {
		return fmt.Sprintf("%s/v1/chat/completions", info.ChannelBaseUrl), nil
	}
	return relaycommon.GetFullRequestURL(info.ChannelBaseUrl, info.RequestURLPath, info.ChannelType), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	// 有会话标识时按 Mistral 官方 CLI 的做法带上亲和头，让上游把同一会话的请求路由到同一节点
	if key := affinityKeyForRequest(c); key != "" {
		req.Set("x-affinity", key)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	// Mistral 只接受 chat completions，无 messages 的请求（如 /v1/completions）直接拒绝
	if len(request.Messages) == 0 {
		return nil, kitreasoning.AsClientError(errors.New("mistral channel only supports chat completions requests with messages"))
	}
	prepared := *request
	// 统一解析嵌套思考字段和模型修饰符，再按模型能力折算
	if prepared.ReasoningConversion == nil && info != nil {
		prepared.ReasoningConversion = info.ReasoningState()
	}
	intent, diagnostics, err := kitreasoning.FromOpenAIChat(&prepared)
	if err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	prepared.ReasoningEffort = string(kitreasoning.EffectiveEffort(intent))
	if info != nil {
		info.RecordConversionDiagnostics(c, diagnostics)
	}
	key := resolveAffinityKey(c, request.PromptCacheKey, rawJSONString(request.User), metadataAffinity(request.Metadata))
	rememberAffinityKey(c, key)
	prepared.PromptCacheKey = key
	converted, err := requestOpenAI2Mistral(&prepared)
	if err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	if info != nil {
		info.SetReasoningEffort(converted.ReasoningEffort)
		if effort := prepared.ReasoningEffort; effort != "" && effort != converted.ReasoningEffort {
			message := fmt.Sprintf("model %q does not support reasoning_effort %q; using %q", request.Model, effort, converted.ReasoningEffort)
			if converted.ReasoningEffort == "" {
				message = fmt.Sprintf("model %q does not support reasoning_effort %q; the field was omitted", request.Model, effort)
			} else if effort == "none" {
				message = fmt.Sprintf("model %q cannot disable thinking; using its lowest reasoning effort %q", request.Model, converted.ReasoningEffort)
			}
			info.RecordConversionDiagnostics(c, []types.ConversionDiagnostic{{
				Code: "mistral_reasoning_effort_adjusted", Path: "reasoning_effort", Message: message,
				Severity: types.ConversionDiagnosticWarning, From: info.RelayFormat, To: types.RelayFormatOpenAI,
			}})
		}
	}
	return converted, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	input, err := mistralResponsesThinkingInput(request.Input)
	if err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	request.Input = input
	// Responses 请求先复用转换器转为 OpenAI chat 格式
	result, err := service.ConvertRequest(c, info, types.RelayFormatOpenAI, &request)
	if err != nil {
		return nil, err
	}
	chatRequest, ok := result.Value.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("expected OpenAI chat completions request, got %T", result.Value)
	}
	chatRequest.Messages, err = mergeMistralAssistantTurns(chatRequest.Messages)
	if err != nil {
		return nil, kitreasoning.AsClientError(err)
	}
	key := resolveAffinityKey(c, rawJSONString(request.PromptCacheKey), metadataAffinity(request.Metadata), metadataAffinity(request.ClientMetadata))
	chatRequest.PromptCacheKey = key
	// 再按 Mistral 的字段限制裁剪
	return a.ConvertOpenAIRequest(c, info, chatRequest)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	// 参数覆盖可能改写缓存键，亲和头必须采用最终正文中的值
	if info != nil && info.ChannelMeta != nil && len(info.ParamOverride) > 0 &&
		!model_setting.GetGlobalSettings().PassThroughRequestEnabled && !info.ChannelSetting.PassThroughBodyEnabled {
		var reader io.ReadCloser
		if body, ok := requestBody.(common.ReplayableBody); ok {
			var err error
			reader, err = body.NewReader()
			if err != nil {
				return nil, fmt.Errorf("read mistral final affinity key: %w", err)
			}
		} else {
			if closer, ok := requestBody.(io.Closer); ok {
				defer closer.Close()
			}
			data, err := io.ReadAll(requestBody)
			if err != nil {
				return nil, fmt.Errorf("read mistral final affinity key: %w", err)
			}
			requestBody = bytes.NewReader(data)
			reader = io.NopCloser(bytes.NewReader(data))
		}
		var final struct {
			PromptCacheKey string `json:"prompt_cache_key"`
		}
		err := common.DecodeJson(reader, &final)
		_ = reader.Close()
		if err != nil {
			return nil, fmt.Errorf("read mistral final affinity key: %w", err)
		}
		if sanitizeAffinityKey(final.PromptCacheKey) != final.PromptCacheKey {
			return nil, errors.New("invalid mistral final prompt_cache_key")
		}
		rememberAffinityKey(c, final.PromptCacheKey)
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
