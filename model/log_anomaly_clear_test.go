package model

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 清理只删管理员可见的抓包现场，日志里的其它字段原样保留
func TestStripLogAnomalyCaptureRemovesOnlyTheCapture(t *testing.T) {
	other := `{"public":{"cache_tokens":10},"admin_info":{"anomaly":"gz:abc","request_policy":[{"decision":{"action":"success"}}]}}`
	stripped, changed := stripLogAnomalyCapture(other)
	require.True(t, changed)
	assert.NotContains(t, stripped, "anomaly")
	assert.Contains(t, stripped, "cache_tokens")
	assert.Contains(t, stripped, "request_policy")

	_, changed = stripLogAnomalyCapture(`{"public":{"cache_tokens":10}}`)
	assert.False(t, changed, "没有抓包数据的行不改动")

	_, changed = stripLogAnomalyCapture("not json")
	assert.False(t, changed, "解析不了的 other 不改动")
}

func TestClearLogAnomalyCaptureKeepsTheLogRow(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	log := &Log{
		Type:    LogTypeError,
		Content: "boom",
		Other:   `{"public":{"cache_tokens":10},"admin_info":{"anomaly":"gz:abc"}}`,
	}
	require.NoError(t, db.Create(log).Error)

	cleared, err := ClearLogAnomalyCapture([]LogLocator{{Id: log.Id}})
	require.NoError(t, err)
	assert.Equal(t, 1, cleared)

	var stored Log
	require.NoError(t, db.First(&stored, log.Id).Error)
	assert.NotContains(t, stored.Other, "anomaly")
	assert.NotContains(t, stored.Other, logOtherAnomalyCapturedKey, "清理后不留抓包标记")
	assert.Contains(t, stored.Other, "cache_tokens")
	assert.Equal(t, "boom", stored.Content, "日志记录本身保留")

	cleared, err = ClearLogAnomalyCapture([]LogLocator{{Id: log.Id}})
	require.NoError(t, err)
	assert.Equal(t, 0, cleared, "已清理过的行不再改动")
}

// 列表接口去掉抓包现场，详情接口仍能按需读到；其它管理员可见字段不受影响
func TestGetLogAnomalyCaptureReadsOnDemand(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	other := NewLogOther()
	other.SetAdmin(logOtherAnomalyKey, common.CompressText(`{"request_url":"https://up.example.com","upstream_status":502}`))
	other.SetAdmin("request_policy", []map[string]any{{"rule_name": "demo"}})
	log := &Log{Type: LogTypeError, Content: "boom", Other: other.JSONString()}
	require.NoError(t, db.Create(log).Error)

	listed := []*Log{{Id: log.Id, Other: log.Other}}
	StripLogAnomalyCapture(listed)
	listedOther, err := common.StrToMap(listed[0].Other)
	require.NoError(t, err)
	listedAdminInfo, ok := listedOther[logOtherAdminInfoKey].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, listedAdminInfo, logOtherAnomalyKey, "列表不返回抓包现场")
	assert.Equal(t, true, listedAdminInfo[logOtherAnomalyCapturedKey], "列表保留有抓包的标记")
	assert.Contains(t, listedAdminInfo, "request_policy", "其它管理员字段留在列表里")

	anomaly, found, err := GetLogAnomalyCapture(LogLocator{Id: log.Id})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, common.CompressText(`{"request_url":"https://up.example.com","upstream_status":502}`), anomaly)

	for _, storedCapture := range []string{
		`{"request_body":"old secret","large_integer":9007199254740993}`,
		`"old plain text"`,
		`null`,
	} {
		legacyOther := `{"admin_info":{"anomaly":` + storedCapture + `}}`
		require.NoError(t, db.Model(&Log{}).Where("id = ?", log.Id).Update("other", legacyOther).Error)
		encoded, found, err := GetLogAnomalyCapture(LogLocator{Id: log.Id})
		require.NoError(t, err)
		require.True(t, found)
		switch storedCapture {
		case `null`:
			assert.Empty(t, encoded)
		case `"old plain text"`:
			assert.Equal(t, "old plain text", common.DecompressText(encoded))
		default:
			assert.Equal(t, storedCapture, common.DecompressText(encoded))
		}
		var unchanged Log
		require.NoError(t, db.First(&unchanged, log.Id).Error)
		assert.Equal(t, legacyOther, unchanged.Other, "reading legacy records must not rewrite them")
	}

	_, found, err = GetLogAnomalyCapture(LogLocator{Id: log.Id + 1000})
	require.NoError(t, err)
	assert.False(t, found, "不存在的日志返回 found=false")
}

// 清理全部时每一行都要清到；没有抓包的行不动，解析不了的行也不能让循环卡住
func TestClearAllLogAnomalyCaptureClearsEveryRow(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	first := &Log{Type: LogTypeError, Content: "first", Other: `{"public":{"cache_tokens":10},"admin_info":{"anomaly":"gz:abc"}}`}
	second := &Log{Type: LogTypeError, Content: "second", Other: `{"admin_info":{"anomaly":"gz:def","request_policy":[{"rule_name":"demo"}]}}`}
	plain := &Log{Type: LogTypeError, Content: "plain", Other: `{"public":{"cache_tokens":5}}`}
	broken := &Log{Type: LogTypeError, Content: "broken", Other: `{"admin_info":{"anomaly":`}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)
	require.NoError(t, db.Create(plain).Error)
	require.NoError(t, db.Create(broken).Error)

	cleared, err := ClearAllLogAnomalyCapture()
	require.NoError(t, err)
	assert.EqualValues(t, 2, cleared, "只统计真正清掉的行")

	var stored []Log
	require.NoError(t, db.Order("id").Find(&stored).Error)
	require.Len(t, stored, 4)
	assert.NotContains(t, stored[0].Other, logOtherAnomalyKey)
	assert.Contains(t, stored[0].Other, "cache_tokens", "其它字段保留")
	assert.NotContains(t, stored[1].Other, logOtherAnomalyKey)
	assert.Contains(t, stored[1].Other, "request_policy", "其它管理员字段保留")
	assert.Equal(t, plain.Other, stored[2].Other, "没有抓包的行不动")
	assert.Contains(t, stored[3].Other, "anomaly", "解析不了的行保持原样")

	cleared, err = ClearAllLogAnomalyCapture()
	require.NoError(t, err)
	assert.EqualValues(t, 0, cleared, "重复清理不再改动")
}

// 统计抓包数据时只算 admin_info.anomaly 的字符串内容，按字节数累加
func TestGetLogAnomalyCaptureUsageCountsAnomalyBytes(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	first := `{"request_url":"https://up.example.com"}`
	second := `{"request_url":"https://up.example.com","note":"中文"}`
	for _, anomaly := range []string{first, second} {
		other := NewLogOther()
		other.SetAdmin(logOtherAnomalyKey, anomaly)
		require.NoError(t, db.Create(&Log{Type: LogTypeError, Other: other.JSONString()}).Error)
	}
	// 旧数据的现场是对象而不是压缩字符串，按原始 JSON 文本计长度
	legacy := `{"request_url":"https://old.example.com"}`
	legacyOther := NewLogOther()
	legacyOther.SetAdmin(logOtherAnomalyKey, json.RawMessage(legacy))
	require.NoError(t, db.Create(&Log{Type: LogTypeError, Other: legacyOther.JSONString()}).Error)
	require.NoError(t, db.Create(&Log{Type: LogTypeError, Other: `{"public":{"cache_tokens":10}}`}).Error)
	require.NoError(t, db.Create(&Log{Type: LogTypeError, Other: `{"admin_info":{"anomaly":`}).Error)

	usage, err := GetLogAnomalyCaptureUsage()
	require.NoError(t, err)
	assert.EqualValues(t, 3, usage.Records, "只统计有抓包的行")
	assert.EqualValues(t, len(first)+len(second)+len(legacy), usage.Bytes, "按抓包内容的字节数累加")
}

// 按保留天数清理时只动更早的日志，新日志的抓包保留
func TestPurgeExpiredLogAnomalyCaptureKeepsNewerRows(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	old := &Log{Type: LogTypeError, CreatedAt: 100, Other: `{"admin_info":{"anomaly":"gz:old"}}`}
	fresh := &Log{Type: LogTypeError, CreatedAt: 200, Other: `{"admin_info":{"anomaly":"gz:new"}}`}
	require.NoError(t, db.Create(old).Error)
	require.NoError(t, db.Create(fresh).Error)

	cleared, err := PurgeExpiredLogAnomalyCapture(150)
	require.NoError(t, err)
	assert.EqualValues(t, 1, cleared)

	var stored []Log
	require.NoError(t, db.Order("id").Find(&stored).Error)
	assert.NotContains(t, stored[0].Other, logOtherAnomalyKey, "过期日志的抓包被清掉")
	assert.Contains(t, stored[1].Other, logOtherAnomalyKey, "未过期日志的抓包保留")
}

// 清理中途失败时整批回滚，不能留下清了一半的状态
func TestClearLogAnomalyCaptureRollsBackOnFailure(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	first := &Log{Type: LogTypeError, Content: "first", Other: `{"admin_info":{"anomaly":"gz:aaa"}}`}
	second := &Log{Type: LogTypeError, Content: "second", Other: `{"admin_info":{"anomaly":"gz:bbb"}}`}
	require.NoError(t, db.Create(first).Error)
	require.NoError(t, db.Create(second).Error)

	updates := 0
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail_second_clear", func(tx *gorm.DB) {
		updates++
		if updates == 2 {
			_ = tx.AddError(errors.New("boom"))
		}
	}))

	_, err = ClearLogAnomalyCapture([]LogLocator{{Id: first.Id}, {Id: second.Id}})
	require.Error(t, err, "第二行写失败时接口必须报错")

	var stored []Log
	require.NoError(t, db.Order("id").Find(&stored).Error)
	require.Len(t, stored, 2)
	assert.Contains(t, stored[0].Other, "anomaly", "第一行的清理必须回滚")
	assert.Contains(t, stored[1].Other, "anomaly")
}

// ClickHouse 的 log id 恒为 0，必须用建表排序键 (created_at, request_id) 定位
func TestLogLocatorUsesClickHouseKey(t *testing.T) {
	previousType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(previousType) })

	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	clickHouse := LogLocator{Id: 5, RequestId: "req-1", CreatedAt: 123}
	assert.True(t, clickHouse.Valid())
	assert.False(t, LogLocator{Id: 5}.Valid(), "只有 id 的定位在 ClickHouse 下不可用")
	condition, args := clickHouse.condition()
	assert.Equal(t, "created_at = ? AND request_id = ?", condition)
	assert.Equal(t, []any{int64(123), "req-1"}, args)

	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	assert.True(t, LogLocator{Id: 5}.Valid())
	condition, args = LogLocator{Id: 5}.condition()
	assert.Equal(t, "id = ?", condition)
	assert.Equal(t, []any{5}, args)
}

// 导出不携带抓包现场，避免每行解压 4 MB 把导出文件撑大
func TestStreamAllLogsStripsCapture(t *testing.T) {
	previousDB := LOG_DB
	previousType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousDB
		common.SetLogDatabaseType(previousType)
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Log{}))
	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	log := &Log{
		Type:    LogTypeError,
		Content: "boom",
		Other:   `{"public":{"cache_tokens":10},"admin_info":{"anomaly":"gz:abc","request_policy":[]}}`,
	}
	require.NoError(t, db.Create(log).Error)

	var exported string
	require.NoError(t, StreamAllLogs(context.Background(), LogFilter{}, common.RoleAdminUser, func(row *Log) error {
		exported = row.Other
		return nil
	}))
	assert.NotContains(t, exported, "anomaly")
	assert.Contains(t, exported, "cache_tokens", "其它字段照常导出")
	assert.Contains(t, exported, "request_policy")
}
