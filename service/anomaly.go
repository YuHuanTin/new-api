package service

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptrace"
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	anomalyCaptureKey    = "anomaly_upstream_capture"
	anomalyCompressedKey = "anomaly_compressed"
	anomalyLogKey        = "anomaly"
	anomalyMaxRawBytes   = 4 << 20
	anomalyOmitted       = "(omited...)"
)

type compressedAnomaly struct {
	digest     [sha256.Size]byte
	compressed string
}

// upstreamCapture 只记录一次网关与上游之间的 HTTP 交换。
// 正文随传输层读取而记录，不回读客户端请求，也不使用协议解析结果或下游响应拼接正文。
type upstreamCapture struct {
	mu                                  sync.Mutex
	method, url                         string
	headers                             http.Header
	status                              int
	responseHeaders                     http.Header
	requestBody, responseBody           []byte
	requestTruncated, responseTruncated bool
	responseReadFailed                  bool
	requestTrailers, responseTrailers   http.Header
}

type upstreamCaptureTransport struct {
	base    http.RoundTripper
	context *gin.Context
}

// WithUpstreamCapture 创建请求级客户端包装，继续复用原 transport 和连接池，不修改共享客户端。
func WithUpstreamCapture(c *gin.Context, client *http.Client) *http.Client {
	clone := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	clone.Transport = &upstreamCaptureTransport{base: base, context: c}
	return &clone
}

func (t *upstreamCaptureTransport) CloseIdleConnections() { closeIdleConnections(t.base) }

func (t *upstreamCaptureTransport) RoundTrip(original *http.Request) (*http.Response, error) {
	capture := &upstreamCapture{method: original.Method, url: original.URL.String(), headers: make(http.Header)}
	t.context.Set(anomalyCaptureKey, capture)
	req := original.WithContext(original.Context())
	req.Header = original.Header.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	// 仅在 net/http 原本会自动请求 gzip 时显式添加相同值，避免其提前删除响应的
	// Content-Encoding/Content-Length。先记录原始响应，再为转发逻辑按需解压。
	addedGzip := transportRequestsGzip(t.base) && req.Header.Get("Accept-Encoding") == "" && req.Header.Get("Range") == "" && req.Method != http.MethodHead
	if addedGzip {
		// 保留调用方已有的空值和多值；将自动 gzip 值放在首位，由本包装负责响应解压。
		req.Header["Accept-Encoding"] = append([]string{"gzip"}, req.Header["Accept-Encoding"]...)
	}
	trace := &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) {
			capture.mu.Lock()
			capture.headers = make(http.Header)
			capture.requestBody = nil
			capture.requestTruncated = false
			capture.requestTrailers = nil
			capture.mu.Unlock()
		},
		WroteHeaderField: func(key string, values []string) {
			capture.mu.Lock()
			capture.headers[key] = append(capture.headers[key], values...)
			capture.mu.Unlock()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	if req.Body != nil && req.Body != http.NoBody {
		req.Body = &upstreamCaptureBody{ReadCloser: req.Body, capture: capture, trailers: req.Trailer}
	}
	if original.GetBody != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			body, err := original.GetBody()
			if err != nil {
				return nil, err
			}
			return &upstreamCaptureBody{ReadCloser: body, capture: capture, trailers: req.Trailer}, nil
		}
	}
	resp, err := t.base.RoundTrip(req)
	if resp == nil {
		return resp, err
	}
	capture.mu.Lock()
	capture.status, capture.responseHeaders = resp.StatusCode, resp.Header.Clone()
	if capture.responseHeaders == nil {
		capture.responseHeaders = make(http.Header)
	}
	// net/http 将这两类响应头移到了独立字段，按传输层解析结果补回展示，避免漏记。
	if len(resp.TransferEncoding) > 0 && len(capture.responseHeaders.Values("Transfer-Encoding")) == 0 {
		capture.responseHeaders["Transfer-Encoding"] = slices.Clone(resp.TransferEncoding)
	}
	if len(resp.Trailer) > 0 && len(capture.responseHeaders.Values("Trailer")) == 0 {
		names := make([]string, 0, len(resp.Trailer))
		for name := range resp.Trailer {
			names = append(names, name)
		}
		slices.Sort(names)
		capture.responseHeaders.Set("Trailer", strings.Join(names, ", "))
	}
	capture.mu.Unlock()
	if resp.Body != nil && resp.Body != http.NoBody {
		resp.Body = &upstreamCaptureBody{ReadCloser: resp.Body, capture: capture, response: true, trailers: resp.Trailer}
	}
	if addedGzip && strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		resp.Body = &upstreamGzipBody{ReadCloser: resp.Body}
		// 对转发逻辑保持 net/http 原有的自动解压行为；抓包保留解压前的 header 和正文。
		resp.Header = resp.Header.Clone()
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Uncompressed = true
	}
	return resp, err
}

func transportRequestsGzip(transport http.RoundTripper) bool {
	switch transport := transport.(type) {
	case *http.Transport:
		return !transport.DisableCompression
	case *shardedRoundTripper:
		return len(transport.shards) > 0 && transportRequestsGzip(transport.shards[0])
	default:
		return false
	}
}

type upstreamGzipBody struct {
	io.ReadCloser
	reader *gzip.Reader
	err    error
}

func (b *upstreamGzipBody) Read(p []byte) (int, error) {
	if b.reader == nil && b.err == nil {
		b.reader, b.err = gzip.NewReader(b.ReadCloser)
	}
	if b.err != nil {
		return 0, b.err
	}
	return b.reader.Read(p)
}

type upstreamCaptureBody struct {
	io.ReadCloser
	capture  *upstreamCapture
	response bool
	trailers http.Header
}

func (b *upstreamCaptureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n == 0 && err == nil {
		return n, err
	}
	b.capture.mu.Lock()
	defer b.capture.mu.Unlock()
	if b.response && err != nil && err != io.EOF {
		b.capture.responseReadFailed = true
	}
	if err == io.EOF && len(b.trailers) > 0 {
		if b.response {
			b.capture.responseTrailers = b.trailers.Clone()
		} else {
			b.capture.requestTrailers = b.trailers.Clone()
		}
	}
	body, truncated := &b.capture.requestBody, &b.capture.requestTruncated
	if b.response {
		body, truncated = &b.capture.responseBody, &b.capture.responseTruncated
	}
	// 两个方向共用暂存额度；只保留连续前缀，截断后不再追加后续片段。
	if *truncated {
		return n, err
	}
	available := max(0, anomalyMaxRawBytes-len(b.capture.requestBody)-len(b.capture.responseBody))
	if b.response && n > available && len(b.capture.requestBody) > 0 {
		// 响应优先：空间不足时先裁掉请求正文尾部，尽可能完整保留上游响应。
		remove := min(len(b.capture.requestBody), n-available)
		b.capture.requestBody = b.capture.requestBody[:len(b.capture.requestBody)-remove]
		b.capture.requestTruncated = true
		available = max(0, anomalyMaxRawBytes-len(b.capture.requestBody)-len(b.capture.responseBody))
	}
	take := min(n, available)
	*body = append(*body, p[:take]...)
	if take < n {
		*truncated = true
	}
	return n, err
}

// upstreamCaptureReadFailed 兜底识别旧适配器只记日志、未上报的上游读取失败。
func upstreamCaptureReadFailed(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, _ := c.Get(anomalyCaptureKey)
	capture, ok := value.(*upstreamCapture)
	if !ok || capture == nil {
		return false
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.responseReadFailed
}

// ResetUpstreamCapture 在渠道重试前清空现场，防止新尝试误用上次的响应。
func ResetUpstreamCapture(c *gin.Context) {
	c.Set(anomalyCaptureKey, nil)
	c.Set(anomalyCompressedKey, nil)
}

// AppendAnomalyInfo 仅供异常日志调用。数据库和 API 保留一个 gzip+Base64 字符串，成功请求不落库。
func AppendAnomalyInfo(c *gin.Context, other *model.LogOther) {
	if c == nil || other == nil {
		return
	}
	value, _ := c.Get(anomalyCaptureKey)
	capture, ok := value.(*upstreamCapture)
	if !ok || capture == nil {
		return
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	requestBody, responseBody := capture.requestBody, capture.responseBody
	requestTruncated, responseTruncated := capture.requestTruncated, capture.responseTruncated
	mediaType, _, _ := mime.ParseMediaType(capture.responseHeaders.Get("Content-Type"))
	if mediaType == "text/event-stream" {
		if strings.EqualFold(capture.responseHeaders.Get("Content-Encoding"), "gzip") {
			reader, err := gzip.NewReader(bytes.NewReader(responseBody))
			if err == nil {
				decoded, readErr := io.ReadAll(io.LimitReader(reader, anomalyMaxRawBytes+1))
				_ = reader.Close()
				responseTruncated = responseTruncated || readErr != nil || len(decoded) > anomalyMaxRawBytes
				responseBody = decoded[:min(len(decoded), anomalyMaxRawBytes)]
			} else {
				// 无法解压时保留原始字节，不伪造 SSE 内容。
				mediaType = ""
			}
		}
		if mediaType == "text/event-stream" {
			responseBody = sseCaptureData(responseBody)
		}
	}
	// 保留传输层提供的 header 值，不脱敏、不替换；顺序、大小写和 HTTP/2 帧由 net/http 管理。
	var requestHead, responseHead strings.Builder
	fmt.Fprintf(&requestHead, ">>> %s %s\n", capture.method, capture.url)
	writeCaptureHeaders(&requestHead, capture.headers)
	if capture.status > 0 {
		fmt.Fprintf(&responseHead, "<<< HTTP %d\n", capture.status)
	} else {
		responseHead.WriteString("<<<\n")
	}
	writeCaptureHeaders(&responseHead, capture.responseHeaders)
	var trailers strings.Builder
	writeCaptureHeaders(&trailers, capture.requestTrailers)
	writeCaptureHeaders(&trailers, capture.responseTrailers)
	if requestHead.Len()+responseHead.Len()+trailers.Len()+5 >= anomalyMaxRawBytes {
		other.SetAdmin(anomalyLogKey, common.CompressText("[capture: Headers exceed the 4 MiB capture limit]"))
		return
	}
	for {
		var raw strings.Builder
		raw.WriteString(requestHead.String())
		raw.WriteByte('\n')
		raw.Write(requestBody)
		if requestTruncated {
			raw.WriteString(anomalyOmitted)
		}
		raw.WriteString("\n\n")
		writeCaptureHeaders(&raw, capture.requestTrailers)
		raw.WriteString(responseHead.String())
		raw.WriteByte('\n')
		raw.Write(responseBody)
		if responseTruncated {
			raw.WriteString(anomalyOmitted)
		}
		raw.WriteByte('\n')
		writeCaptureHeaders(&raw, capture.responseTrailers)
		// 以解压后的完整报文计限额，分隔符和省略标记也计入；压缩后的存储值同样受限。
		remove := raw.Len() - (anomalyMaxRawBytes - 1)
		if remove <= 0 {
			digest := sha256.Sum256([]byte(raw.String()))
			cached, _ := c.Get(anomalyCompressedKey)
			compressed, ok := cached.(compressedAnomaly)
			if !ok || compressed.digest != digest {
				compressed = compressedAnomaly{digest: digest, compressed: common.CompressText(raw.String())}
				c.Set(anomalyCompressedKey, compressed)
			}
			if len(compressed.compressed)+2 < anomalyMaxRawBytes {
				other.SetAdmin(anomalyLogKey, compressed.compressed)
				return
			}
			// 不可压缩的二进制数据可能因 Base64 膨胀超限，按超出量缩减后重新压缩。
			remove = max(1, (len(compressed.compressed)+2-anomalyMaxRawBytes+1)*3/4+64)
		}
		if len(requestBody) == 0 && len(responseBody) == 0 {
			other.SetAdmin(anomalyLogKey, common.CompressText("[capture: Headers exceed the 4 MiB capture limit]"))
			return
		}
		// 先裁请求正文；请求已经清空而响应本身仍超限时，才裁响应正文。
		if len(requestBody) > 0 {
			if !requestTruncated {
				remove += len(anomalyOmitted)
			}
			requestBody = requestBody[:len(requestBody)-min(remove, len(requestBody))]
			requestTruncated = true
		} else {
			if !responseTruncated {
				remove += len(anomalyOmitted)
			}
			responseBody = responseBody[:len(responseBody)-min(remove, len(responseBody))]
			responseTruncated = true
		}
	}
}

func writeCaptureHeaders(out *strings.Builder, headers http.Header) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range headers[name] {
			fmt.Fprintf(out, "%s: %s\n", name, value)
		}
	}
}

// sseCaptureData 只剥离 SSE 封装，不解析或改写 data 里的 JSON。
// 支持 LF、CRLF、CR 换行、多行 data、流开头的 BOM，以及中断时尚未换行的 data 片段。
// 未发现 data 字段时保留原文，避免上游错误标注 Content-Type 导致抓包内容丢失。
func sseCaptureData(body []byte) []byte {
	var out bytes.Buffer
	first := true
	remaining := bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	for len(remaining) > 0 {
		end := bytes.IndexAny(remaining, "\r\n")
		line := remaining
		if end < 0 {
			remaining = nil
		} else {
			line = remaining[:end]
			separator := remaining[end]
			remaining = remaining[end+1:]
			if separator == '\r' {
				remaining, _ = bytes.CutPrefix(remaining, []byte("\n"))
			}
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		if !bytes.Equal(field, []byte("data")) {
			continue
		}
		value, _ = bytes.CutPrefix(value, []byte(" "))
		if !first {
			out.WriteByte('\n')
		}
		out.Write(value)
		first = false
	}
	if first {
		return body
	}
	return out.Bytes()
}
