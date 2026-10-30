package service

import (
	"fmt"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// markResponsesFailure 把上游的失败事件写进流状态：错误码和错误类型取事件上的
// 错误对象；上游只带 code 和 message 时按 upstream_error 分类，和 new-api 发给
// 客户端的错误一致。cyber_policy 拒绝同时打上标记，供按渠道开启的处置统计
func markResponsesFailure(info *relaycommon.RelayInfo, event *dto.ResponsesStreamResponse) {
	code, errorType := event.Code, ""
	if oaiErr := event.GetOpenAIError(); oaiErr != nil {
		if oaiErr.Code != nil {
			code = fmt.Sprint(oaiErr.Code)
		}
		errorType = oaiErr.Type
	}
	if errorType == "" {
		errorType = string(types.ErrorTypeUpstreamError)
	}
	info.StreamStatus.MarkFailed(code, errorType, 0)
	if dto.IsCyberPolicyCode(code) {
		info.CyberPolicyViolation = true
	}
}
