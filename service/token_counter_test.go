package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateRequestTokenDefersExactCountUntilFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevCountToken := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() {
		constant.CountToken = prevCountToken
	})

	req := &dto.GeneralOpenAIRequest{
		Model: "gpt-4o",
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "func check(id int, q float64) int { return id + int(q) } // verify deferred exact count",
			},
		},
	}
	meta := req.GetTokenCountMeta()
	require.NotEmpty(t, meta.CombineText)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-4o")

	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		Request:     req,
	}

	// 入口预估使用零分配启发式估算 + OpenAI 格式开销
	estimated, err := EstimateRequestToken(c, meta, info)
	require.NoError(t, err)
	overhead := meta.ToolsCount*8 + meta.MessagesCount*3 + meta.NameCount*3 + 3
	wantEstimated := tokenkit.Estimate("gpt-4o", meta.CombineText) + overhead
	wantExact := tokenkit.Count("gpt-4o", meta.CombineText) + overhead
	require.NotEqual(t, wantEstimated, wantExact, "测试样本需能区分 Estimate 与精确 Count")
	assert.Equal(t, wantEstimated, estimated)
	assert.Equal(t, wantEstimated, common.GetContextKeyInt(c, constant.ContextKeyPromptTokens))

	// 上游未返回 usage 时读取 GetEstimatePromptTokens，按需升级为精确 tiktoken 计数
	assert.Equal(t, wantExact, info.GetEstimatePromptTokens())
	assert.Equal(t, wantExact, common.GetContextKeyInt(c, constant.ContextKeyPromptTokens))
	// 再次读取直接命中缓存结果
	assert.Equal(t, wantExact, info.GetEstimatePromptTokens())

	// CountRequestToken（例如 count_tokens 接口）始终直接返回精确值
	exact, err := CountRequestToken(c, meta, info)
	require.NoError(t, err)
	assert.Equal(t, wantExact, exact)
}

func TestRelayInfoEstimatePromptTokensResolverLifecycle(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	calls := 0
	info.SetEstimatePromptTokensWithResolver(120, func() int {
		calls++
		return 150
	})

	assert.Equal(t, 150, info.GetEstimatePromptTokens())
	assert.Equal(t, 150, info.GetEstimatePromptTokens())
	assert.Equal(t, 1, calls, "延迟回调应只触发一次")

	// 显式覆盖估算值会清空挂起的延迟回调
	info.SetEstimatePromptTokensWithResolver(10, func() int {
		calls++
		return 999
	})
	info.SetEstimatePromptTokens(42)
	assert.Equal(t, 42, info.GetEstimatePromptTokens())
	assert.Equal(t, 1, calls)
}
