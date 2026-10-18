package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// LogTokenStat totals the token usage of the logs passed to Add. For Claude the
// prompt tokens exclude cache reads and writes, so they are added back to get
// the input that the cache rate is measured against. Cache writes are not cache
// hits. Image cache tokens are a subset of cache tokens.
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
	cacheWriteTokens := otherTokenCount(other, "cache_creation_tokens_5m") + otherTokenCount(other, "cache_creation_tokens_1h")
	if cacheWriteTokens == 0 {
		cacheWriteTokens = otherTokenCount(other, "cache_creation_tokens")
	}

	inputTokens := promptTokens
	if other["claude"] == true {
		inputTokens += cachedTokens + cacheWriteTokens
	}
	s.InputTokens += inputTokens
	s.TotalTokens += inputTokens + completionTokens
	s.CachedInputTokens += cachedTokens
}

// CacheRate is the percentage of input tokens served from cache, capped at 100.
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

// SumAllLogTokens totals the token usage of every log matching filter.
func SumAllLogTokens(ctx context.Context, filter LogFilter) (LogTokenStat, error) {
	tx, err := filter.apply(LOG_DB.WithContext(ctx))
	if err != nil {
		return LogTokenStat{}, err
	}
	return sumLogTokens(tx)
}

// SumUserLogTokens is SumAllLogTokens restricted to one user's logs.
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
