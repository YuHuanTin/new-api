package middleware

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// 临时调试用：仅在 LOCAL_DEBUG=1 时按原始字节记录客户端请求和上游交换
// 未开启时中间件只做一次透传，不读取请求体、不包装客户端
// LocalDebugCapture 必须注册在所有会改写请求的中间件之前
func LocalDebugCapture() gin.HandlerFunc {
	if os.Getenv("LOCAL_DEBUG") != "1" {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}
		headers := c.Request.Header.Clone()
		gzipEncoded := strings.Contains(strings.ToLower(headers.Get("Content-Encoding")), "gzip")
		var body []byte
		if c.Request.Body != nil && c.Request.Body != http.NoBody {
			read, err := io.ReadAll(c.Request.Body)
			_ = c.Request.Body.Close()
			if err != nil && len(read) == 0 {
				body = []byte("(read failed: " + err.Error() + ")")
			} else {
				body = read
				c.Request.Body = io.NopCloser(bytes.NewReader(read))
				c.Request.ContentLength = int64(len(read))
			}
		}

		c.Next()

		logger.LogInfo(c, "[client-request] "+formatRequest(c.Request.Method, c.Request.URL.String(), headers, body, gzipEncoded))
		if value, ok := c.Get(common.LocalDebugUpstreamKey); ok {
			if exchange, ok := value.(*service.LocalDebugExchange); ok {
				logger.LogInfo(c, "[upstream] "+formatExchange(exchange.Snapshot()))
			}
		}
	}
}

// formatRequest 输出一次客户端 HTTP 报文：请求行、原始头、正文
// 正文按 gzip 编码解压成文本，方便直接阅读；其余字节原样输出
func formatRequest(method, url string, header http.Header, body []byte, gzipEncoded bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s %s\n", method, url)
	writeHeader(&out, header)
	out.WriteString("\n")
	if gzipEncoded {
		out.Write(decodeGzip(body))
		return out.String()
	}
	out.Write(body)
	return out.String()
}

// decodeGzip 解压 gzip 正文；解压失败时原样返回并附上原因
func decodeGzip(body []byte) []byte {
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return append(body, []byte("\n(gzip decompress failed: "+err.Error()+")")...)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return append(body, []byte("\n(gzip read failed: "+err.Error()+")")...)
	}
	return decoded
}

// formatExchange 原样输出一次上游交换的报文
func formatExchange(exchange service.LocalDebugExchange) string {
	var out strings.Builder
	fmt.Fprintf(&out, ">>> %s %s\n", exchange.Method, exchange.URL)
	writeHeader(&out, exchange.RequestHeaders)
	out.WriteString("\n")
	out.Write(exchange.RequestBody)
	fmt.Fprintf(&out, "\n\n<<< HTTP %d\n", exchange.Status)
	writeHeader(&out, exchange.ResponseHeaders)
	out.WriteString("\n")
	out.Write(exchange.ResponseBody)
	return out.String()
}

func writeHeader(out *strings.Builder, header http.Header) {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		fmt.Fprintf(out, "%s: %s\n", name, strings.Join(header[name], ","))
	}
}
