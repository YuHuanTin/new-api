package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restoredAnomaly 走一遍读取路径，拿到前端实际收到的异常现场
func restoredAnomaly(t *testing.T, otherJSON string) map[string]any {
	t.Helper()
	logs := []*model.Log{{Other: otherJSON}}
	model.FormatAdminLogs(logs)
	stored, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	adminInfo, ok := stored["admin_info"].(map[string]any)
	require.True(t, ok, "异常信息应写进管理员可见的部分")
	anomaly, ok := adminInfo["anomaly"].(map[string]any)
	require.True(t, ok, "读取时应还原压缩的异常现场")
	return anomaly
}

func TestAppendAnomalyInfoCapturesRequestAndUpstreamResponse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=sk-query", io.NopCloser(io.LimitReader(
		strings.NewReader(`{"model":"gpt-5.1","messages":[]}`), 1024)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer secret-token")
	c.Request.Header.Add("X-Trace", "a")
	c.Request.Header.Add("X-Trace", "b")

	upstream := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"Set-Cookie":   []string{"session=abc", "theme=dark"},
		},
	}
	RecordUpstreamResponse(c, upstream, []byte(`{"error":{"code":"cyber_policy"}}`))

	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)

	stored := other.JSONString()
	logs := []*model.Log{{Other: stored}}
	model.FormatAdminLogs(logs)
	assert.Less(t, len(stored), len(logs[0].Other), "现场报文压缩后应比还原结果短")

	anomaly := restoredAnomaly(t, logs[0].Other)
	assert.Contains(t, anomaly["request_body"], "gpt-5.1")
	assert.Equal(t, http.MethodPost, anomaly["request_method"])
	assert.Equal(t, "http://example.com/v1/chat/completions?key=sk-query", anomaly["request_url"])
	assert.EqualValues(t, http.StatusBadRequest, anomaly["upstream_status"])
	assert.Contains(t, anomaly["upstream_body"], "cyber_policy")

	headers, ok := anomaly["request_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"Bearer secret-token"}, headers["Authorization"], "请求头原样留存")
	assert.Equal(t, []any{"application/json"}, headers["Content-Type"])
	assert.Equal(t, []any{"a", "b"}, headers["X-Trace"], "同名多值不合并")

	upstreamHeaders, ok := anomaly["upstream_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"session=abc", "theme=dark"}, upstreamHeaders["Set-Cookie"], "响应头原样留存")
	assert.Equal(t, []any{"application/json"}, upstreamHeaders["Content-Type"])
}

// 流式请求的 HTTP 状态码是 200，失败帧才是真正的拒绝；HTTP 层已经有响应体时
// 不被流里的帧覆盖
func TestRecordUpstreamStreamBodyKeepsTheFailingFrame(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	streamResp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Upstream": []string{"sub2api"}}}
	RecordUpstreamStreamBody(c, streamResp, `{"type":"response.failed","response":{"error":{"code":"cyber_policy"}}}`)

	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())
	assert.Contains(t, anomaly["upstream_body"], "cyber_policy")
	assert.EqualValues(t, http.StatusOK, anomaly["upstream_status"], "流式请求的 HTTP 层状态码也要留下")
	assert.Equal(t, map[string]any{"X-Upstream": []any{"sub2api"}}, anomaly["upstream_headers"])

	RecordUpstreamResponse(c, &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}}, []byte("bad gateway"))
	other = model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly = restoredAnomaly(t, other.JSONString())
	assert.Equal(t, "bad gateway", anomaly["upstream_body"], "HTTP 层的响应体优先")
	assert.EqualValues(t, http.StatusBadGateway, anomaly["upstream_status"])
}

// 请求体和响应体共用一个 4 MB 预算：超限时截断正文，头和状态码照常保留
func TestAppendAnomalyInfoKeepsTheWholeCaptureWithinBudget(t *testing.T) {
	requestBody := strings.Repeat("r", 3<<20)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", io.NopCloser(strings.NewReader(requestBody)))
	c.Request.Header.Set("Content-Type", "application/json")
	RecordUpstreamResponse(c, &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}}, []byte(strings.Repeat("u", 3<<20)))

	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())

	payload, err := common.Marshal(anomaly)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(payload), anomalyMaxRawBytes, "正文截断后整个现场不超过 4 MB")
	assert.Contains(t, anomaly["request_body"], anomalyTruncatedSuffix, "超预算的请求体应被截断")
	assert.Contains(t, anomaly["upstream_body"], anomalyTruncatedSuffix, "超预算的响应体应被截断")
	headers, ok := anomaly["request_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"application/json"}, headers["Content-Type"], "头不参与截断")
	assert.EqualValues(t, http.StatusBadGateway, anomaly["upstream_status"])
}

// 头没有单独的数量和长度上限，也不参与截断
func TestAppendAnomalyInfoDoesNotLimitHeaderSizeOrCount(t *testing.T) {
	longValue := strings.Repeat("h", 128<<10)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	c.Request.Header.Set("X-Long", longValue)
	for i := 0; i < 80; i++ {
		c.Request.Header.Set("X-Multi-"+strconv.Itoa(i), "v")
	}

	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())
	headers, ok := anomaly["request_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{longValue}, headers["X-Long"], "长头不单独截断")
	assert.GreaterOrEqual(t, len(headers), 81, "头的数量不单独限制")
	assert.Equal(t, []any{"v"}, headers["X-Multi-79"])
}

// 正文都砍不动时（只剩超大头），头仍然原样保留，不丢弃也不截断
func TestFitAnomalyPayloadKeepsHeadersWhenBodiesCannotShrink(t *testing.T) {
	bigValue := strings.Repeat("h", anomalyMaxRawBytes+1)
	anomaly := map[string]any{
		"request_headers":  map[string][]string{"X-Big": {bigValue}},
		"upstream_headers": map[string][]string{"X-Small": {"v"}},
		"upstream_body":    "",
	}
	payload := fitAnomalyPayload(anomaly)
	require.NotEmpty(t, payload)

	var decoded map[string]any
	require.NoError(t, common.UnmarshalJsonStr(string(payload), &decoded))
	assert.NotContains(t, decoded, "headers_dropped")
	requestHeaders, ok := decoded["request_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{bigValue}, requestHeaders["X-Big"], "超预算的头也要完整保留")
	assert.Equal(t, map[string]any{"X-Small": []any{"v"}}, decoded["upstream_headers"])
}
