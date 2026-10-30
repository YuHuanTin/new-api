package helper

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// WriteStreamError 把上游失败写进已经开流的响应，格式必须是客户端能解析的
// 错误帧。直接 c.JSON 会把裸 JSON 对象塞进事件流，标准 SSE 客户端读不到，
// 只会看到一个被截断的正常结束
func WriteStreamError(c *gin.Context, format types.RelayFormat, apiError *types.NewAPIError) {
	if c == nil || apiError == nil {
		return
	}
	status, _ := common.GetContextKeyType[*relaycommon.StreamStatus](c, constant.ContextKeyResponseStreamStatus)
	if status == nil {
		status = relaycommon.NewStreamStatus()
		common.SetContextKey(c, constant.ContextKeyResponseStreamStatus, status)
	}
	status.MarkFailed(string(apiError.GetErrorCode()), apiError.ToOpenAIError().Type, apiError.StatusCode)
	upstreamURLs := common.UpstreamAddresses(c)
	if format == types.RelayFormatClaude {
		_ = ClaudeData(c, dto.ClaudeResponse{Type: "error", Error: apiError.ToClaudeError(upstreamURLs...)})
		return
	}
	_ = ObjectData(c, gin.H{"error": apiError.ToOpenAIError(upstreamURLs...)})
	Done(c)
}
