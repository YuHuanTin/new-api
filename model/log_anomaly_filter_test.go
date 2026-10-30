package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

// 错误码来自上游，必须永远当字面量处理：LIKE 元字符全部转义，不许变成通配符
func TestEscapeLogOtherLiteral(t *testing.T) {
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(originalLogDatabaseType)
	})

	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	assert.Equal(t, `x' OR 1=1 -- !_!%`, escapeLogOtherLiteral(`x' OR 1=1 -- _%`))
	assert.Equal(t, `!_!%`, escapeLogOtherLiteral(`_%`))

	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	assert.Equal(t, `a\\\_\%`, escapeLogOtherLiteral(`a\_%`))
}

// 异常管理的筛选条件全部通过参数绑定传给数据库，用户输入不会进入 SQL 文本；
// FailedOnly 的 OR 条件还要被括号包住，避免和后面的 AND 条件串味
func TestApplyAnomalyFilterBindsUserInputAsParameters(t *testing.T) {
	originalLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		common.SetLogDatabaseType(originalLogDatabaseType)
	})
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)

	dummyDB, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)

	code := `x' OR 1=1 -- _%`
	var rows []Log
	tx, err := LogFilter{FailedOnly: true, ErrorCode: code, Username: "alice"}.apply(dummyDB)
	require.NoError(t, err)
	tx.Find(&rows)

	sqlText := tx.Statement.SQL.String()
	assert.NotContains(t, sqlText, "1=1", "user input must never reach the SQL text")
	assert.NotContains(t, sqlText, code)
	assert.Contains(t, sqlText, "(logs.type = ? OR logs.other LIKE ? ESCAPE '!')")
	assert.Contains(t, sqlText, "logs.username = ?")

	require.Len(t, tx.Statement.Vars, 4)
	assert.Equal(t, LogTypeError, tx.Statement.Vars[0])
	assert.Equal(t, `%"status":"error"%`, tx.Statement.Vars[1])
	assert.Equal(t, `%"error_code":"x' OR 1=1 -- !_!%"%`, tx.Statement.Vars[2])
	assert.Equal(t, "alice", tx.Statement.Vars[3])
}
