package model

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
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
			name:       "normalized cache writes include tokens outside the duration breakdown",
			log:        Log{PromptTokens: 2, CompletionTokens: 100, Other: `{"claude":true,"cache_tokens":200000,"cache_write_tokens":5000,"cache_creation_tokens_5m":3000,"cache_creation_tokens_1h":736}`},
			wantTotal:  205102,
			wantInput:  205002,
			wantCached: 200000,
			wantRate:   97.56,
		},
		{
			name:       "normalized input already includes cache reads and writes",
			log:        Log{PromptTokens: 2, CompletionTokens: 100, Other: `{"claude":true,"input_tokens_total":203002,"cache_tokens":200000,"cache_write_tokens":3000}`},
			wantTotal:  203102,
			wantInput:  203002,
			wantCached: 200000,
			wantRate:   98.52,
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

func TestRankingsUsesLogTokenTotals(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var mainDSN, logDSN string
			switch dialect {
			case "sqlite":
				mainDSN, logDSN = "local", "local"
			case "mysql":
				mainDSN, logDSN = os.Getenv("TEST_MYSQL_DSN"), os.Getenv("TEST_MYSQL_LOG_DSN")
			case "postgres":
				mainDSN, logDSN = os.Getenv("TEST_POSTGRES_DSN"), os.Getenv("TEST_POSTGRES_LOG_DSN")
			}
			if mainDSN == "" {
				t.Skip("test database DSN is not configured")
			}
			if logDSN == "" {
				logDSN = mainDSN
			}
			previousDB, previousLogDB := DB, LOG_DB
			previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			previousSQLitePath := common.SQLitePath
			previousExport, previousConsume := common.DataExportEnabled, common.LogConsumeEnabled
			CacheQuotaDataLock.Lock()
			previousCache := CacheQuotaData
			CacheQuotaData = make(map[string]*QuotaData)
			CacheQuotaDataLock.Unlock()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMainType, previousLogType)
				initCol()
				common.SQLitePath = previousSQLitePath
				common.DataExportEnabled, common.LogConsumeEnabled = previousExport, previousConsume
				CacheQuotaDataLock.Lock()
				CacheQuotaData = previousCache
				CacheQuotaDataLock.Unlock()
			})
			testDir := t.TempDir()
			common.SQLitePath = filepath.Join(testDir, "quota.db")
			t.Setenv("RANKINGS_TEST_DSN", mainDSN)
			db, mainType, err := chooseDB("RANKINGS_TEST_DSN", false)
			require.NoError(t, err)
			common.SQLitePath = filepath.Join(testDir, "logs.db")
			t.Setenv("RANKINGS_TEST_LOG_DSN", logDSN)
			logDB, logType, err := chooseDB("RANKINGS_TEST_LOG_DSN", true)
			require.NoError(t, err)
			for _, connection := range []*gorm.DB{db, logDB} {
				sqlDB, err := connection.DB()
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			}
			// 测试连接必须指向可清理的临时数据库，避免修改已有部署的数据。
			require.False(t, db.Migrator().HasTable(&User{}))
			require.False(t, db.Migrator().HasTable(&QuotaData{}))
			require.False(t, logDB.Migrator().HasTable(&Log{}))
			t.Cleanup(func() {
				require.NoError(t, logDB.Migrator().DropTable(&Log{}))
				require.NoError(t, db.Migrator().DropTable(&QuotaData{}, &User{}))
			})
			DB, LOG_DB = db, logDB
			common.SetDatabaseTypes(mainType, logType)
			initCol()
			common.DataExportEnabled, common.LogConsumeEnabled = true, true
			require.NoError(t, db.AutoMigrate(&User{}, &QuotaData{}))
			require.NoError(t, logDB.AutoMigrate(&Log{}))
			require.NoError(t, db.Create(&User{Id: 1, Username: "rankings", Setting: "{}"}).Error)

			for _, tc := range []struct {
				claude bool
				prompt int
			}{
				{true, 2},
				{false, 203002},
			} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Set("username", "rankings")
				other := NewLogOther()
				other.SetPublic("claude", tc.claude)
				other.SetPublic("cache_tokens", 200000)
				other.SetPublic("cache_creation_tokens", 3000)
				other.SetPublic("cache_write_tokens", 3000)
				RecordConsumeLog(c, 1, RecordConsumeLogParams{
					ModelName: "mixed-protocol-model", PromptTokens: tc.prompt, CompletionTokens: 100,
					TokenId: 1, ChannelId: 1, Group: "default", Quota: 7, Other: other,
				})
				SaveQuotaDataCache()
			}
			stat, err := SumAllLogTokens(context.Background(), LogFilter{Type: LogTypeConsume})
			require.NoError(t, err)
			assert.Equal(t, int64(406204), stat.TotalTokens)
			totals, err := GetRankingQuotaTotals(0, 0)
			require.NoError(t, err)
			require.Len(t, totals, 1)
			assert.Equal(t, stat.TotalTokens, totals[0].TotalTokens)
			buckets, err := GetRankingQuotaBuckets(0, 0, 3600)
			require.NoError(t, err)
			var bucketTokens int64
			for _, bucket := range buckets {
				bucketTokens += bucket.Tokens
			}
			assert.Equal(t, stat.TotalTokens, bucketTokens)
			var quota QuotaData
			require.NoError(t, db.Model(&QuotaData{}).Select("sum(count) as count, sum(quota) as quota, sum(cache_read_tokens) as cache_read_tokens, sum(cache_creation_tokens) as cache_creation_tokens").Scan(&quota).Error)
			assert.Equal(t, 2, quota.Count)
			assert.Equal(t, 14, quota.Quota)
			assert.Equal(t, int64(400000), quota.CacheReadTokens)
			assert.Equal(t, int64(6000), quota.CacheCreationTokens)
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
