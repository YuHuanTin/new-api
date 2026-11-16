package mistral

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	kitreasoning "github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertClaudeRequestReturnsMistralChatRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeMistral,
			ChannelBaseUrl:    "https://api.mistral.ai",
			UpstreamModelName: "mistral-large-latest",
		},
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "mistral-large-latest",
	}
	maxTokens := uint(128)
	converted, err := (&Adaptor{}).ConvertClaudeRequest(c, info, &dto.ClaudeRequest{
		Model:     "mistral-large-latest",
		MaxTokens: &maxTokens,
		Messages: []dto.ClaudeMessage{
			{Role: "user", Content: "hi"},
		},
	})
	require.NoError(t, err)

	openAIRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, "mistral-large-latest", openAIRequest.Model)
	require.NotNil(t, openAIRequest.MaxTokens)
	require.Equal(t, uint(128), *openAIRequest.MaxTokens)
	require.Len(t, openAIRequest.Messages, 1)
	require.Equal(t, "user", openAIRequest.Messages[0].Role)
	contents := openAIRequest.Messages[0].ParseContent()
	require.Len(t, contents, 1)
	require.Equal(t, "hi", contents[0].Text)
}

func TestRequestOpenAI2MistralSanitizesContent(t *testing.T) {
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{
		Model: "mistral-large-latest",
		Messages: []dto.Message{
			{Role: "system", Content: []dto.MediaContent{
				{Type: dto.ContentTypeText, Text: "a"},
				{Type: dto.ContentTypeText, Text: "b"},
			}},
			{Role: "assistant", Content: []dto.MediaContent{
				{Type: dto.ContentTypeText, Text: "c"},
			}},
			{Role: "user", Content: []dto.MediaContent{
				{Type: dto.ContentTypeText, Text: "d", CacheControl: []byte(`{"type":"ephemeral"}`)},
			}},
		},
	})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 3)
	require.Equal(t, "ab", converted.Messages[0].Content)
	require.Equal(t, "c", converted.Messages[1].Content)
	userContents := converted.Messages[2].ParseContent()
	require.Len(t, userContents, 1)
	require.Nil(t, userContents[0].CacheControl)
}

func TestConvertOpenAIResponsesRequestReturnsMistralChatRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeMistral,
			ChannelBaseUrl:    "https://api.mistral.ai",
			UpstreamModelName: "mistral-large-latest",
		},
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "mistral-large-latest",
	}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
		Model: "mistral-large-latest",
		Input: []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
		Tools: []byte(`[{"type":"function","name":"exec_command","description":"run","parameters":{"type":"object","properties":{"cmd":{"type":"string"}}}}]`),
	})
	require.NoError(t, err)

	chatRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, "mistral-large-latest", chatRequest.Model)
	require.Len(t, chatRequest.Messages, 1)
	require.Equal(t, "user", chatRequest.Messages[0].Role)
	require.Len(t, chatRequest.Tools, 1)
	require.Equal(t, "exec_command", chatRequest.Tools[0].Function.Name)
}

func TestConvertOpenAIRequestRejectsRequestWithoutMessages(t *testing.T) {
	_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Model:  "mistral-large-latest",
		Prompt: "hi",
	})
	require.Error(t, err)
}

// Mistral 要求 tool call id 为 9 位字母数字，改写后 tool 消息必须引用同一个新 id
func TestRequestOpenAI2MistralRewritesToolCallId(t *testing.T) {
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{
			Role:      "assistant",
			ToolCalls: []byte(`[{"id":"chatcmpl-tool-abc","type":"function","function":{"name":"Read","arguments":"{}"}}]`),
		},
		{Role: "tool", ToolCallId: "chatcmpl-tool-abc", Content: "ok"},
	}})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)

	newId := converted.Messages[0].ParseToolCalls()[0].ID
	require.Regexp(t, mistralToolCallIdRegexp, newId)
	require.Equal(t, newId, converted.Messages[1].ToolCallId)
}

func TestRequestOpenAI2MistralForwardsPromptCacheKey(t *testing.T) {
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{
		Model:          "mistral-large-latest",
		PromptCacheKey: "session-key",
		Messages:       []dto.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "session-key", converted.PromptCacheKey)
}

func TestConvertClaudeRequestUsesSessionIdAsPromptCacheKey(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeMistral,
			ChannelBaseUrl:    "https://api.mistral.ai",
			UpstreamModelName: "mistral-large-latest",
		},
		RelayFormat:     types.RelayFormatClaude,
		OriginModelName: "mistral-large-latest",
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request.Header.Set("x-claude-code-session-id", "session-abc")
	converted, err := (&Adaptor{}).ConvertClaudeRequest(c, info, &dto.ClaudeRequest{
		Model:    "mistral-large-latest",
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "session-abc", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)

	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	converted, err = (&Adaptor{}).ConvertClaudeRequest(c, info, &dto.ClaudeRequest{
		Model:    "mistral-large-latest",
		Metadata: []byte(`{"user_id":"{\"device_id\":\"dev\",\"session_id\":\"session-metadata\"}"}`),
		Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "session-metadata", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)
}

func TestGetRequestURLForClaudeUsesChatCompletions(t *testing.T) {
	adaptor := &Adaptor{}

	url, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeMistral,
			ChannelBaseUrl: "https://api.mistral.ai",
		},
		RelayFormat:    types.RelayFormatClaude,
		RequestURLPath: "/v1/messages?beta=true",
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.mistral.ai/v1/chat/completions", url)

	url, err = adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeMistral,
			ChannelBaseUrl: "https://api.mistral.ai",
		},
		RelayFormat:    types.RelayFormatOpenAI,
		RequestURLPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.mistral.ai/v1/chat/completions", url)

	url, err = adaptor.GetRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeMistral,
			ChannelBaseUrl: "https://api.mistral.ai",
		},
		RelayFormat:    types.RelayFormatOpenAIResponses,
		RequestURLPath: "/v1/responses",
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.mistral.ai/v1/chat/completions", url)
}

func TestConvertOpenAIRequestUsesClientAffinityHeaderAsPromptCacheKey(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral}}
	build := func(t *testing.T, header, value string, request *dto.GeneralOpenAIRequest) *dto.GeneralOpenAIRequest {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		if header != "" {
			c.Request.Header.Set(header, value)
		}
		request.Model = "mistral-large-latest"
		converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
		require.NoError(t, err)
		return converted.(*dto.GeneralOpenAIRequest)
	}
	message := func() []dto.Message {
		return []dto.Message{{Role: "system", Content: "you are a helper"}, {Role: "user", Content: "hi"}}
	}

	require.Equal(t, "session-abc", build(t, "x-affinity", "session-abc", &dto.GeneralOpenAIRequest{Messages: message()}).PromptCacheKey)
	require.Equal(t, "session-def", build(t, "x-deepseek-harness-session-id", "session-def", &dto.GeneralOpenAIRequest{Messages: message()}).PromptCacheKey)
	require.Equal(t, "session-ghi", build(t, "session-id", "session-ghi", &dto.GeneralOpenAIRequest{Messages: message()}).PromptCacheKey)
	// pi-ai 的 Responses 路径用 x-client-request-id 承载 sessionId
	require.Equal(t, "session-jkl", build(t, "x-client-request-id", "session-jkl", &dto.GeneralOpenAIRequest{Messages: message()}).PromptCacheKey)
	require.Empty(t, build(t, "", "", &dto.GeneralOpenAIRequest{Messages: message()}).PromptCacheKey)

	// 协议体里的会话字段：x-affinity 缺席时退到请求头之外的来源
	require.Equal(t, "body-user", build(t, "", "", &dto.GeneralOpenAIRequest{Messages: message(), User: []byte(`"body-user"`)}).PromptCacheKey)
	require.Equal(t, "body-metadata", build(t, "", "", &dto.GeneralOpenAIRequest{Messages: message(), Metadata: []byte(`{"session_id":"body-metadata"}`)}).PromptCacheKey)
	// 请求头优先于协议体
	require.Equal(t, "header-wins", build(t, "x-affinity", "header-wins", &dto.GeneralOpenAIRequest{Messages: message(), User: []byte(`"body-user"`)}).PromptCacheKey)
}

func TestConvertOpenAIResponsesRequestUsesClientAffinity(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("x-session-affinity", "responses-session")
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral},
		RelayFormat: types.RelayFormatOpenAIResponses,
	}, dto.OpenAIResponsesRequest{
		Model: "mistral-large-latest",
		Input: []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
	})
	require.NoError(t, err)
	require.Equal(t, "responses-session", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)
}

// 转换阶段记下的会话标识要能被随后的上游请求头复用
func TestConvertThenSetupRequestHeaderReusesBodyAffinity(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	_, err := (&Adaptor{}).ConvertOpenAIRequest(c, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Model:    "mistral-large-latest",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
		User:     []byte(`"body-user"`),
	})
	require.NoError(t, err)
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "key"}}))
	require.Equal(t, "body-user", header.Get("x-affinity"))
}

func TestConvertOpenAIRequestKeepsClientPromptCacheKey(t *testing.T) {
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Model:          "mistral-large-latest",
		PromptCacheKey: "client-key",
		Messages:       []dto.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "client-key", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)
}

func TestSetupRequestHeaderForwardsAffinityHeader(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Header.Set("x-affinity", "session-abc")
	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "key"}}))
	require.Equal(t, "session-abc", header.Get("x-affinity"))

	plain, _ := gin.CreateTestContext(httptest.NewRecorder())
	plain.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	header = http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(plain, &header, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "key"}}))
	require.Empty(t, header.Get("x-affinity"))
}

func TestResolveAffinityKeySanitizesValues(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request.Header.Set("x-session-affinity", "  padded-key  ")
	require.Equal(t, "padded-key", resolveAffinityKey(c))

	c.Request.Header.Set("x-session-affinity", "bad\nkey")
	require.Empty(t, resolveAffinityKey(c), "控制字符不能进请求头")

	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	c.Request.Header.Set("x-session-affinity", string(long))
	require.Len(t, resolveAffinityKey(c), maxAffinityKeyLength)
}

func TestConvertOpenAIResponsesRequestKeepsBodyPromptCacheKey(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral},
		RelayFormat: types.RelayFormatOpenAIResponses,
	}, dto.OpenAIResponsesRequest{
		Model:          "mistral-large-latest",
		Input:          []byte(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]`),
		PromptCacheKey: []byte(`"pi-ai-session"`),
	})
	require.NoError(t, err)
	require.Equal(t, "pi-ai-session", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)
}

// pi-ai 在 cacheRetention=long 时把 sessionId 写进 chat 请求体的 prompt_cache_key
func TestConvertOpenAIRequestUsesBodyPromptCacheKey(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, &relaycommon.RelayInfo{}, &dto.GeneralOpenAIRequest{
		Model:          "mistral-large-latest",
		Messages:       []dto.Message{{Role: "user", Content: "hi"}},
		PromptCacheKey: "pi-ai-session",
	})
	require.NoError(t, err)
	require.Equal(t, "pi-ai-session", converted.(*dto.GeneralOpenAIRequest).PromptCacheKey)

	header := http.Header{}
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "key"}}))
	require.Equal(t, "pi-ai-session", header.Get("x-affinity"))
}

// Mistral 只接受 system/user/assistant/tool，developer 角色必须映射成 system
func TestRequestOpenAI2MistralMapsDeveloperRole(t *testing.T) {
	converted, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "developer", Content: "rules"},
		{Role: "user", Content: "hi"},
	}})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	require.Equal(t, "system", converted.Messages[0].Role)
	require.Equal(t, "rules", converted.Messages[0].Content)
	require.Equal(t, "user", converted.Messages[1].Role)
}

// reasoning_effort 必须透传到上游，模型不收的档位就近折算
func TestRequestOpenAI2MistralForwardsReasoningEffort(t *testing.T) {
	inputs := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	for _, tt := range []struct {
		model string
		want  [7]string
	}{
		{model: "deepseek-v4-1-flash-1017", want: [7]string{"none", "low", "low", "high", "high", "xhigh", "max"}},
		{model: "zai-glm-5-3", want: [7]string{"low", "low", "low", "high", "high", "max", "max"}},
		{model: "zai-glm-5", want: [7]string{"low", "low", "low", "high", "high", "max", "max"}},
		{model: "zai-glm-latest", want: [7]string{"low", "low", "low", "high", "high", "max", "max"}},
		{model: "zai-glm-5-3-flash", want: [7]string{"low", "low", "low", "high", "high", "max", "max"}},
		{model: "mistral-large-latest", want: [7]string{"low", "low", "low", "medium", "high", "high", "high"}},
		{model: "mistral-large-4-0", want: [7]string{"low", "low", "low", "medium", "high", "high", "high"}},
		{model: "mistral-large-2512"},
		{model: "mistral-small-latest", want: [7]string{"none", "none", "high", "high", "high", "high", "high"}},
		{model: "mistral-medium-3-5", want: [7]string{"none", "none", "high", "high", "high", "high", "high"}},
		{model: "mistral-medium-3", want: [7]string{"none", "none", "high", "high", "high", "high", "high"}},
		{model: "custom-model", want: [7]string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}},
	} {
		for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
			for i, effort := range inputs {
				t.Run(tt.model+"/"+string(format)+"/"+effort, func(t *testing.T) {
					var body string
					switch format {
					case types.RelayFormatClaude:
						body = fmt.Sprintf(`{"model":%q,"max_tokens":128,"output_config":{"effort":%q},"messages":[{"role":"user","content":"hi"}]}`, tt.model, effort)
					case types.RelayFormatOpenAIResponses:
						body = fmt.Sprintf(`{"model":%q,"reasoning":{"effort":%q},"input":"hi"}`, tt.model, effort)
					default:
						body = fmt.Sprintf(`{"model":%q,"reasoning_effort":%q,"messages":[{"role":"user","content":"hi"}]}`, tt.model, effort)
					}
					converted, info := convertMistralRequestForTest(t, format, body)
					assert.Equal(t, tt.want[i], converted.ReasoningEffort)
					assert.Equal(t, tt.want[i], info.ReasoningEffort)
					if effort != tt.want[i] {
						assert.NotEmpty(t, info.ConversionDiagnostics(), "折算必须记录诊断")
					}
				})
			}
		}
	}
}

func TestMistralChatReasoningControls(t *testing.T) {
	for _, tc := range []struct {
		name, model, fields, want string
	}{
		{name: "nested_effort", fields: `,"reasoning":{"effort":"high"}`, want: "high"},
		{name: "nested_enabled", fields: `,"reasoning":{"enabled":true}`, want: "high"},
		{name: "nested_disabled", fields: `,"reasoning":{"enabled":false}`, want: "none"},
		{name: "top_level_wins", fields: `,"reasoning_effort":"high","reasoning":{"enabled":false}`, want: "high"},
		{name: "thinking_on", model: "mistral-small-latest@thinking:on", want: "high"},
		{name: "thinking_off", model: "mistral-small-latest@thinking:off", fields: `,"reasoning":{"effort":"high"}`, want: "none"},
		{name: "budget_modifier", model: "mistral-small-latest@thinking:2048", want: "high"},
		{name: "large_cannot_disable", model: "mistral-large-latest", fields: `,"reasoning":{"enabled":false}`, want: "low"},
		{name: "absent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := tc.model
			if model == "" {
				model = "mistral-small-latest"
			}
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"q"}]%s}`, model, tc.fields), &request))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Request.Header.Set("x-affinity", "session-key")
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral, UpstreamModelName: model},
				RelayFormat: types.RelayFormatOpenAI, OriginModelName: model, Request: &request,
			}
			require.NoError(t, helper.ApplyReasoningModelSuffix(c, info, &request))
			before, err := common.Marshal(request)
			require.NoError(t, err)
			value, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &request)
			require.NoError(t, err)
			converted := value.(*dto.GeneralOpenAIRequest)
			assert.Equal(t, tc.want, converted.ReasoningEffort)
			assert.Equal(t, tc.want, info.ReasoningEffort)
			assert.Equal(t, "session-key", converted.PromptCacheKey)
			after, err := common.Marshal(request)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after))
		})
	}
	_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "q"}}, Reasoning: []byte(`{"effort":"invalid"}`),
	})
	require.Error(t, err)
	assert.True(t, kitreasoning.IsClientError(err))
}

func TestMistralAffinityAfterParamOverride(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name, want string
		override   map[string]any
		headers    map[string]any
		plainBody  bool
	}{
		{name: "replace", override: map[string]any{"prompt_cache_key": "admin-key"}, want: "admin-key"},
		{name: "operation", override: map[string]any{"operations": []any{map[string]any{"mode": "set", "path": "prompt_cache_key", "value": "operation-key"}}}, want: "operation-key"},
		{name: "remove", override: map[string]any{"prompt_cache_key": nil}},
		{name: "unrelated", override: map[string]any{"temperature": 0}, want: "header-key"},
		{name: "plain_reader", override: map[string]any{"prompt_cache_key": "admin-key"}, want: "admin-key", plainBody: true},
		{name: "explicit_header", override: map[string]any{"prompt_cache_key": "admin-key"}, headers: map[string]any{"x-affinity": "manual-key"}, want: "manual-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type capture struct {
				body []byte
				key  string
				err  error
			}
			received := make(chan capture, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- capture{body: body, key: r.Header.Get("x-affinity"), err: err}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(upstream.Close)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Request.Header.Set("x-affinity", "header-key")
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeMistral, ChannelBaseUrl: upstream.URL,
				ParamOverride: tc.override, HeadersOverride: tc.headers,
			}, RelayFormat: types.RelayFormatOpenAI, RequestURLPath: "/v1/chat/completions"}
			adaptor := &Adaptor{}
			value, err := adaptor.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{
				Model: "mistral-large-latest", Messages: []dto.Message{{Role: "user", Content: "q"}}, PromptCacheKey: "body-key",
			})
			require.NoError(t, err)
			payload, err := common.Marshal(value)
			require.NoError(t, err)
			payload, err = relaycommon.ApplyParamOverrideWithRelayInfo(payload, info)
			require.NoError(t, err)
			replayable, closer, err := relaycommon.NewOutboundJSONBody(payload)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			var body io.Reader = replayable
			if tc.plainBody {
				body = bytes.NewReader(payload)
			}
			response, err := adaptor.DoRequest(c, info, body)
			require.NoError(t, err)
			require.NoError(t, response.(*http.Response).Body.Close())
			got := <-received
			require.NoError(t, got.err)
			assert.Equal(t, tc.want, got.key)
			assert.Equal(t, payload, got.body)
			reader, err := replayable.NewReader()
			require.NoError(t, err)
			replayed, err := io.ReadAll(reader)
			require.NoError(t, err)
			require.NoError(t, reader.Close())
			assert.Equal(t, payload, replayed)
		})
	}
}

// 思考分片必须改写成 reasoning_content，否则通用流式解析会失败
func TestRewriteReasoningChunkSplitsThinking(t *testing.T) {
	line := []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"index\":0,\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"The\"}],\"closed\":true}]},\"finish_reason\":null}]}\n")
	rewritten := rewriteReasoningChunk(line)
	require.Contains(t, string(rewritten), `"reasoning_content":"The"`)
	require.Contains(t, string(rewritten), `"content":""`)
	require.NotContains(t, string(rewritten), `"type":"thinking"`)

	var chunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.UnmarshalJsonStr(strings.TrimSuffix(strings.TrimPrefix(string(rewritten), "data: "), "\n"), &chunk))
	require.Equal(t, "The", chunk.Choices[0].Delta.GetReasoningContent())
	require.Equal(t, "", chunk.Choices[0].Delta.GetContentString())
}

// 思考块与正文块同片时，正文写 content，思考写 reasoning_content
func TestRewriteReasoningChunkKeepsText(t *testing.T) {
	line := []byte("data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"thinking\",\"thinking\":[{\"type\":\"text\",\"text\":\"plan\"}]},{\"type\":\"text\",\"text\":\"hello\"}]}}]}\n")
	rewritten := rewriteReasoningChunk(line)
	require.Contains(t, string(rewritten), `"content":"hello"`)
	require.Contains(t, string(rewritten), `"reasoning_content":"plan"`)
}

// 普通文本分片和非 data 行必须原样返回
func TestRewriteReasoningChunkLeavesNormalLines(t *testing.T) {
	for _, line := range [][]byte{
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n"),
		[]byte("data: [DONE]\n"),
		[]byte("\n"),
		[]byte(": ping\n"),
	} {
		require.Equal(t, line, rewriteReasoningChunk(line))
	}
}

type mistralTestBody struct {
	io.ReadCloser
	readStarted chan struct{}
	readOnce    sync.Once
	closed      atomic.Bool
}

func (r *mistralTestBody) Read(p []byte) (int, error) {
	if r.readStarted != nil {
		r.readOnce.Do(func() { close(r.readStarted) })
	}
	return r.ReadCloser.Read(p)
}

func (r *mistralTestBody) Close() error {
	r.closed.Store(true)
	return r.ReadCloser.Close()
}

func newMistralResponseTestContext(t *testing.T, body string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo, *mistralTestBody) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "mistral-test")
	source := &mistralTestBody{ReadCloser: io.NopCloser(strings.NewReader(body))}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: source}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral, UpstreamModelName: "mistral-large-latest"},
		RelayFormat: types.RelayFormatOpenAIResponses,
		IsStream:    stream,
		DisablePing: true,
	}
	info.SetEstimatePromptTokens(10)
	return c, recorder, resp, info, source
}

func TestMistralResponseClosesOriginalBody(t *testing.T) {
	for _, body := range []string{
		"data: {\"id\":\"chat_1\",\"model\":\"mistral-large-latest\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\ndata: [DONE]\n\n",
		"data: {\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\"}}\n\n",
	} {
		c, _, resp, info, source := newMistralResponseTestContext(t, body, true)
		(&Adaptor{}).DoResponse(c, resp, info)
		assert.True(t, source.closed.Load(), "上游响应体必须关闭")
	}
}

func TestMistralClientCancelClosesBlockedStream(t *testing.T) {
	c, _, resp, info, source := newMistralResponseTestContext(t, "", true)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	reader, writer := io.Pipe()
	source.ReadCloser = reader
	source.readStarted = make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() {
		reader.Close()
		writer.Close()
		<-done
	})
	go func() {
		(&Adaptor{}).DoResponse(c, resp, info)
		close(done)
	}()
	select {
	case <-source.readStarted:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "上游读取未开始")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "客户端断开后流未退出")
	}
	assert.True(t, source.closed.Load())
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}

func TestMistralNonStreamPreservesThinkingAndUsage(t *testing.T) {
	const body = `{"id":"chat_1","model":"mistral-large-latest","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"review-plan"}]},{"type":"text","text":"hello"}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"prompt_tokens_details":{"cached_tokens":7}}}`
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		t.Run(string(format), func(t *testing.T) {
			c, recorder, resp, info, source := newMistralResponseTestContext(t, body, false)
			info.RelayFormat = format
			value, err := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, err)
			usage, ok := value.(*dto.Usage)
			require.True(t, ok)
			assert.Equal(t, 10, usage.PromptTokens)
			assert.Equal(t, 20, usage.CompletionTokens)
			assert.Equal(t, 30, usage.TotalTokens)
			billable := usage
			if usage.BillingUsage != nil {
				billable, ok = usage.BillingUsage.CanonicalUsage()
				require.True(t, ok)
			}
			assert.Equal(t, 7, billable.PromptTokensDetails.CachedTokens)
			assert.Contains(t, recorder.Body.String(), "hello")
			assert.Contains(t, recorder.Body.String(), "review-plan")
			assert.True(t, source.closed.Load())
		})
	}
	// 无上游用量时仍保留正文和思考
	c, recorder, resp, info, _ := newMistralResponseTestContext(t, `{"choices":[{"message":{"content":[{"type":"thinking","thinking":[{"type":"text","text":"review-plan"}]},{"type":"text","text":"hello"}]}}]}`, false)
	value, err := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, err)
	assert.Equal(t, 10, value.(*dto.Usage).PromptTokens)
	assert.Contains(t, recorder.Body.String(), "hello")
	assert.Contains(t, recorder.Body.String(), "review-plan")
}

func TestMistralResponsesPreservesJSONFormat(t *testing.T) {
	for _, format := range []string{`{"type":"json_object"}`, `{"type":"json_schema","name":"answer","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}},"strict":true}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral}, RelayFormat: types.RelayFormatOpenAIResponses}
		value, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
			Model: "mistral-large-latest", Input: []byte(`"return JSON"`), Text: []byte(`{"format":` + format + `}`),
		})
		require.NoError(t, err)
		responseFormat := value.(*dto.GeneralOpenAIRequest).ResponseFormat
		require.NotNil(t, responseFormat)
		if strings.Contains(format, "json_schema") {
			assert.Equal(t, "json_schema", responseFormat.Type)
			var schema dto.FormatJsonSchema
			require.NoError(t, common.Unmarshal(responseFormat.JsonSchema, &schema))
			assert.Equal(t, "answer", schema.Name)
			assert.JSONEq(t, `true`, string(schema.Strict))
			assert.Equal(t, map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}}, schema.Schema)
		} else {
			assert.Equal(t, "json_object", responseFormat.Type)
		}
	}
}

func TestMistralSSEContentArrays(t *testing.T) {
	for name, line := range map[string]string{
		"data_without_space":  "data:{\"choices\":[{\"index\":0,\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}]}\n",
		"second_choice_array": "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}},{\"index\":1,\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"other\"}]}}]}\r\n",
		"escaped_content":     "data: {\"choices\":[{\"index\":0,\"delta\":{\"\\u0063ontent\":[{\"type\":\"text\",\"text\":\"hello\"}]}}]}\n",
	} {
		t.Run(name, func(t *testing.T) {
			rewritten := rewriteReasoningChunk([]byte(line))
			payload := strings.TrimSpace(strings.TrimPrefix(string(rewritten), "data:"))
			var result dto.ChatCompletionsStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(payload, &result))
			assert.Equal(t, "hello", result.Choices[0].Delta.GetContentString())
			if len(result.Choices) > 1 {
				assert.Equal(t, "other", result.Choices[1].Delta.GetContentString())
			}
		})
	}
}

func TestMistralAffinityUsesOneKey(t *testing.T) {
	for _, tc := range []struct{ name, header, body, want string }{
		{name: "trim", body: "  session-key  ", want: "session-key"},
		{name: "length", body: strings.Repeat("a", 70), want: strings.Repeat("a", 64)},
		{name: "header_wins", header: "header-key", body: "body-key", want: "header-key"},
		{name: "invalid_body", body: "bad\nkey"},
		{name: "invalid_header", header: "bad\nkey", body: "body-key", want: "body-key"},
		{name: "invalid_utf8_header", header: string([]byte{'a', 0xff}), body: "body-key", want: "body-key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, responses := range []bool{false, true} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				c.Request.Header.Set("x-affinity", tc.header)
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral}}
				var value any
				var err error
				if responses {
					key, marshalErr := common.Marshal(tc.body)
					require.NoError(t, marshalErr)
					value, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{Model: "mistral-large-latest", Input: []byte(`"hi"`), PromptCacheKey: key})
				} else {
					value, err = (&Adaptor{}).ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: "mistral-large-latest", Messages: []dto.Message{{Role: "user", Content: "hi"}}, PromptCacheKey: tc.body})
				}
				require.NoError(t, err)
				header := http.Header{}
				require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
				assert.Equal(t, tc.want, value.(*dto.GeneralOpenAIRequest).PromptCacheKey)
				assert.Equal(t, tc.want, header.Get("x-affinity"))
			}
		})
	}
}

func TestMistralStreamEOFAndLineLimit(t *testing.T) {
	const payload = `{"id":"chat_1","model":"mistral-large-latest","choices":[{"index":0,"delta":{"content":[{"type":"text","text":"hello"}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
	t.Run("eof_without_newline", func(t *testing.T) {
		c, recorder, resp, info, source := newMistralResponseTestContext(t, "data:"+payload, true)
		value, err := (&Adaptor{}).DoResponse(c, resp, info)
		require.Nil(t, err)
		assert.Equal(t, 12, value.(*dto.Usage).TotalTokens)
		assert.Contains(t, recorder.Body.String(), "hello")
		assert.True(t, source.closed.Load())
		assert.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason)
	})
	t.Run("raw_line_limit", func(t *testing.T) {
		oldLimit := constant.StreamScannerMaxBufferMB
		constant.StreamScannerMaxBufferMB = 1
		t.Cleanup(func() { constant.StreamScannerMaxBufferMB = oldLimit })
		// 改写前也必须限制原始分片长度
		c, _, resp, info, source := newMistralResponseTestContext(t, "data:"+strings.Repeat(" ", 1<<20)+payload+"\n", true)
		(&Adaptor{}).DoResponse(c, resp, info)
		assert.Equal(t, relaycommon.StreamEndReasonScannerErr, info.StreamStatus.EndReason)
		assert.True(t, source.closed.Load())
	})
}

func convertMistralRequestForTest(t *testing.T, format types.RelayFormat, body string) (*dto.GeneralOpenAIRequest, *relaycommon.RelayInfo) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral, ChannelBaseUrl: "https://api.mistral.ai"},
		RelayFormat: format,
	}
	var request any
	switch format {
	case types.RelayFormatClaude:
		request = &dto.ClaudeRequest{}
	case types.RelayFormatOpenAIResponses:
		request = &dto.OpenAIResponsesRequest{}
	default:
		request = &dto.GeneralOpenAIRequest{}
	}
	require.NoError(t, common.UnmarshalJsonStr(body, request))
	original, err := common.Marshal(request)
	require.NoError(t, err)
	adaptor := &Adaptor{}
	var converted any
	switch value := request.(type) {
	case *dto.ClaudeRequest:
		converted, err = adaptor.ConvertClaudeRequest(c, info, value)
	case *dto.OpenAIResponsesRequest:
		converted, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *value)
	case *dto.GeneralOpenAIRequest:
		converted, err = adaptor.ConvertOpenAIRequest(c, info, value)
	}
	require.NoError(t, err)
	after, err := common.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, string(original), string(after), "思考转换不能修改原始历史")
	chat, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	return chat, info
}

func TestMistralThinkingReplayAcrossProtocols(t *testing.T) {
	for _, tc := range []struct {
		format types.RelayFormat
		body   string
	}{
		{format: types.RelayFormatOpenAI, body: `{"model":"mistral-large-latest","messages":[{"role":"user","content":"q"},{"role":"assistant","reasoning_content":"plan","content":"hello","tool_calls":[{"id":"abcdef123","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"abcdef123","content":"result"},{"role":"assistant","reasoning":"finish-plan","content":"answer"},{"role":"user","content":"next"}]}`},
		{format: types.RelayFormatClaude, body: `{"model":"mistral-large-latest","max_tokens":128,"messages":[{"role":"user","content":"q"},{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"claude-signature"},{"type":"text","text":"hello"},{"type":"tool_use","id":"abcdef123","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"abcdef123","content":"result"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"finish-plan"},{"type":"text","text":"answer"}]},{"role":"user","content":"next"}]}`},
		{format: types.RelayFormatOpenAIResponses, body: `{"model":"mistral-large-latest","input":[{"role":"user","content":"q"},{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","call_id":"abcdef123","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"abcdef123","output":"result"},{"type":"reasoning","content":[{"type":"reasoning_text","text":"finish-plan"}],"summary":[{"type":"summary_text","text":"summary-must-not-repeat"}]},{"type":"message","role":"assistant","content":"answer"},{"role":"user","content":"next"}]}`},
		{format: types.RelayFormatOpenAIResponses, body: `{"model":"mistral-large-latest","input":[{"role":"user","content":"q"},{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},{"type":"function_call","call_id":"abcdef123","name":"lookup","arguments":"{}"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call_output","call_id":"abcdef123","output":"result"},{"type":"reasoning","content":[{"type":"reasoning_text","text":"finish-plan"}]},{"type":"message","role":"assistant","content":"answer"},{"role":"user","content":"next"}]}`},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			converted, _ := convertMistralRequestForTest(t, tc.format, tc.body)
			require.Len(t, converted.Messages, 5)
			for i, role := range []string{"user", "assistant", "tool", "assistant", "user"} {
				assert.Equal(t, role, converted.Messages[i].Role)
			}
			first, err := common.Marshal(converted.Messages[1].Content)
			require.NoError(t, err)
			assert.JSONEq(t, `[{"type":"thinking","thinking":[{"type":"text","text":"plan"}]},{"type":"text","text":"hello"}]`, string(first))
			second, err := common.Marshal(converted.Messages[3].Content)
			require.NoError(t, err)
			assert.JSONEq(t, `[{"type":"thinking","thinking":[{"type":"text","text":"finish-plan"}]},{"type":"text","text":"answer"}]`, string(second))
			calls := converted.Messages[1].ParseToolCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "lookup", calls[0].Function.Name)
			assert.Equal(t, calls[0].ID, converted.Messages[2].ToolCallId)
			wire, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.NotContains(t, string(wire), "reasoning_content")
			assert.NotContains(t, string(wire), "claude-signature")
		})
	}
}

func TestMistralThinkingOrderAcrossProtocols(t *testing.T) {
	for _, tc := range []struct {
		format types.RelayFormat
		body   string
		before string
		after  string
	}{
		{
			format: types.RelayFormatOpenAI,
			body:   `{"model":%q,"messages":[{"role":"user","content":"q"},{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"thinking","thinking":[{"type":"text","text":"plan"}],"signature":"native-signature","closed":true},{"type":"text","text":"after"},{"type":"thinking","thinking":[{"type":"text","text":"finish"}],"closed":false}],"tool_calls":[{"id":"abcdef123","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"abcdef123","content":"result"}]}`,
			before: `[{"type":"text","text":"hello"},{"type":"thinking","thinking":[{"type":"text","text":"plan"}],"signature":"native-signature","closed":true},{"type":"text","text":"after"},{"type":"thinking","thinking":[{"type":"text","text":"finish"}],"closed":false}]`,
			after:  `[{"type":"thinking","thinking":[{"type":"text","text":"plan"}],"signature":"native-signature","closed":true},{"type":"thinking","thinking":[{"type":"text","text":"finish"}],"closed":false},{"type":"text","text":"hello"},{"type":"text","text":"after"}]`,
		},
		{
			format: types.RelayFormatClaude,
			body:   `{"model":%q,"max_tokens":128,"messages":[{"role":"user","content":"q"},{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"thinking","thinking":"plan"},{"type":"text","text":"after"},{"type":"thinking","thinking":"finish"},{"type":"tool_use","id":"abcdef123","name":"lookup","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"abcdef123","content":"result"}]}]}`,
			before: `[{"type":"text","text":"hello"},{"type":"thinking","thinking":[{"type":"text","text":"plan"}]},{"type":"text","text":"after"},{"type":"thinking","thinking":[{"type":"text","text":"finish"}]}]`,
			after:  `[{"type":"thinking","thinking":[{"type":"text","text":"plan"}]},{"type":"thinking","thinking":[{"type":"text","text":"finish"}]},{"type":"text","text":"hello"},{"type":"text","text":"after"}]`,
		},
		{
			format: types.RelayFormatOpenAIResponses,
			body:   `{"model":%q,"input":[{"role":"user","content":"q"},{"role":"assistant","content":"hello"},{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},{"role":"assistant","content":"after"},{"type":"reasoning","content":[{"type":"reasoning_text","text":"finish"}]},{"type":"function_call","call_id":"abcdef123","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"abcdef123","output":"result"}]}`,
			before: `[{"type":"text","text":"hello"},{"type":"thinking","thinking":[{"type":"text","text":"plan"}]},{"type":"text","text":"after"},{"type":"thinking","thinking":[{"type":"text","text":"finish"}]}]`,
			after:  `[{"type":"thinking","thinking":[{"type":"text","text":"plan"}]},{"type":"thinking","thinking":[{"type":"text","text":"finish"}]},{"type":"text","text":"hello"},{"type":"text","text":"after"}]`,
		},
	} {
		for _, model := range []struct {
			name          string
			thinkingFirst bool
		}{
			{name: "deepseek-v4-1-flash-1017", thinkingFirst: true},
			{name: "deepseek-chat", thinkingFirst: true},
			{name: "deepseek"},
			{name: "mistral-small-latest"},
			{name: "mistral-large-latest"},
		} {
			t.Run(model.name+"/"+string(tc.format), func(t *testing.T) {
				converted, _ := convertMistralRequestForTest(t, tc.format, fmt.Sprintf(tc.body, model.name))
				require.Len(t, converted.Messages, 3)
				content, err := common.Marshal(converted.Messages[1].Content)
				require.NoError(t, err)
				want := tc.before
				if model.thinkingFirst {
					want = tc.after
				}
				assert.JSONEq(t, want, string(content))
				calls := converted.Messages[1].ParseToolCalls()
				require.Len(t, calls, 1)
				assert.Equal(t, "abcdef123", calls[0].ID)
				assert.Equal(t, calls[0].ID, converted.Messages[2].ToolCallId)
				assert.Equal(t, "lookup", calls[0].Function.Name)
				assert.Equal(t, "{}", calls[0].Function.Arguments)
			})
		}
	}
}

func TestMistralNativeThinkingReplay(t *testing.T) {
	converted, _ := convertMistralRequestForTest(t, types.RelayFormatOpenAI, `{"model":"mistral-large-latest","messages":[{"role":"system","content":[{"type":"text","text":"rules"},{"type":"thinking","thinking":[{"type":"text","text":"instructions"}]}]},{"role":"assistant","reasoning_content":"duplicate","content":[{"type":"text","text":"before"},{"type":"thinking","thinking":[{"type":"text","text":"plan"},{"type":"reference","reference_ids":[1]},{"type":"tool_reference","tool":"lookup","title":"Lookup"}],"signature":"mistral-signature","closed":true},{"type":"text","text":"after"}]},{"role":"assistant","reasoning_content":" [reasoning unavailable] ","content":"answer"},{"role":"user","content":"[reasoning unavailable]"}]}`)
	system, err := common.Marshal(converted.Messages[0].Content)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"text","text":"rules"},{"type":"thinking","thinking":[{"type":"text","text":"instructions"}]}]`, string(system))
	assistant, err := common.Marshal(converted.Messages[1].Content)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"text","text":"before"},{"type":"thinking","thinking":[{"type":"text","text":"plan"},{"type":"reference","reference_ids":[1]},{"type":"tool_reference","tool":"lookup","title":"Lookup"}],"signature":"mistral-signature","closed":true},{"type":"text","text":"after"}]`, string(assistant))
	assert.Equal(t, "answer", converted.Messages[2].Content)
	assert.Equal(t, "[reasoning unavailable]", converted.Messages[3].ParseContent()[0].Text)
	prefix, _ := convertMistralRequestForTest(t, types.RelayFormatOpenAI, `{"model":"mistral-large-latest","messages":[{"role":"user","content":"q"},{"role":"assistant","prefix":true,"content":[{"type":"thinking","thinking":[{"type":"text","text":"plan"}],"closed":false}]}]}`)
	require.NotNil(t, prefix.Messages[1].Prefix)
	assert.True(t, *prefix.Messages[1].Prefix)
	content, err := common.Marshal(prefix.Messages[1].Content)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"thinking","thinking":[{"type":"text","text":"plan"}],"closed":false}]`, string(content))
}

func TestMistralThinkingOnlyReplay(t *testing.T) {
	for _, tc := range []struct {
		format types.RelayFormat
		body   string
	}{
		{format: types.RelayFormatClaude, body: `{"model":"mistral-large-latest","messages":[{"role":"user","content":"q"},{"role":"assistant","content":[{"type":"thinking","thinking":"[reasoning unavailable]"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"plan"}]},{"role":"assistant","content":[{"type":"thinking","thinking":"finish-plan"},{"type":"text","text":"answer"}]}]}`},
		{format: types.RelayFormatOpenAIResponses, body: `{"model":"mistral-large-latest","input":[{"role":"user","content":"q"},{"type":"reasoning","summary":[{"type":"summary_text","text":"[reasoning unavailable]"}],"encrypted_content":"opaque"},{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},{"role":"user","content":"next"},{"type":"reasoning","content":[{"type":"reasoning_text","text":"finish-plan"}]}]}`},
		{format: types.RelayFormatOpenAIResponses, body: `{"model":"mistral-large-latest","input":[{"role":"user","content":"q"},{"type":"\u0072easoning","summary":[{"type":"summary_text","text":"plan"}]},{"type":"function_call","call_id":"abcdef123","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"abcdef123","output":"result"},{"type":"reasoning","summary":[],"encrypted_content":"opaque"},{"type":"reasoning","summary":[{"type":"summary_text","text":"finish-plan"}]}]}`},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			converted, _ := convertMistralRequestForTest(t, tc.format, tc.body)
			content, err := common.Marshal(converted.Messages[1].Content)
			require.NoError(t, err)
			assert.JSONEq(t, `[{"type":"thinking","thinking":[{"type":"text","text":"plan"}]}]`, string(content))
			wire, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.NotContains(t, string(wire), "[reasoning unavailable]")
			assert.NotContains(t, string(wire), "opaque")
			assert.Contains(t, string(wire), "finish-plan")
			assert.NotContains(t, string(wire), `"text":""`)
		})
	}
}

func TestMistralRejectsMalformedThinkingReplay(t *testing.T) {
	adaptor := &Adaptor{}
	_, err := adaptor.ConvertOpenAIRequest(nil, nil, &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "assistant", Content: []any{map[string]any{"type": "thinking", "thinking": "invalid"}}}},
	})
	require.Error(t, err)
	assert.True(t, kitreasoning.IsClientError(err))
	_, err = adaptor.ConvertOpenAIResponsesRequest(nil, nil, dto.OpenAIResponsesRequest{
		Input: []byte(`[{"type":"reasoning","content":"invalid"}]`),
	})
	require.Error(t, err)
	assert.True(t, kitreasoning.IsClientError(err))
}
