package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// LogTokenStat 按日志累计完整 token 用量，供日志页统计和排行榜共用。
// Claude 的 prompt_tokens 不含缓存读写，计算总输入时需加回；
// 缓存写入不算命中，图片缓存是缓存读取的子集
type LogTokenStat struct {
	TotalTokens       int64
	InputTokens       int64
	CachedInputTokens int64
}

func (s *LogTokenStat) Add(log *Log) {
	var other map[string]any
	if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
		other = nil
	}
	promptTokens := max(int64(log.PromptTokens), 0)
	completionTokens := max(int64(log.CompletionTokens), 0)
	cachedTokens := max(otherTokenCount(other, "cache_tokens"), otherTokenCount(other, "image_cache_tokens"))
	cacheWriteTokens := otherTokenCount(other, "cache_write_tokens")
	if cacheWriteTokens == 0 {
		cacheWriteTokens = otherTokenCount(other, "cache_creation_tokens_5m") + otherTokenCount(other, "cache_creation_tokens_1h")
		if cacheWriteTokens == 0 {
			cacheWriteTokens = otherTokenCount(other, "cache_creation_tokens")
		}
	}

	// 已归一化的总输入包含缓存读写，无需再累加
	// 旧版 Claude 日志的 prompt_tokens 不含缓存读写，其他协议已包含
	inputTokens := otherTokenCount(other, "input_tokens_total")
	if inputTokens == 0 {
		inputTokens = promptTokens
		if other["claude"] == true {
			inputTokens += cachedTokens + cacheWriteTokens
		}
	}
	s.InputTokens += inputTokens
	s.TotalTokens += inputTokens + completionTokens
	s.CachedInputTokens += cachedTokens
}

// CacheRate 返回缓存读取占总输入的百分比，上限为 100%
func (s LogTokenStat) CacheRate() float64 {
	if s.InputTokens <= 0 {
		return 0
	}
	return min(100, float64(s.CachedInputTokens)/float64(s.InputTokens)*100)
}

func otherTokenCount(other map[string]any, key string) int64 {
	value, _ := other[key].(float64)
	if value <= 0 {
		return 0
	}
	return int64(value)
}

// SumAllLogTokens 汇总所有符合筛选条件的日志 token 用量
func SumAllLogTokens(ctx context.Context, filter LogFilter) (LogTokenStat, error) {
	tx, err := filter.apply(LOG_DB.WithContext(ctx))
	if err != nil {
		return LogTokenStat{}, err
	}
	return sumLogTokens(tx)
}

// SumUserLogTokens 仅汇总指定用户符合筛选条件的日志 token 用量
func SumUserLogTokens(ctx context.Context, userId int, filter LogFilter) (LogTokenStat, error) {
	tx, err := filter.apply(LOG_DB.WithContext(ctx).Where("logs.user_id = ?", userId))
	if err != nil {
		return LogTokenStat{}, err
	}
	return sumLogTokens(tx)
}

func sumLogTokens(tx *gorm.DB) (LogTokenStat, error) {
	var stat LogTokenStat
	err := streamLogs(tx, "logs.id desc", func(log *Log) error {
		stat.Add(log)
		return nil
	})
	return stat, err
}
