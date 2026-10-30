package dto

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// CyberPolicyErrorCode 是上游 OpenAI Codex 账号用网络安全策略拒绝请求时的错误码
// sub2api 这类网关会先识别出这个错误码，再把上游响应原样转发给 new-api，
// 所以 new-api 必须自己认得同一个错误码
const CyberPolicyErrorCode = "cyber_policy"

// responsesStreamEventError 是不挂在 response 上的裸 error 事件，它的错误码和
// 错误信息直接放在事件顶层
const responsesStreamEventError = "error"

// DetectCyberPolicy 判断上游错误是不是 cyber_policy 拒绝，并返回上游给出的原因
// 查找位置与 sub2api 转发前的检测保持一致：错误码可能在事件上的 error 对象里，
// 可能在 response.error 里，也可能直接挂在事件顶层；比较时忽略大小写和空白
func DetectCyberPolicy(errorField any) (bool, string) {
	err := GetOpenAIError(errorField)
	if err == nil {
		return false, ""
	}
	if !IsCyberPolicyCode(openAIErrorCodeString(err)) {
		return false, ""
	}
	return true, strings.TrimSpace(err.Message)
}

// IsCyberPolicyCode 判断错误码是不是 cyber_policy 拒绝。上游给的大小写和
// 前后空白都可能不同，比较时统一忽略
func IsCyberPolicyCode(code string) bool {
	return strings.EqualFold(strings.TrimSpace(code), CyberPolicyErrorCode)
}

// GetOpenAIError 返回流里携带的上游错误。正常分片没有错误对象，返回 nil
func (c *ChatCompletionsStreamResponse) GetOpenAIError() *types.OpenAIError {
	if c == nil {
		return nil
	}
	err := GetOpenAIError(c.Error)
	if !HasOpenAIError(err) {
		return nil
	}
	return err
}

// GetOpenAIError 返回事件携带的上游错误。错误对象可能挂在事件上（裸 error
// 事件），也可能挂在 response 里；两者都取不到时返回 nil
func (r *ResponsesStreamResponse) GetOpenAIError() *types.OpenAIError {
	if r == nil {
		return nil
	}
	if err := GetOpenAIError(r.Error); HasOpenAIError(err) {
		return err
	}
	if r.Response != nil {
		if err := r.Response.GetOpenAIError(); HasOpenAIError(err) {
			return err
		}
	}
	if r.Type == responsesStreamEventError {
		return &types.OpenAIError{Code: r.Code, Message: r.Message}
	}
	return nil
}

// HasOpenAIError 判断上游错误对象是否真的携带了错误。上游（含 sub2api 这类
// 网关）转发拒绝时常常只带 code 和 message 而没有 type，所以不能拿 type 当
// "是不是错误"的判据，否则拒绝会被当成正常响应丢掉
func HasOpenAIError(err *types.OpenAIError) bool {
	if err == nil {
		return false
	}
	return err.Type != "" || err.Message != "" || openAIErrorCodeString(err) != ""
}

// openAIErrorCodeString 把错误码统一成字符串。上游多数用字符串，也有用数字或
// 对象的，不能因为不是字符串就丢掉
func openAIErrorCodeString(err *types.OpenAIError) string {
	if err == nil || err.Code == nil {
		return ""
	}
	if code, ok := err.Code.(string); ok {
		return code
	}
	return fmt.Sprintf("%v", err.Code)
}
