package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	relaytypes "github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cyberPolicyMessage 是上游拒绝时给出的原因，sub2api 之类的网关会原样转发
const cyberPolicyMessage = "Request blocked by content policy."

// upstreamCyberBodies 是上游 OpenAI 协议在非流式响应里给出的 cyber_policy 拒绝形态
// wantCode 是 new-api 记录下来的错误码：原样保留上游给的值，不做改写
var upstreamCyberBodies = []struct {
	name     string
	body     string
	wantCode string
}{
	{"错误对象只有 code 和 message", `{"error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}`, "cyber_policy"},
	{"错误对象带 type", `{"error":{"type":"invalid_request_error","code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}`, "cyber_policy"},
	{"错误码大写", `{"error":{"code":"CYBER_POLICY","message":"` + cyberPolicyMessage + `"}}`, "CYBER_POLICY"},
	{"错误码前后有空白", `{"error":{"code":" cyber_policy ","message":"` + cyberPolicyMessage + `"}}`, " cyber_policy "},
}

// upstreamCyberStreamEvents 是上游 OpenAI Responses 协议在流里给出的 cyber_policy 拒绝形态
var upstreamCyberStreamEvents = []struct {
	name     string
	data     string
	wantCode string
}{
	{"response.failed 带 code 和 message", `{"type":"response.failed","response":{"id":"resp_1","model":"gpt-5.1","error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}}`, "cyber_policy"},
	{"response.failed 带 type", `{"type":"response.failed","response":{"id":"resp_1","model":"gpt-5.1","error":{"type":"invalid_request_error","code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}}`, "cyber_policy"},
	{"response.error", `{"type":"response.error","response":{"id":"resp_1","model":"gpt-5.1","error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}}`, "cyber_policy"},
	{"裸 error 事件带嵌套 error", `{"type":"error","error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}`, "cyber_policy"},
	{"裸 error 事件带 type", `{"type":"error","error":{"type":"invalid_request_error","code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}`, "cyber_policy"},
	{"裸 error 事件顶层带 code", `{"type":"error","code":"cyber_policy","message":"` + cyberPolicyMessage + `"}`, "cyber_policy"},
}

// cyberClientFormats 是所有会把上游 Responses 响应转给客户端的客户端协议
var cyberClientFormats = []relaytypes.RelayFormat{
	relaytypes.RelayFormatOpenAI,
	relaytypes.RelayFormatOpenAIResponses,
	relaytypes.RelayFormatClaude,
	relaytypes.RelayFormatGemini,
}

func newCyberRelayContext(t *testing.T, clientFormat relaytypes.RelayFormat, body string, contentType string) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "cyber-policy-test")

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.1"},
		IsStream:           contentType == "text/event-stream",
		RelayFormat:        clientFormat,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}
	return c, recorder, resp, info
}

// TestCyberPolicyNonStreamReachesEveryClientProtocol 覆盖非流式：上游给出
// cyber_policy 拒绝时，每种客户端协议都必须拿到带 cyber_policy 的错误
func TestCyberPolicyNonStreamReachesEveryClientProtocol(t *testing.T) {
	for _, shape := range upstreamCyberBodies {
		for _, clientFormat := range cyberClientFormats {
			t.Run(shape.name+" "+string(clientFormat), func(t *testing.T) {
				c, _, resp, info := newCyberRelayContext(t, clientFormat, shape.body, "application/json")

				var apiErr *relaytypes.NewAPIError
				if clientFormat == relaytypes.RelayFormatOpenAIResponses {
					_, apiErr = openaichannel.OaiResponsesHandler(c, info, resp)
				} else {
					_, apiErr = openaichannel.OaiResponsesToChatHandler(c, info, resp)
				}

				require.NotNil(t, apiErr, "cyber_policy 拒绝不能被当成正常响应")
				assert.Equal(t, relaytypes.ErrorCode(shape.wantCode), apiErr.GetErrorCode())
				assert.Contains(t, apiErr.Error(), cyberPolicyMessage)
			})
		}
	}
}

// TestCyberPolicyStreamReachesEveryClientProtocol 覆盖流式：上游在流里给出
// cyber_policy 拒绝时，每种客户端协议都必须看到这次拒绝
func TestCyberPolicyStreamReachesEveryClientProtocol(t *testing.T) {
	for _, shape := range upstreamCyberStreamEvents {
		for _, clientFormat := range cyberClientFormats {
			t.Run(shape.name+" "+string(clientFormat), func(t *testing.T) {
				body := "data: " + shape.data + "\n\n"
				c, recorder, resp, info := newCyberRelayContext(t, clientFormat, body, "text/event-stream")

				var apiErr *relaytypes.NewAPIError
				if clientFormat == relaytypes.RelayFormatOpenAIResponses {
					// 原生路径把事件原样转给客户端，错误由事件本身承载
					_, apiErr = openaichannel.OaiResponsesStreamHandler(c, info, resp)
					require.Nil(t, apiErr)
					assert.Contains(t, recorder.Body.String(), dto.CyberPolicyErrorCode)
					assert.Equal(t, dto.CyberPolicyErrorCode, info.StreamStatus.OutcomeSnapshot().ErrorCode)
					return
				}
				_, apiErr = openaichannel.OaiResponsesToChatStreamHandler(c, info, resp)

				require.NotNil(t, apiErr, "cyber_policy 拒绝不能被吞掉")
				assert.Equal(t, relaytypes.ErrorCode(shape.wantCode), apiErr.GetErrorCode())
				assert.Contains(t, apiErr.Error(), cyberPolicyMessage)
				assert.Equal(t, shape.wantCode, info.StreamStatus.OutcomeSnapshot().ErrorCode)
			})
		}
	}
}

// TestCyberPolicyFromChatUpstreamKeepsTheCode 覆盖 Chat 上游：上游 Chat 协议给出
// cyber_policy 时，new-api 作为上游的客户端也必须把错误码留住
func TestCyberPolicyFromChatUpstreamKeepsTheCode(t *testing.T) {
	for _, shape := range upstreamCyberBodies {
		t.Run(shape.name, func(t *testing.T) {
			c, recorder, resp, info := newCyberRelayContext(t, relaytypes.RelayFormatOpenAI, shape.body, "application/json")

			_, apiErr := openaichannel.OpenaiHandler(c, info, resp)

			require.NotNil(t, apiErr, "cyber_policy 拒绝不能被当成正常响应")
			assert.Equal(t, relaytypes.ErrorCode(shape.wantCode), apiErr.GetErrorCode())
			assert.Contains(t, apiErr.Error(), cyberPolicyMessage)

			_ = recorder
		})
	}
}

// TestCyberPolicyFromChatUpstreamStreamReachesClient 覆盖 Chat 上游的流式形态：
// 上游在流里直接给出 cyber_policy 时，这次请求必须判为失败——错误码要留住，
// 流状态要记下失败，客户端由控制台的错误帧流程收到带错误码的错误
func TestCyberPolicyFromChatUpstreamStreamReachesClient(t *testing.T) {
	body := "data: " + `{"error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}` + "\n\n"
	c, recorder, resp, info := newCyberRelayContext(t, relaytypes.RelayFormatOpenAI, body, "text/event-stream")

	_, apiErr := openaichannel.OaiStreamHandler(c, info, resp)

	require.NotNil(t, apiErr, "cyber_policy 拒绝不能被当成一次成功的空回复")
	assert.Equal(t, relaytypes.ErrorCode(dto.CyberPolicyErrorCode), apiErr.GetErrorCode())
	assert.Contains(t, apiErr.Error(), cyberPolicyMessage)

	outcome := info.StreamStatus.OutcomeSnapshot()
	assert.Equal(t, dto.CyberPolicyErrorCode, outcome.ErrorCode)
	assert.Equal(t, relaycommon.ResponseOutcomeFailed, outcome.Response)

	// 控制台把上游失败写成客户端能解析的错误帧，错误码原样带过去
	helper.WriteStreamError(c, info.RelayFormat, apiErr)
	assert.Contains(t, recorder.Body.String(), dto.CyberPolicyErrorCode)
	assert.Contains(t, recorder.Body.String(), cyberPolicyMessage)
}

// TestCyberPolicyBufferedStreamKeepsTheCode 覆盖上游给流、客户端不要流的情况
// 裸 error 事件不挂在 response 上，也必须认得出来
func TestCyberPolicyBufferedStreamKeepsTheCode(t *testing.T) {
	for _, shape := range upstreamCyberStreamEvents {
		t.Run(shape.name, func(t *testing.T) {
			body := "data: " + shape.data + "\n\n"
			c, _, resp, info := newCyberRelayContext(t, relaytypes.RelayFormatOpenAI, body, "text/event-stream")
			info.IsStream = false

			_, apiErr := openaichannel.OaiResponsesToChatBufferedStreamHandler(c, info, resp)

			require.NotNil(t, apiErr, "cyber_policy 拒绝不能被吞掉")
			assert.Equal(t, relaytypes.ErrorCode(shape.wantCode), apiErr.GetErrorCode())
			assert.Contains(t, apiErr.Error(), cyberPolicyMessage)
		})
	}
}

// TestDetectCyberPolicyLooksUpEveryUpstreamPosition 覆盖检测本身的查找位置，
// 与 sub2api 转发前的检测保持一致：错误码可能在 error 对象里、可能在
// response.error 里、也可能直接挂在事件顶层
func TestDetectCyberPolicyLooksUpEveryUpstreamPosition(t *testing.T) {
	for _, tc := range []struct {
		name        string
		errorField  any
		wantHit     bool
		wantMessage string
	}{
		{
			name:        "事件上的 error 对象",
			errorField:  map[string]any{"code": "cyber_policy", "message": cyberPolicyMessage},
			wantHit:     true,
			wantMessage: cyberPolicyMessage,
		},
		{
			name:        "错误码大写",
			errorField:  map[string]any{"code": "CYBER_POLICY", "message": cyberPolicyMessage},
			wantHit:     true,
			wantMessage: cyberPolicyMessage,
		},
		{
			name:        "错误码前后有空白",
			errorField:  map[string]any{"code": " cyber_policy ", "message": cyberPolicyMessage},
			wantHit:     true,
			wantMessage: cyberPolicyMessage,
		},
		{
			name:        "错误码是数字",
			errorField:  map[string]any{"code": 400, "message": cyberPolicyMessage},
			wantHit:     false,
			wantMessage: "",
		},
		{
			name:        "其它错误码",
			errorField:  map[string]any{"code": "invalid_api_key", "message": "bad key"},
			wantHit:     false,
			wantMessage: "",
		},
		{
			name:        "没有错误",
			errorField:  nil,
			wantHit:     false,
			wantMessage: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hit, message := dto.DetectCyberPolicy(tc.errorField)
			assert.Equal(t, tc.wantHit, hit)
			assert.Equal(t, tc.wantMessage, message)
		})
	}
}

// TestDetectCyberPolicyOnResponsesEvent 覆盖 Responses 事件上的三种错误位置
func TestDetectCyberPolicyOnResponsesEvent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		wantHit bool
	}{
		{
			name:    "response.failed 里的 error",
			payload: `{"type":"response.failed","response":{"id":"resp_1","error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}}`,
			wantHit: true,
		},
		{
			name:    "裸 error 事件里的 error",
			payload: `{"type":"error","error":{"code":"cyber_policy","message":"` + cyberPolicyMessage + `"}}`,
			wantHit: true,
		},
		{
			name:    "裸 error 事件顶层的 code",
			payload: `{"type":"error","code":"cyber_policy","message":"` + cyberPolicyMessage + `"}`,
			wantHit: true,
		},
		{
			name:    "无关事件",
			payload: `{"type":"response.created","response":{"id":"resp_1"}}`,
			wantHit: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var event dto.ResponsesStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(tc.payload, &event))

			hit, message := dto.DetectCyberPolicy(event.Error)
			if !hit && event.Response != nil {
				hit, message = dto.DetectCyberPolicy(event.Response.Error)
			}
			if !hit && event.Type == "error" {
				hit, message = dto.DetectCyberPolicy(map[string]any{"code": event.Code, "message": event.Message})
			}
			assert.Equal(t, tc.wantHit, hit)
			if tc.wantHit {
				assert.Equal(t, cyberPolicyMessage, message)
			}
		})
	}
}
