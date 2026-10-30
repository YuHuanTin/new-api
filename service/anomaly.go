package service

import (
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	// anomalyUpstreamResponseKey 是请求上下文里存放上游失败响应现场的键
	anomalyUpstreamResponseKey = "anomaly_upstream_response"
	// anomalyUpstreamRequestKey 是请求上下文里存放上游请求行的键
	anomalyUpstreamRequestKey = "anomaly_upstream_request"
	// anomalyCompressedKey 缓存本次请求已经压缩过的现场，重试和最终消耗日志
	// 往往保存同一份内容，不必重复压缩
	anomalyCompressedKey = "anomaly_compressed"
	// anomalyLogKey 是异常现场在 logs.other 管理员可见部分里的键
	anomalyLogKey = "anomaly"
	// anomalyMaxRawBytes 是一条失败报文（请求行、头、正文和上游响应）的总预算
	anomalyMaxRawBytes = 4 << 20
	// anomalyTruncatedSuffix 标记内容被截断，读日志时一眼能看出来
	anomalyTruncatedSuffix = "...(truncated)"
)

// upstreamResponse 是上游失败响应的现场。Headers 直接引用原 map，落库前才复制
type upstreamResponse struct {
	Status  int
	Headers http.Header
	Body    string
}

// upstreamRequest 是真正发给上游的请求行
type upstreamRequest struct {
	Method string
	URL    string
}

// compressedAnomaly 是本次请求已经压好的现场。digest 用来判断新现场是否与它
// 相同：同一份内容重试、错误日志和最终消耗日志都会再写一次
type compressedAnomaly struct {
	digest     [sha256.Size]byte
	compressed string
}

// RecordUpstreamRequest 记下实际发往上游的方法和完整地址。客户端请求的是本站
// 地址，排查时更需要看到上游被调用的那个 URL
func RecordUpstreamRequest(c *gin.Context, req *http.Request) {
	if c == nil || req == nil || req.URL == nil {
		return
	}
	c.Set(anomalyUpstreamRequestKey, upstreamRequest{
		Method: req.Method,
		URL:    req.URL.String(),
	})
}

// RecordUpstreamResponse 记下上游失败响应的状态码、响应头和响应体；只在失败分支调用
func RecordUpstreamResponse(c *gin.Context, resp *http.Response, body []byte) {
	if c == nil || resp == nil {
		return
	}
	c.Set(anomalyUpstreamResponseKey, upstreamResponse{
		Status:  resp.StatusCode,
		Headers: resp.Header,
		Body:    truncateAnomalyBodyBytes(body),
	})
}

// RecordUpstreamStreamBody 记下流里的失败帧；流式请求的 HTTP 状态码和响应头仍取自 resp
func RecordUpstreamStreamBody(c *gin.Context, resp *http.Response, body string) {
	if c == nil || body == "" {
		return
	}
	record := upstreamResponse{Body: truncateAnomalyBody(body)}
	if resp != nil {
		record.Status = resp.StatusCode
		record.Headers = resp.Header
	}
	if value, ok := c.Get(anomalyUpstreamResponseKey); ok {
		if existing, ok := value.(upstreamResponse); ok {
			if existing.Body != "" {
				// HTTP 层已经留下了响应体，流里的帧不再覆盖它
				return
			}
			record.Status = existing.Status
			record.Headers = existing.Headers
		}
	}
	c.Set(anomalyUpstreamResponseKey, record)
}

// requestURL 拼出这次请求的完整地址，查询串也原样保留
func requestURL(c *gin.Context) string {
	request := c.Request
	if request == nil || request.URL == nil {
		return ""
	}
	scheme := request.URL.Scheme
	if scheme == "" {
		scheme = "http"
		if request.TLS != nil {
			scheme = "https"
		}
	}
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}
	if host == "" {
		return request.URL.RequestURI()
	}
	return scheme + "://" + host + request.URL.RequestURI()
}

// AppendAnomalyInfo 把失败现场写进日志的管理员可见部分。整条报文共用 4 MB
// 预算，超限先截断正文再丢弃头；整块压缩后再落库，清理时只删这一块
func AppendAnomalyInfo(c *gin.Context, other *model.LogOther) {
	if c == nil || other == nil {
		return
	}
	anomaly := map[string]any{}

	if value, ok := c.Get(anomalyUpstreamRequestKey); ok {
		if upstream, ok := value.(upstreamRequest); ok {
			if upstream.Method != "" {
				anomaly["request_method"] = upstream.Method
			}
			if upstream.URL != "" {
				anomaly["request_url"] = upstream.URL
			}
		}
	}
	if c.Request != nil {
		if _, ok := anomaly["request_method"]; !ok {
			anomaly["request_method"] = c.Request.Method
		}
		if _, ok := anomaly["request_url"]; !ok {
			anomaly["request_url"] = requestURL(c)
		}
		if storage, err := common.GetBodyStorage(c); err == nil {
			if reader, err := storage.NewReader(); err == nil {
				buf := make([]byte, anomalyMaxRawBytes+1)
				n, _ := io.ReadFull(reader, buf)
				_ = reader.Close()
				if n > 0 {
					anomaly["request_body"] = truncateAnomalyBody(string(buf[:n]))
				}
			}
		}
		if headers := anomalyHeaders(c.Request.Header); len(headers) > 0 {
			anomaly["request_headers"] = headers
		}
	}
	if value, ok := c.Get(anomalyUpstreamResponseKey); ok {
		if upstream, ok := value.(upstreamResponse); ok {
			if upstream.Status > 0 {
				anomaly["upstream_status"] = upstream.Status
			}
			if headers := anomalyHeaders(upstream.Headers); len(headers) > 0 {
				anomaly["upstream_headers"] = headers
			}
			if upstream.Body != "" {
				anomaly["upstream_body"] = upstream.Body
			}
		}
	}
	if len(anomaly) == 0 {
		return
	}
	payload := fitAnomalyPayload(anomaly)
	if len(payload) == 0 {
		return
	}
	other.SetAdmin(anomalyLogKey, compressAnomalyPayload(c, payload))
}

// compressAnomalyPayload 压缩现场，并复用本次请求里内容相同的上一次结果
func compressAnomalyPayload(c *gin.Context, payload []byte) string {
	digest := sha256.Sum256(payload)
	if value, ok := c.Get(anomalyCompressedKey); ok {
		if cached, ok := value.(compressedAnomaly); ok && cached.digest == digest {
			return cached.compressed
		}
	}
	compressed := common.CompressText(string(payload))
	c.Set(anomalyCompressedKey, compressedAnomaly{digest: digest, compressed: compressed})
	return compressed
}

// fitAnomalyPayload 把现场序列化到 4 MB 以内：先反复截断请求体和响应体里较大的
// 那个，仍然超限就丢掉请求头和响应头并留下标记
func fitAnomalyPayload(anomaly map[string]any) []byte {
	payload, err := common.Marshal(anomaly)
	if err != nil {
		return nil
	}
	for len(payload) > anomalyMaxRawBytes && halveLargestAnomalyBody(anomaly) {
		payload, err = common.Marshal(anomaly)
		if err != nil {
			return nil
		}
	}
	if len(payload) <= anomalyMaxRawBytes {
		return payload
	}
	delete(anomaly, "request_headers")
	delete(anomaly, "upstream_headers")
	anomaly["headers_dropped"] = true
	payload, err = common.Marshal(anomaly)
	if err != nil {
		return nil
	}
	return payload
}

// halveLargestAnomalyBody 把两个正文里较长的那个砍掉一半，返回是否还有可砍的
// 内容。截断点落在 UTF-8 字符边界上，避免写出半个字符
func halveLargestAnomalyBody(anomaly map[string]any) bool {
	key := ""
	length := 0
	for _, candidate := range []string{"request_body", "upstream_body"} {
		value, ok := anomaly[candidate].(string)
		if !ok {
			continue
		}
		value = strings.TrimSuffix(value, anomalyTruncatedSuffix)
		if len(value) == 0 {
			continue
		}
		if len(value) > length {
			key, length = candidate, len(value)
		}
	}
	if key == "" {
		return false
	}
	body := strings.TrimSuffix(anomaly[key].(string), anomalyTruncatedSuffix)
	cut := len(body) / 2
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	anomaly[key] = body[:cut] + anomalyTruncatedSuffix
	return true
}

// truncateAnomalyBody 截断单个正文，并标记被截断的事实。单条正文不可能超过
// 整条报文的总预算，这里只是提前省掉一份用不上的内存
func truncateAnomalyBody(body string) string {
	if len(body) <= anomalyMaxRawBytes {
		return body
	}
	cut := anomalyMaxRawBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return body[:cut] + anomalyTruncatedSuffix
}

// truncateAnomalyBodyBytes 先按字节截断再转字符串，避免为一个用不上的大响应
// 整体复制一份
func truncateAnomalyBodyBytes(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) <= anomalyMaxRawBytes {
		return string(body)
	}
	cut := anomalyMaxRawBytes
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return string(body[:cut]) + anomalyTruncatedSuffix
}

// anomalyHeaders 把头转成 map，同名多值用逗号拼接
func anomalyHeaders(header http.Header) map[string]string {
	result := make(map[string]string, len(header))
	for name, values := range header {
		if len(values) == 0 {
			continue
		}
		result[name] = strings.Join(values, ", ")
	}
	return result
}
