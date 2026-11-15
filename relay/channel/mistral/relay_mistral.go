package mistral

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(errors.New("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	// 思考块统一转为 reasoning_content，再交给通用解析
	if info.IsStream {
		scanner := helper.NewStreamScanner(resp.Body)
		// 保留原始换行，并使用共享分片长度上限
		scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				return i + 1, data[:i+1], nil
			}
			if atEOF && len(data) > 0 {
				return len(data), data, nil
			}
			return 0, nil, nil
		})
		resp.Body = &reasoningStreamReader{Closer: resp.Body, source: scanner}
	} else {
		body, readErr := io.ReadAll(resp.Body)
		service.CloseResponseBodyGracefully(resp)
		if readErr != nil {
			return nil, types.NewOpenAIError(readErr, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
		}
		body, rewriteErr := rewriteReasoningContent(body, "message")
		if rewriteErr != nil {
			return nil, types.NewOpenAIError(rewriteErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	// Responses 请求由 chat completions 上游承接，响应需转回 Responses 格式
	if info.RelayFormat == types.RelayFormatOpenAIResponses {
		if info.IsStream {
			usage, err = openai.OaiChatToResponsesStreamHandler(c, info, resp)
		} else {
			usage, err = openai.OaiChatToResponsesHandler(c, info, resp)
		}
		return
	}
	if info.IsStream {
		usage, err = openai.OaiStreamHandler(c, info, resp)
	} else {
		usage, err = openai.OpenaiHandler(c, info, resp)
	}
	return
}

// reasoningStreamReader 转换思考分片，并保留上游关闭能力
type reasoningStreamReader struct {
	io.Closer
	source  *bufio.Scanner
	pending []byte
}

func (r *reasoningStreamReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if !r.source.Scan() {
			if err := r.source.Err(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		r.pending = rewriteReasoningChunk(r.source.Bytes())
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// rewriteReasoningChunk 改写一条 SSE 分片；不是思考分片时原样返回
func rewriteReasoningChunk(line []byte) []byte {
	body := bytes.TrimRight(line, "\r\n")
	payload, ok := bytes.CutPrefix(body, []byte("data:"))
	if !ok {
		return line
	}
	payload = bytes.TrimSpace(payload)
	encoded, err := rewriteReasoningContent(payload, "delta")
	if err != nil || bytes.Equal(encoded, payload) {
		return line
	}
	out := make([]byte, 0, len(encoded)+8)
	out = append(out, "data: "...)
	out = append(out, encoded...)
	out = append(out, line[len(body):]...)
	return out
}

// rewriteReasoningContent 统一转换流式和非流式思考块，保留用量原文
func rewriteReasoningContent(payload []byte, field string) ([]byte, error) {
	if !hasArrayContent(payload) && !bytes.Contains(payload, []byte(`\u`)) {
		return payload, nil
	}
	if !ojson.ValidBytes(payload) {
		return payload, nil
	}
	doc := ojson.NewDoc(payload)
	for i, choice := range doc.Get("choices").Array() {
		message := choice.Get(field)
		blocks := message.Get("content")
		if !blocks.IsArray() {
			continue
		}
		var content, reasoning strings.Builder
		if text := message.Get("reasoning_content"); text.Type == ojson.String {
			reasoning.WriteString(text.Str)
		}
		for _, block := range blocks.Array() {
			switch block.Get("type").String() {
			case "text":
				if text := block.Get("text"); text.Type == ojson.String {
					content.WriteString(text.Str)
				}
			case "thinking":
				for _, part := range block.Get("thinking").Array() {
					if text := part.Get("text"); text.Type == ojson.String {
						reasoning.WriteString(text.Str)
					}
				}
			}
		}
		path := fmt.Sprintf("choices.%d.%s", i, field)
		if err := relaycommon.SetJSONMember(doc, path, "content", content.String()); err != nil {
			return nil, err
		}
		if reasoning.Len() > 0 {
			if err := relaycommon.SetJSONMember(doc, path, "reasoning_content", reasoning.String()); err != nil {
				return nil, err
			}
		}
	}
	return doc.Bytes(), nil
}

// hasArrayContent 快速检查是否存在内容块数组
func hasArrayContent(payload []byte) bool {
	for {
		index := bytes.Index(payload, []byte(`"content"`))
		if index < 0 {
			return false
		}
		payload = payload[index+len(`"content"`):]
		rest := bytes.TrimLeft(payload, " \t\r\n")
		rest, ok := bytes.CutPrefix(rest, []byte(":"))
		if !ok {
			continue
		}
		rest = bytes.TrimLeft(rest, " \t\r\n")
		if len(rest) > 0 && rest[0] == '[' {
			return true
		}
	}
}
