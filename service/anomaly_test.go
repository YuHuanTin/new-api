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

	upstream := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "Set-Cookie": []string{"session=abc"}},
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
	assert.Equal(t, "Bearer secret-token", headers["Authorization"], "请求头原样留存")
	assert.Equal(t, "application/json", headers["Content-Type"])

	upstreamHeaders, ok := anomaly["upstream_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "session=abc", upstreamHeaders["Set-Cookie"], "响应头原样留存")
	assert.Equal(t, "application/json", upstreamHeaders["Content-Type"])
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
	assert.Equal(t, map[string]any{"X-Upstream": "sub2api"}, anomaly["upstream_headers"])

	RecordUpstreamResponse(c, &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{}}, []byte("bad gateway"))
	other = model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly = restoredAnomaly(t, other.JSONString())
	assert.Equal(t, "bad gateway", anomaly["upstream_body"], "HTTP 层的响应体优先")
	assert.EqualValues(t, http.StatusBadGateway, anomaly["upstream_status"])
}

// 一条失败报文（请求行、请求头、请求体、上游响应头、上游响应体）共用一个
// 4 MB 总预算：超限时截断正文，而不是把整条现场丢掉
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
	assert.LessOrEqual(t, len(payload), anomalyMaxRawBytes, "整条报文不能超过 4 MB")
	assert.Contains(t, anomaly["request_body"], anomalyTruncatedSuffix, "超预算的请求体应被截断")
	assert.Contains(t, anomaly["upstream_body"], anomalyTruncatedSuffix, "超预算的响应体应被截断")
	headers, ok := anomaly["request_headers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "application/json", headers["Content-Type"], "预算内头照常保留")
	assert.EqualValues(t, http.StatusBadGateway, anomaly["upstream_status"])
}

// 头没有单独的数量和长度上限，只有整条报文的 4 MB 总预算
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
	assert.Equal(t, longValue, headers["X-Long"], "长头不单独截断")
	assert.GreaterOrEqual(t, len(headers), 81, "头的数量不单独限制")
	assert.Equal(t, "v", headers["X-Multi-79"])
}

// 正文都砍不动时（只有超大头），整块头会被丢掉并留下标记
func TestFitAnomalyPayloadDropsHeadersWhenBodiesCannotShrink(t *testing.T) {
	anomaly := map[string]any{
		"request_headers": map[string]string{
			"X-Big": strings.Repeat("h", anomalyMaxRawBytes+1),
		},
		"upstream_headers": map[string]string{"X-Small": "v"},
		"upstream_body":    "",
	}
	payload := fitAnomalyPayload(anomaly)
	require.LessOrEqual(t, len(payload), anomalyMaxRawBytes)

	var decoded map[string]any
	require.NoError(t, common.UnmarshalJsonStr(string(payload), &decoded))
	assert.Equal(t, true, decoded["headers_dropped"])
	_, hasRequestHeaders := decoded["request_headers"]
	assert.False(t, hasRequestHeaders, "超预算的头应整块丢弃")
	_, hasUpstreamHeaders := decoded["upstream_headers"]
	assert.False(t, hasUpstreamHeaders)
}
