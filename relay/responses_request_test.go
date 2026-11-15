package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	awschannel "github.com/QuantumNous/new-api/relay/channel/aws"
	claudechannel "github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/requestplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForceClaude1hCacheMarkers(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		wantToolInput     string
		channelType       int
		awsAPIType        dto.AwsApiType
		disabled, openAI  bool
	}{
		{
			name:  "tools system messages and automatic markers retain positions and metadata",
			input: `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral","ttl":"5m","scope":"global"}}],"system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"5m"}},{"type":"text","text":"tail"}]}],"cache_control":{"type":"ephemeral"}}`,
			want:  `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral","ttl":"1h","scope":"global"}}],"system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"1h"}},{"type":"text","text":"tail"}]}],"cache_control":{"type":"ephemeral","ttl":"1h"}}`,
		},
		{
			name:  "unmarked direct request enables automatic caching",
			input: `{"system":"s","messages":[{"role":"user","content":"hi"}]}`,
			want:  `{"system":"s","messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral","ttl":"1h"}}`,
		},
		{
			name:  "duplicate automatic markers retain the last marker metadata",
			input: `{"messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral","ttl":"5m","scope":"first"},"cache_control":{"type":"ephemeral","ttl":"5m","scope":"last"}}`,
			want:  `{"messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral","ttl":"1h","scope":"last"}}`,
		},
		{
			name:  "duplicate block markers retain the last marker metadata",
			input: `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral","ttl":"5m"},"cache_control":{"type":"ephemeral","ttl":"5m","scope":"last"}}],"system":[{"type":"text","text":"s","cache_control":null,"cache_control":{"type":"ephemeral","ttl":"5m"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"},"cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`,
			want:  `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral","ttl":"1h","scope":"last"}}],"system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
		},
		{
			name:  "a final null marker permits automatic caching",
			input: `{"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"5m"},"cache_control":null}]}],"cache_control":{"type":"ephemeral","ttl":"5m"},"cache_control":null}`,
			want:  `{"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":null}]}],"cache_control":{"type":"ephemeral","ttl":"1h"}}`,
		},
		{
			name:  "duplicate messages retain the last prompt and enforce its TTL",
			input: `{"messages":[{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral","ttl":"5m"}}]}],"messages":[{"role":"user","content":[{"type":"text","text":"last","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`,
			want:  `{"messages":[{"role":"user","content":[{"type":"text","text":"last","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
		},
		{
			name:  "duplicate tools retain the last definitions and enforce their TTL",
			input: `{"tools":[{"name":"first","cache_control":{"type":"ephemeral","ttl":"5m"}}],"tools":[{"name":"last","cache_control":{"type":"ephemeral","ttl":"5m"}}],"messages":[]}`,
			want:  `{"tools":[{"name":"last","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[]}`,
		},
		{
			name:  "duplicate system retains the last instructions and enforces their TTL",
			input: `{"system":[{"type":"text","text":"first","cache_control":{"type":"ephemeral","ttl":"5m"}}],"system":[{"type":"text","text":"last","cache_control":{"type":"ephemeral","ttl":"5m"}}],"messages":[]}`,
			want:  `{"system":[{"type":"text","text":"last","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[]}`,
		},
		{
			name:  "duplicate content retains the last blocks and enforces their TTL",
			input: `{"messages":[{"role":"user","content":[{"type":"text","text":"first","cache_control":{"type":"ephemeral","ttl":"5m"}}],"content":[{"type":"text","text":"last","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]}`,
			want:  `{"messages":[{"role":"user","content":[{"type":"text","text":"last","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
		},
		{
			name:          "cached tool input keeps its key order and original bytes",
			input:         `{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"lookup","input":{ "z": 1, "a": 2 },"cache_control":{"type":"ephemeral"}}]}]}`,
			want:          `{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"lookup","input":{ "z": 1, "a": 2 },"cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
			wantToolInput: `{ "z": 1, "a": 2 }`,
		},
		{
			name: "Bedrock duplicate parents use the last unmarked string", channelType: constant.ChannelTypeAws,
			input: `{"system":[{"type":"text","text":"discard","cache_control":{"type":"ephemeral"}}],"system":"last system","messages":[{"role":"user","content":[{"type":"text","text":"discard","cache_control":{"type":"ephemeral"}}]}],"messages":[{"role":"user","content":[],"content":"last message"}]}`,
			want:  `{"system":"last system","messages":[{"role":"user","content":[{"type":"text","text":"last message","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
		},
		{
			name:  "explicit direct breakpoint does not add automatic caching",
			input: `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral"}}]}`,
			want:  `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral","ttl":"1h"}}]}`,
		},
		{
			name: "Bedrock Mantle retains automatic caching", channelType: constant.ChannelTypeAws, awsAPIType: dto.AwsApiTypeMantle,
			input: `{"messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral"}}`,
			want:  `{"messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral","ttl":"1h"}}`,
		},
		{
			name: "Bedrock Runtime caches the last eligible block", channelType: constant.ChannelTypeAws,
			input: `{"tools":[{"name":"lookup"}],"system":[{"type":"text","text":"s"}],"messages":[{"role":"assistant","content":[{"type":"text","text":"hi"},{"type":"thinking","thinking":"plan"}]}]}`,
			want:  `{"tools":[{"name":"lookup"}],"system":[{"type":"text","text":"s"}],"messages":[{"role":"assistant","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"1h"}},{"type":"thinking","thinking":"plan"}]}]}`,
		},
		{
			name: "Bedrock Runtime converts string content to an explicit breakpoint", channelType: constant.ChannelTypeAws,
			input: `{"messages":[{"role":"user","content":"hi"}]}`,
			want:  `{"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]}`,
		},
		{
			name: "Bedrock Runtime replaces automatic caching without adding another explicit breakpoint", channelType: constant.ChannelTypeAws,
			input: `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral"}}`,
			want:  `{"tools":[{"name":"lookup","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "Bedrock Runtime skips thinking blocks when placing a breakpoint", channelType: constant.ChannelTypeAws,
			input: `{"system":"s","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"plan"},{"type":"redacted_thinking","data":"signature"}]}]}`,
			want:  `{"system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"plan"},{"type":"redacted_thinking","data":"signature"}]}]}`,
		},
		{name: "disabled policy preserves bytes", disabled: true, input: `{"messages": []}`, want: `{"messages": []}`},
		{name: "OpenAI final protocol preserves bytes", openAI: true, input: `{"messages": []}`, want: `{"messages": []}`},
		{name: "invalid JSON remains unchanged", input: `not json`, want: `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{Force1hCache: !tc.disabled, RelayFormat: types.RelayFormatClaude,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: tc.channelType, ChannelOtherSettings: dto.ChannelOtherSettings{AwsApiType: tc.awsAPIType}}}
			if tc.openAI {
				info.FinalRequestRelayFormat = types.RelayFormatOpenAI
			}
			input := []byte(tc.input)
			out, err := relaycommon.ForceClaude1hCache(input, info)
			require.NoError(t, err)
			assert.Equal(t, tc.input, string(input), "the original body remains replayable")
			if tc.disabled || tc.openAI || tc.input == "not json" {
				assert.Equal(t, tc.want, string(out))
			} else {
				assert.JSONEq(t, tc.want, string(out))
				assert.Equal(t, strings.Count(tc.want, `"cache_control"`), strings.Count(string(out), `"cache_control"`), "each breakpoint has one cache_control member")
			}
			if tc.wantToolInput != "" {
				assert.Equal(t, tc.wantToolInput, gjson.GetBytes(out, "messages.0.content.0.input").Raw)
			}
			repeated, err := relaycommon.ForceClaude1hCache(out, info)
			require.NoError(t, err)
			assert.Equal(t, string(out), string(repeated), "repeated rewrites keep the same upstream body")
		})
	}
}

func TestClaude1hCacheTokenGroupPolicy(t *testing.T) {
	for _, tc := range []struct {
		group, userGroup string
		force, want      bool
	}{
		{group: "default", force: true, want: true},
		{group: "default", force: false},
		{group: "auto", force: true},
		{userGroup: "auto", force: true},
		{userGroup: "default", force: true, want: true},
	} {
		t.Run(fmt.Sprintf("%s_user_%s_force_%t", tc.group, tc.userGroup, tc.force), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			common.SetContextKey(c, constant.ContextKeyUserGroup, tc.userGroup)
			require.NoError(t, middleware.SetupContextForToken(c, &model.Token{
				Id: 1, UserId: 1, Group: tc.group, ForceClaude1hCache: tc.force,
			}))
			info := relaycommon.GenRelayInfoClaude(c, &dto.ClaudeRequest{})
			input := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
			output, err := relaycommon.ForceClaude1hCache(input, info)
			require.NoError(t, err)
			assert.Equal(t, tc.want, info.Force1hCache)
			if tc.want {
				assert.Equal(t, "1h", gjson.GetBytes(output, "cache_control.ttl").String())
			} else {
				assert.Equal(t, string(input), string(output))
			}
		})
	}
}

func TestClaude1hCacheReachesUpstreamAcrossRequestPaths(t *testing.T) {
	service.InitHttpClient()
	require.NoError(t, i18n.Init())
	plugin, err := jsplugin.DefaultRegistry.Register(`
export const meta = {apiVersion: 1, key: "rp-claude-cache-egress", name: "Cache egress", version: "1.0.0", author: {name: "Test"}};
export function egress(ctx, req) {
  if (req.body.cache_control?.ttl !== "1h") {
    return {action: "reject", code: "cache.ttl", message: "Egress must see the final cache TTL"};
  }
  return {action: "continue"};
}`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key) })
	previousInstances, err := common.Marshal(request_plugin_setting.GetInstances())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: string(previousInstances)}))
	})
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = false
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	for _, tc := range []struct {
		name, path, raw                     string
		format                              types.RelayFormat
		passthrough, viaResponses, disabled bool
	}{
		{name: "Messages", path: "/v1/messages", format: types.RelayFormatClaude, raw: `{"messages":[{"role":"user","content":"hi"}],"max_tokens":8}`},
		{name: "Messages passthrough", path: "/v1/messages", format: types.RelayFormatClaude, passthrough: true, raw: `{"messages":[{"role":"user","content":"hi"}],"max_tokens":8,"vendor_field":"kept"}`},
		{name: "Chat Completions", path: "/v1/chat/completions", format: types.RelayFormatOpenAI, raw: `{"messages":[{"role":"user","content":"hi"}],"max_tokens":8}`},
		{name: "Responses", path: "/v1/responses", format: types.RelayFormatOpenAIResponses, raw: `{"input":"hi","max_output_tokens":8}`},
		{name: "Gemini", path: "/v1beta/models/claude-sonnet-4-6:generateContent", format: types.RelayFormatGemini, raw: `{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`},
		{name: "internal Responses route", path: "/v1/chat/completions", format: types.RelayFormatOpenAI, viaResponses: true, raw: `{"messages":[{"role":"user","content":"hi"}],"max_tokens":8}`},
		{name: "disabled token", path: "/v1/messages", format: types.RelayFormatClaude, disabled: true, raw: `{"messages":[{"role":"user","content":"hi"}],"max_tokens":8}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instances := `[{"id":"cache-egress","name":"Cache egress","plugin":"rp-claude-cache-egress","enabled":true,"global":true}]`
			if tc.disabled {
				instances = "[]"
			}
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: instances}))
			received := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				received <- body
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, `{"error":{"type":"api_error","message":"fixture upstream failure"}}`)
			}))
			t.Cleanup(upstream.Close)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.raw))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "claude-sonnet-4-6")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAnthropic)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
			common.SetContextKey(c, constant.ContextKeyTokenClaude1hCache, !tc.disabled)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{"cache_control": map[string]any{"type": "ephemeral", "ttl": "5m"}})
			_, _, ingressErr := requestplugin.Ingress(c, tc.format)
			require.Nil(t, ingressErr)
			var request dto.Request
			switch tc.format {
			case types.RelayFormatClaude:
				request = &dto.ClaudeRequest{}
			case types.RelayFormatOpenAI:
				request = &dto.GeneralOpenAIRequest{}
			case types.RelayFormatOpenAIResponses:
				request = &dto.OpenAIResponsesRequest{}
			case types.RelayFormatGemini:
				request = &dto.GeminiChatRequest{}
			}
			require.NoError(t, common.Unmarshal([]byte(tc.raw), request))
			request.SetModelName("claude-sonnet-4-6")
			info, err := relaycommon.GenRelayInfo(c, tc.format, request, nil)
			require.NoError(t, err)
			var apiErr *types.NewAPIError
			switch {
			case tc.viaResponses:
				info.InitChannelMeta(c)
				_, apiErr = relayResponsesRequest(c, info, &claudechannel.Adaptor{}, &dto.OpenAIResponsesRequest{Model: "claude-sonnet-4-6", Input: []byte(`"hi"`)}, false)
			case tc.format == types.RelayFormatClaude:
				apiErr = ClaudeHelper(c, info)
			case tc.format == types.RelayFormatOpenAI:
				apiErr = TextHelper(c, info)
			case tc.format == types.RelayFormatGemini:
				apiErr = GeminiHelper(c, info)
			case tc.format == types.RelayFormatOpenAIResponses:
				adaptor, body, closer, prepareErr := PrepareResponsesRequest(c, info, request.(*dto.OpenAIResponsesRequest))
				require.Nil(t, prepareErr)
				t.Cleanup(func() { assert.NoError(t, closer.Close()) })
				response, err := adaptor.DoRequest(c, info, body)
				require.NoError(t, err)
				httpResponse, ok := response.(*http.Response)
				require.True(t, ok)
				service.CloseResponseBodyGracefully(httpResponse)
				assert.Equal(t, http.StatusBadGateway, httpResponse.StatusCode)
			}
			if tc.format != types.RelayFormatOpenAIResponses {
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			}
			require.Len(t, received, 1)
			wire := <-received
			wantTTL := "1h"
			if tc.disabled {
				wantTTL = "5m"
			}
			assert.Equal(t, wantTTL, gjson.GetBytes(wire, "cache_control.ttl").String())
			if tc.passthrough {
				assert.Equal(t, "kept", gjson.GetBytes(wire, "vendor_field").String())
			}
		})
	}
}

func TestClaude1hCacheSurvivesBedrockRuntimeSerialization(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprintf("passthrough=%t", passthrough), func(t *testing.T) {
			const raw = `{"model":"claude-sonnet-4-6","max_tokens":8,"messages":[{"role":"user","content":"hi"}],"cache_control":{"type":"ephemeral"}}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(raw))
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			info := &relaycommon.RelayInfo{Force1hCache: true, RelayFormat: types.RelayFormatClaude,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAws, ApiKey: "test-access|test-secret|us-east-1", UpstreamModelName: "claude-sonnet-4-6",
					ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: passthrough}}}
			body := []byte(raw)
			var err error
			if !passthrough {
				body, err = relaycommon.ForceClaude1hCache(body, info)
				require.NoError(t, err)
			}
			adaptor := &awschannel.Adaptor{}
			_, err = adaptor.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			request, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
			require.True(t, ok)
			assert.False(t, gjson.GetBytes(request.Body, "cache_control").Exists())
			assert.Equal(t, "hi", gjson.GetBytes(request.Body, "messages.0.content.0.text").String())
			assert.Equal(t, "1h", gjson.GetBytes(request.Body, "messages.0.content.0.cache_control.ttl").String())
		})
	}
}

func TestPrepareResponsesRequestDeepSeekEffectiveReasoning(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	settings.PassThroughRequestEnabled = false
	const input = `[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"}]`
	for _, tc := range []struct {
		name, effort, override, endpoint         string
		passthrough, replaceInput, wantReasoning bool
		globalPassthrough                        bool
	}{
		{name: "override enables thinking", effort: "none", override: "high", wantReasoning: true},
		{name: "override disables thinking", effort: "high", override: "none"},
		{name: "override replaces history", effort: "high", override: "high", replaceInput: true, wantReasoning: true},
		{name: "no override", effort: "high", wantReasoning: true},
		{name: "thinking disabled", effort: "none"},
		{name: "third party endpoint", effort: "none", override: "high", endpoint: "https://other.example"},
		{name: "passthrough bypasses overrides and normalization", effort: "none", override: "high", passthrough: true},
		{name: "global passthrough bypasses normalization", effort: "high", globalPassthrough: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings.PassThroughRequestEnabled = tc.globalPassthrough
			endpoint := tc.endpoint
			if endpoint == "" {
				endpoint = "https://API.DEEPSEEK.COM:443/v1"
			}
			raw := `{"model":"deepseek-flash","reasoning":{"effort":"` + tc.effort + `"},"input":` + input + `}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-flash")
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, endpoint)
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
			if tc.override != "" {
				ops := []any{map[string]any{"path": "reasoning.effort", "mode": "set", "value": tc.override}, map[string]any{"path": "vendor_extension", "mode": "set", "value": "preserved"}}
				if tc.replaceInput {
					var replacement any
					require.NoError(t, common.Unmarshal([]byte(input), &replacement))
					ops = append(ops, map[string]any{"path": "input", "mode": "set", "value": replacement})
				}
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{"operations": ops})
			}
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.Unmarshal([]byte(raw), &req))
			info := relaycommon.GenRelayInfoResponses(c, &req)
			_, body, closer, apiErr := PrepareResponsesRequest(c, info, &req)
			require.Nil(t, apiErr)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })
			wire, err := io.ReadAll(body)
			require.NoError(t, err)
			if tc.passthrough || tc.globalPassthrough {
				assert.JSONEq(t, raw, string(wire))
				return
			}
			wantEffort := tc.effort
			if tc.override != "" {
				wantEffort = tc.override
				assert.Equal(t, "preserved", gjson.GetBytes(wire, "vendor_extension").String())
			}
			assert.Equal(t, wantEffort, gjson.GetBytes(wire, "reasoning.effort").String())
			if tc.wantReasoning {
				assert.Equal(t, "reasoning", gjson.GetBytes(wire, "input.0.type").String())
				assert.Equal(t, " ", gjson.GetBytes(wire, "input.0.content.0.text").String())
				assert.Equal(t, "function_call", gjson.GetBytes(wire, "input.1.type").String())
			} else {
				assert.JSONEq(t, input, gjson.GetBytes(wire, "input").Raw)
			}
		})
	}
}

// finalizedEgressPlugin rejects a body whose first input item is reasoning,
// which only the DeepSeek finalizer adds to the test request.
const finalizedEgressPlugin = `export const meta = {apiVersion: 1, key: "rp-finalized-egress", name: "Finalized egress", version: "1.0.0", author: {name: "Test"}};
export function egress(ctx, req) {
  const input = req.body && req.body.input;
  if (Array.isArray(input) && input.length && input[0].type === "reasoning") {
    return {action: "reject", code: "finalized.seen", message: "Egress saw the finalized body"};
  }
  return {action: "continue"};
}`

func TestPrepareResponsesRequestEgressReadsTheFinalizedBody(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	settings.PassThroughRequestEnabled = false
	require.NoError(t, i18n.Init())
	plugin, err := jsplugin.DefaultRegistry.Register(finalizedEgressPlugin, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = jsplugin.DefaultRegistry.Unregister(plugin.Meta.Key) })
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: `[{"id":"fe","name":"Finalized","plugin":"rp-finalized-egress","enabled":true,"global":true}]`}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: "[]"}))
	})

	raw := `{"model":"deepseek-flash","reasoning":{"effort":"high"},"input":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":"{}"}]}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	t.Cleanup(func() { common.CleanupBodyStorage(c) })
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "deepseek-flash")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDeepSeek)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://api.deepseek.com")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	_, _, apiErr := requestplugin.Ingress(c, types.RelayFormatOpenAIResponses)
	require.Nil(t, apiErr)
	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(raw), &req))
	info := relaycommon.GenRelayInfoResponses(c, &req)
	_, _, closer, apiErr := PrepareResponsesRequest(c, info, &req)
	if closer != nil {
		t.Cleanup(func() { _ = closer.Close() })
	}
	require.NotNil(t, apiErr, "egress must read the reasoning item the finalizer added")
	assert.Equal(t, types.ErrorCodeRequestRejected, apiErr.GetErrorCode())
}
