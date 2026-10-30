package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// failedStreamStatusPattern 是消耗日志里流状态判定为失败时写入的片段
const failedStreamStatusPattern = `"status":"error"`

// logOtherLikeCondition 生成 other 列的 LIKE 条件，并把 pattern 包成
// %pattern%。pattern 里的用户输入必须先用 escapeLogOtherLiteral 转义；JSON 键
// 这类固定片段保持原样。日志数据库可能是 ClickHouse，转义写法与其它数据库不同
func logOtherLikeCondition(pattern string) (string, string) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return "logs.other LIKE ?", "%" + pattern + "%"
	}
	return "logs.other LIKE ? ESCAPE '!'", "%" + pattern + "%"
}

// escapeLogOtherLiteral 把值里的 LIKE 元字符转义成本方言的字面量，避免上游给
// 的错误码变成通配符。和其它日志筛选不同，这里不接受用户写通配符
func escapeLogOtherLiteral(value string) string {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// ClickHouse 的 LIKE 用反斜杠转义，只认 \% 和 \_
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, "%", `\%`)
		return strings.ReplaceAll(value, "_", `\_`)
	}
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	return strings.ReplaceAll(value, "_", "!_")
}

// applyAnomalyFilter 把「只看失败」和「按错误码」两个条件加到查询上。它们是
// 异常管理的入口条件，和其余日志筛选条件互相独立
func (f LogFilter) applyAnomalyFilter(tx *gorm.DB) *gorm.DB {
	if f.FailedOnly {
		// 失败日志行本身就是失败；消耗日志行看流状态里的 status
		condition, pattern := logOtherLikeCondition(failedStreamStatusPattern)
		tx = tx.Where("logs.type = ? OR "+condition, LogTypeError, pattern)
	}
	if f.ErrorCode != "" {
		condition, pattern := logOtherLikeCondition(
			`"error_code":"` + escapeLogOtherLiteral(f.ErrorCode) + `"`,
		)
		tx = tx.Where(condition, pattern)
	}
	return tx
}
