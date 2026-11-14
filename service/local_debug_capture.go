package service

import (
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

// LocalDebugEnabled 在进程启动时读取一次，未开启时调试链路零开销
var LocalDebugEnabled = os.Getenv("LOCAL_DEBUG") == "1"

// LocalDebugExchange 保存一次上游 HTTP 交换，由调试中间件在请求结束后打印
type LocalDebugExchange struct {
	mu              sync.Mutex
	Method          string
	URL             string
	RequestHeaders  http.Header
	RequestBody     []byte
	Status          int
	ResponseHeaders http.Header
	ResponseBody    []byte
}

// Append 记录正文；调试模式不做长度限制，完整保留一次交换
func (e *LocalDebugExchange) Append(request bool, p []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if request {
		e.RequestBody = append(e.RequestBody, p...)
		return
	}
	e.ResponseBody = append(e.ResponseBody, p...)
}

// Snapshot 复制一份快照，避免打印时与传输层并发写
func (e *LocalDebugExchange) Snapshot() LocalDebugExchange {
	e.mu.Lock()
	defer e.mu.Unlock()
	return LocalDebugExchange{
		Method:          e.Method,
		URL:             e.URL,
		RequestHeaders:  e.RequestHeaders.Clone(),
		RequestBody:     append([]byte(nil), e.RequestBody...),
		Status:          e.Status,
		ResponseHeaders: e.ResponseHeaders.Clone(),
		ResponseBody:    append([]byte(nil), e.ResponseBody...),
	}
}

// WithLocalDebugUpstreamCapture 仅在 LOCAL_DEBUG=1 时包装客户端，
// 未开启时原样返回同一个客户端，不复制、不增加传输层开销
func WithLocalDebugUpstreamCapture(c *gin.Context, client *http.Client) *http.Client {
	if !LocalDebugEnabled || c == nil || client == nil {
		return client
	}
	exchange := &LocalDebugExchange{}
	c.Set(common.LocalDebugUpstreamKey, exchange)
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if existing, ok := base.(*localDebugTransport); ok {
		base = existing.base
	}
	clone := *client
	clone.Transport = &localDebugTransport{base: base, exchange: exchange}
	return &clone
}

type localDebugTransport struct {
	base     http.RoundTripper
	exchange *LocalDebugExchange
}

func (t *localDebugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	exchange := t.exchange
	exchange.mu.Lock()
	exchange.Method = req.Method
	exchange.URL = req.URL.String()
	exchange.RequestHeaders = req.Header.Clone()
	exchange.mu.Unlock()

	cloned := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		cloned.Body = &localDebugBody{ReadCloser: req.Body, exchange: exchange, request: true}
	}
	resp, err := t.base.RoundTrip(cloned)
	if resp == nil {
		return resp, err
	}
	exchange.mu.Lock()
	exchange.Status = resp.StatusCode
	exchange.ResponseHeaders = resp.Header.Clone()
	exchange.mu.Unlock()
	if resp.Body != nil && resp.Body != http.NoBody {
		resp.Body = &localDebugBody{ReadCloser: resp.Body, exchange: exchange}
	}
	return resp, err
}

type localDebugBody struct {
	io.ReadCloser
	exchange *LocalDebugExchange
	request  bool
}

func (b *localDebugBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.exchange.Append(b.request, p[:n])
	}
	return n, err
}
