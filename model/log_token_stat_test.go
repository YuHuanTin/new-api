package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLogTokenStatAddMatchesInputAccounting(t *testing.T) {
	tests := []struct {
		name       string
		log        Log
		wantTotal  int64
		wantInput  int64
		wantCached int64
		wantRate   float64
	}{
		{
			name:       "claude adds cache reads and writes to input",
			log:        Log{PromptTokens: 2, CompletionTokens: 9255, Other: `{"claude":true,"cache_tokens":46137,"cache_creation_tokens":3736}`},
			wantTotal:  59130,
			wantInput:  49875,
			wantCached: 46137,
			wantRate:   92.5,
		},
		{
			name:       "split 5m and 1h cache writes replace the legacy total",
			log:        Log{Other: `{"claude":true,"cache_creation_tokens_5m":10,"cache_creation_tokens_1h":5,"cache_creation_tokens":999}`},
			wantTotal:  15,
			wantInput:  15,
			wantCached: 0,
			wantRate:   0,
		},
		{
			name:       "non-claude prompt excludes cache writes and image cache is a subset",
			log:        Log{PromptTokens: 300, CompletionTokens: 150, Other: `{"cache_tokens":75,"image_cache_tokens":25,"cache_creation_tokens":500}`},
			wantTotal:  450,
			wantInput:  300,
			wantCached: 75,
			wantRate:   25,
		},
		{
			name:       "cache rate is capped at 100",
			log:        Log{PromptTokens: 10, Other: `{"cache_tokens":50}`},
			wantTotal:  10,
			wantInput:  10,
			wantCached: 50,
			wantRate:   100,
		},
		{
			name:       "unparseable other counts only prompt and completion tokens",
			log:        Log{PromptTokens: 7, CompletionTokens: 3, Other: "not json"},
			wantTotal:  10,
			wantInput:  7,
			wantCached: 0,
			wantRate:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stat LogTokenStat
			stat.Add(&tt.log)
			assert.Equal(t, tt.wantTotal, stat.TotalTokens)
			assert.Equal(t, tt.wantInput, stat.InputTokens)
			assert.Equal(t, tt.wantCached, stat.CachedInputTokens)
			assert.InDelta(t, tt.wantRate, stat.CacheRate(), 0.01)
		})
	}
}

func TestSumUserLogTokensScopesToUserAndTimeRange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/logs.db"), &gorm.Config{})
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&Log{}))
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })

	now := common.GetTimestamp()
	logs := []Log{
		{UserId: 1, CreatedAt: now - 60, Type: LogTypeConsume, PromptTokens: 100, CompletionTokens: 50, Other: `{"cache_tokens":25}`},
		{UserId: 1, CreatedAt: now - 10*24*3600, Type: LogTypeConsume, PromptTokens: 999, CompletionTokens: 1},
		{UserId: 2, CreatedAt: now - 60, Type: LogTypeConsume, PromptTokens: 500, CompletionTokens: 5},
	}
	require.NoError(t, db.Create(&logs).Error)

	stat, err := SumUserLogTokens(context.Background(), 1, LogFilter{StartTimestamp: now - 3600})
	require.NoError(t, err)
	assert.Equal(t, int64(150), stat.TotalTokens)
	assert.Equal(t, int64(100), stat.InputTokens)
	assert.Equal(t, int64(25), stat.CachedInputTokens)
}
