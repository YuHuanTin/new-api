package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// appendStreamFailureCode 把上游的结构化错误码和错误类型写进流状态日志。这是让
// 控制台能认出拒绝原因的关键，比如 cyber_policy；只记录分类事实，上游的原始消息
// 不外泄。传输层的失败（流空闲超时、上游断连）没有协议错误码，用结束原因补一个，
// 异常管理的「错误码」列才有内容
func appendStreamFailureCode(streamInfo map[string]any, ss *relaycommon.StreamStatus, status string) {
	outcome := ss.OutcomeSnapshot()
	if outcome.ErrorCode != "" {
		streamInfo["error_code"] = outcome.ErrorCode
	}
	if outcome.ErrorType != "" {
		streamInfo["error_type"] = outcome.ErrorType
	}
	if status != "error" || outcome.ErrorCode != "" {
		return
	}
	code, errorType := streamTransportFailure(ss.EndReason)
	if code != "" {
		streamInfo["error_code"] = code
		streamInfo["error_type"] = errorType
	}
}

// streamTransportFailure 把传输层的结束原因翻译成错误码和错误类型。协议层给出
// 的错误码优先，这里只兜住没有协议错误的失败
func streamTransportFailure(reason relaycommon.StreamEndReason) (string, string) {
	upstream := string(types.ErrorTypeUpstreamError)
	switch reason {
	case relaycommon.StreamEndReasonTimeout:
		return "stream_timeout", upstream
	case relaycommon.StreamEndReasonScannerErr:
		return "stream_aborted", upstream
	case relaycommon.StreamEndReasonPingFail:
		return "stream_ping_failed", upstream
	case relaycommon.StreamEndReasonClientGone:
		return "client_gone", string(types.ErrorTypeNewAPIError)
	case relaycommon.StreamEndReasonPanic:
		return "stream_panic", string(types.ErrorTypeNewAPIError)
	default:
		return "", ""
	}
}
