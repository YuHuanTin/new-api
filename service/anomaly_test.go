package service

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func restoredAnomaly(t *testing.T, otherJSON string) string {
	t.Helper()
	logs := []*model.Log{{Other: otherJSON}}
	model.FormatRootLogs(logs)
	stored, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	admin, ok := stored["admin_info"].(map[string]any)
	require.True(t, ok)
	anomaly, ok := admin["anomaly"].(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(anomaly, "gz:"))
	assert.LessOrEqual(t, len(anomaly)+2, anomalyMaxRawBytes)
	return common.DecompressText(anomaly)
}

func TestUpstreamCaptureObservesActualHTTPExchange(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		name := "HTTP1"
		if http2 {
			name = "HTTP2"
		}
		t.Run(name, func(t *testing.T) {
			var receivedBody []byte
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var err error
				receivedBody, err = io.ReadAll(req.Body)
				require.NoError(t, err)
				assert.Equal(t, "Bearer upstream-secret", req.Header.Get("Authorization"))
				assert.Equal(t, []string{"one", "two"}, req.Header.Values("X-Multi"))
				assert.ElementsMatch(t, []string{"", "gzip"}, req.Header.Values("Accept-Encoding"))
				w.Header().Add("Set-Cookie", "session=upstream-secret")
				w.Header().Add("Set-Cookie", "theme=dark")
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, ": heartbeat\r\nevent: error\r\ndata: {\"error\":\"secret\"}\r\n\r\n")
			}))
			server.EnableHTTP2 = http2
			server.StartTLS()
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "http://gateway/client?key=client-secret", strings.NewReader("client body"))
			c.Request.Header.Set("Authorization", "Bearer client-secret")
			baseClient := server.Client()
			transport := baseClient.Transport.(*http.Transport)
			transport.ForceAttemptHTTP2 = http2
			client := WithUpstreamCapture(c, baseClient)
			req, err := http.NewRequest("POST", server.URL+"/upstream?key=upstream-query", io.NopCloser(strings.NewReader("converted upstream body")))
			require.NoError(t, err)
			require.Nil(t, req.GetBody, "non-replayable bodies must also be captured")
			req.Header.Set("Authorization", "Bearer upstream-secret")
			req.Header.Add("X-Multi", "one")
			req.Header.Add("X-Multi", "two")
			req.Header.Set("Accept-Encoding", "")
			resp, err := client.Do(req)
			require.NoError(t, err)
			gotBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, []string{""}, req.Header.Values("Accept-Encoding"), "capture must not mutate the caller's headers")
			if http2 {
				assert.Equal(t, 2, resp.ProtoMajor)
			} else {
				assert.Equal(t, 1, resp.ProtoMajor)
			}
			other := model.NewLogOther()
			AppendAnomalyInfo(c, other)
			anomaly := restoredAnomaly(t, other.JSONString())
			assert.Contains(t, anomaly, string(receivedBody))
			assert.Contains(t, anomaly, `{"error":"secret"}`)
			assert.NotContains(t, anomaly, "data:")
			assert.NotContains(t, anomaly, "event:")
			assert.Contains(t, string(gotBody), "data:", "capture must not modify the relay stream")
			assert.Contains(t, anomaly, server.URL+"/upstream?key=upstream-query")
			assert.NotContains(t, anomaly, "client-secret")
			authKey, multiKey, userAgentKey := "Authorization", "X-Multi", "User-Agent"
			if http2 {
				authKey, multiKey, userAgentKey = "authorization", "x-multi", "user-agent"
			}
			assert.Contains(t, anomaly, authKey+": Bearer upstream-secret")
			assert.Contains(t, anomaly, multiKey+": one\n"+multiKey+": two")
			assert.Contains(t, anomaly, userAgentKey+": Go-http-client/")
			assert.Contains(t, anomaly, "Set-Cookie: session=upstream-secret\nSet-Cookie: theme=dark")
			assert.Contains(t, anomaly, "<<< HTTP 502")
		})
	}
}

func TestUpstreamCapturePreservesGzipHeadersAndBytes(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte("original error body"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "gzip", r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(500)
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	client := WithUpstreamCapture(c, server.Client())
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "original error body", string(body), "relay still receives transparently decoded data")
	assert.True(t, resp.Uncompressed)
	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())
	assert.Contains(t, anomaly, "Content-Encoding: gzip")
	assert.Contains(t, anomaly, "Content-Length:")
	assert.Contains(t, anomaly, string(compressed.Bytes()), "raw download retains original binary gzip bytes")
}

func TestUpstreamCaptureBudgetPreservesHeaders(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	capture := &upstreamCapture{method: "POST", url: "https://upstream", headers: http.Header{"Authorization": {"Bearer secret"}, "X-Multi": {"one", "two"}}, status: 500, responseHeaders: http.Header{"Set-Cookie": {"secret=one", "secret=two"}}}
	request := &upstreamCaptureBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("r", 3<<20) + "TAIL")), capture: capture}
	_, err := io.ReadFull(request, make([]byte, 3<<20))
	require.NoError(t, err)
	response := &upstreamCaptureBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("u", 3<<20))), capture: capture, response: true}
	_, err = io.Copy(io.Discard, response)
	require.NoError(t, err)
	tail, err := io.ReadAll(request)
	require.NoError(t, err)
	assert.Equal(t, "TAIL", string(tail), "capture limits must never truncate the transmitted body")
	c.Set(anomalyCaptureKey, capture)
	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())
	assert.LessOrEqual(t, len(anomaly), anomalyMaxRawBytes)
	assert.Contains(t, anomaly, "r(omited...)\n\n<<< HTTP 500")
	assert.Contains(t, anomaly, strings.Repeat("u", 3<<20)+"\n", "请求正文应先让出空间，保留完整响应")
	assert.Equal(t, 1, strings.Count(anomaly, "(omited...)"))
	assert.Less(t, len(anomaly), anomalyMaxRawBytes)
	assert.Contains(t, anomaly, "Authorization: Bearer secret")
	assert.Contains(t, anomaly, "Set-Cookie: secret=one\nSet-Cookie: secret=two")
	assert.NotContains(t, anomaly, "TAIL", "a truncated prefix must never be joined to later chunks")
}

func TestUpstreamCaptureNeverFallsBackToClientAndSeparatesRetries(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/client", strings.NewReader("client secret"))
	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	assert.NotContains(t, other.JSONString(), "anomaly")
	c.Set(anomalyCaptureKey, &upstreamCapture{method: "POST", url: "https://first", status: 500, responseBody: []byte("first upstream secret")})
	ResetUpstreamCapture(c)
	other = model.NewLogOther()
	AppendAnomalyInfo(c, other)
	assert.NotContains(t, other.JSONString(), "anomaly")
}

func TestUpstreamCaptureTrimsResponseOnlyAfterRequestExhaustion(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	capture := &upstreamCapture{method: "POST", url: "https://upstream", status: 500,
		headers:         http.Header{"Authorization": {"Bearer secret"}},
		responseHeaders: http.Header{"Set-Cookie": {"secret=one", "secret=two"}}}
	request := &upstreamCaptureBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("r", 1024))), capture: capture}
	_, err := io.Copy(io.Discard, request)
	require.NoError(t, err)
	response := &upstreamCaptureBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("s", 5<<20))), capture: capture, response: true}
	n, err := io.Copy(io.Discard, response)
	require.NoError(t, err)
	assert.EqualValues(t, 5<<20, n, "抓包限额不能截断实际转发的响应")
	c.Set(anomalyCaptureKey, capture)
	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	raw := restoredAnomaly(t, other.JSONString())
	assert.Contains(t, raw, "Authorization: Bearer secret\n\n(omited...)\n\n<<< HTTP 500")
	assert.Contains(t, raw, "Set-Cookie: secret=one\nSet-Cookie: secret=two")
	assert.True(t, strings.HasSuffix(raw, "s(omited...)\n"))
	assert.Equal(t, anomalyMaxRawBytes-1, len(raw), "包含省略标记的完整报文必须小于 4 MiB")
}

type failingCaptureBody struct{ closed bool }

func (b *failingCaptureBody) Read(p []byte) (int, error) {
	return copy(p, "partial"), errors.New("upstream disconnected")
}
func (b *failingCaptureBody) Close() error { b.closed = true; return nil }

func TestUpstreamCapturePreservesPartialReadAndClose(t *testing.T) {
	source := &failingCaptureBody{}
	capture := &upstreamCapture{}
	body := &upstreamCaptureBody{ReadCloser: source, capture: capture, response: true}
	buf := make([]byte, 20)
	n, err := body.Read(buf)
	require.EqualError(t, err, "upstream disconnected")
	assert.Equal(t, "partial", string(buf[:n]))
	assert.Equal(t, "partial", string(capture.responseBody))
	require.NoError(t, body.Close())
	assert.True(t, source.closed)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(anomalyCaptureKey, capture)
	info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	other := GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 1, 0, 1)
	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.Failed(), "旧适配器未上报的读取失败也必须标为异常")
	assert.Contains(t, restoredAnomaly(t, other.JSONString()), "partial")
	assert.NotContains(t, other.JSONString(), "upstream disconnected", "读取错误原文不能进入公开日志")
}

func TestUpstreamCapturePreservesBinaryHeadersAndTrailers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "\xff", r.Header.Get("X-Binary"))
		w.Header().Set("X-Binary", "\xff")
		w.Header().Set("Trailer", "X-End")
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "failure")
		w.Header().Set("X-End", "upstream-secret-trailer")
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	client := WithUpstreamCapture(c, server.Client())
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("X-Binary", "\xff")
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())
	assert.Equal(t, 2, strings.Count(anomaly, "X-Binary: \xff"))
	assert.Contains(t, anomaly, "Transfer-Encoding: chunked")
	assert.Contains(t, anomaly, "Trailer: X-End")
	assert.Contains(t, anomaly, "X-End: upstream-secret-trailer")
}

func TestUpstreamCaptureRejectsHeaderBlockOverBudgetWithoutChangingHeaders(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	headers := http.Header{"Authorization": {strings.Repeat("h", anomalyMaxRawBytes)}}
	capture := &upstreamCapture{method: "POST", url: "https://upstream", headers: headers, requestBody: []byte(strings.Repeat("r", 1<<20)), responseBody: []byte(strings.Repeat("u", 1<<20))}
	c.Set(anomalyCaptureKey, capture)
	other := model.NewLogOther()
	AppendAnomalyInfo(c, other)
	anomaly := restoredAnomaly(t, other.JSONString())
	assert.Equal(t, "[capture: Headers exceed the 4 MiB capture limit]", anomaly)
	assert.Equal(t, strings.Repeat("h", anomalyMaxRawBytes), capture.headers.Get("Authorization"))
	assert.Less(t, len(other.JSONString()), anomalyMaxRawBytes)
}

func TestSSECaptureRetainsDataIncludingInterruptedTools(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(fmt.Sprint(compressed), func(t *testing.T) {
			wire := ": heartbeat\r\nevent: message\r\ndata: {\"tool_calls\":[{\"id\":\"tool-1\"}]}\r\n\r\ndata:  {\"usage\":{\"total_tokens\":7}}\n\ndata:{\"error\":\"partial-a"
			responseBody := []byte(wire)
			headers := http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}
			if compressed {
				var buffer bytes.Buffer
				writer := gzip.NewWriter(&buffer)
				_, err := writer.Write(responseBody)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				responseBody = buffer.Bytes()
				headers.Set("Content-Encoding", "gzip")
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			capture := &upstreamCapture{responseHeaders: headers}
			body := &upstreamCaptureBody{ReadCloser: io.NopCloser(bytes.NewReader(responseBody)), capture: capture, response: true}
			// 故意让每次读取跨越 data 前缀和 JSON 边界。
			buffer := make([]byte, 3)
			for {
				_, err := body.Read(buffer)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
			}
			c.Set(anomalyCaptureKey, capture)
			other := model.NewLogOther()
			AppendAnomalyInfo(c, other)
			raw := restoredAnomaly(t, other.JSONString())
			assert.Contains(t, raw, "{\"tool_calls\":[{\"id\":\"tool-1\"}]}\n {\"usage\":{\"total_tokens\":7}}\n{\"error\":\"partial-a")
			assert.NotContains(t, raw, "data:")
			assert.NotContains(t, raw, "heartbeat")
		})
	}
}

func TestSSECapturePreservesUpstreamPayloadFormats(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, wire, payload string
	}{
		{"responses", "text/event-stream", "event: response.output_text.delta\r\ndata: {\"delta\":\"OK\"}\r\n\r\nevent: response.completed\r\ndata: {\"type\":\"response.completed\"}\r\n\r\n", "{\"delta\":\"OK\"}\n{\"type\":\"response.completed\"}"},
		{"BOM_CR_multiline", "text/event-stream", "\xef\xbb\xbfdata: {\rdata:  \"tool\":\"one\"\rdata: }\r\rdata\r\rdata: partial-a", "{\n \"tool\":\"one\"\n}\n\npartial-a"},
		{"mislabelled_JSON_lines", "text/event-stream", "{\"response\":\"hello\"}\n{\"done\":true}\n", "{\"response\":\"hello\"}\n{\"done\":true}\n"},
		{"JSON_lines", "application/x-ndjson", "{\"response\":\"hello\"}\n{\"done\":true}\n", "{\"response\":\"hello\"}\n{\"done\":true}\n"},
		{"binary_eventstream", "application/vnd.amazon.eventstream", "\x00\xffdata: literal\n", "\x00\xffdata: literal\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set(anomalyCaptureKey, &upstreamCapture{status: 502,
				responseHeaders: http.Header{"Content-Type": {tc.contentType}}, responseBody: []byte(tc.wire)})
			other := model.NewLogOther()
			AppendAnomalyInfo(c, other)
			raw := restoredAnomaly(t, other.JSONString())
			_, body, found := strings.Cut(raw[strings.Index(raw, "<<< HTTP"):], "\n\n")
			require.True(t, found)
			assert.Equal(t, tc.payload+"\n", body)
		})
	}
}
