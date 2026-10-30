package openai

import (
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// upstreamStreamErrorFromUpstream 构造上游流失败时返回给客户端的错误。cyber_policy 拒绝是
// 上游按内容判定的，所以按客户端错误上报，而且不换渠道重试，与 sub2api 的处理一致
func upstreamStreamErrorFromUpstream(oaiErr *types.OpenAIError) *types.NewAPIError {
	if hit, _ := dto.DetectCyberPolicy(oaiErr); hit {
		return types.WithOpenAIError(*oaiErr, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return types.WithOpenAIError(*oaiErr, http.StatusInternalServerError)
}
