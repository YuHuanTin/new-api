package service

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCyberGuardTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB, previousType := model.DB, common.MainDatabaseType()
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
	})

	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := database.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.UserSession{}))
	model.DB = database
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	return database
}

func enableCyberGuard(t *testing.T, threshold int, minutes int) {
	t.Helper()
	setting := operation_setting.GetCyberSetting()
	previousAutoBan := setting.AutoBan
	previousThreshold, previousWindow := setting.Threshold, setting.WindowMinutes
	t.Cleanup(func() {
		setting.AutoBan = previousAutoBan
		setting.Threshold, setting.WindowMinutes = previousThreshold, previousWindow
	})
	setting.AutoBan = true
	setting.Threshold = threshold
	setting.WindowMinutes = minutes
}

func newCyberRelayInfo(userId int) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{UserId: userId}
	info.CyberPolicyViolation = true
	// 开关在渠道上，默认全关；测试里显式给这个渠道打开
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelSetting: dto.ChannelSettings{CyberGuardEnabled: true},
	}
	return info
}

func cyberPolicyError() *types.NewAPIError {
	return types.WithOpenAIError(types.OpenAIError{
		Code:    dto.CyberPolicyErrorCode,
		Message: "Request blocked by content policy.",
	}, http.StatusBadRequest)
}

func TestRecordCyberPolicyViolationDisablesUserBeyondThreshold(t *testing.T) {
	database := setupCyberGuardTest(t)
	enableCyberGuard(t, 3, 10)
	user := &model.User{Username: "cyber-user", Status: common.UserStatusEnabled, Role: common.RoleCommonUser, Quota: 123456, UsedQuota: 100}
	require.NoError(t, database.Create(user).Error)
	cyberGuardState.reset(user.Id)
	t.Cleanup(func() { cyberGuardState.reset(user.Id) })

	// threshold=3 表示窗口内允许 3 次，第 4 次才禁用
	for attempt := 1; attempt <= 4; attempt++ {
		RecordCyberPolicyViolation(newCyberRelayInfo(user.Id))
		loaded, err := model.GetUserById(user.Id, true)
		require.NoError(t, err)
		if attempt <= 3 {
			assert.Equal(t, common.UserStatusEnabled, loaded.Status, "达到阈值的次数仍在允许范围内")
			continue
		}
		assert.Equal(t, common.UserStatusDisabled, loaded.Status, "超过阈值后账号应被禁用")
		assert.Equal(t, 123456, loaded.Quota, "禁用不应清空余额")
		assert.Equal(t, 100, loaded.UsedQuota, "禁用不应改写已用额度")
	}
}

// 清退函数要区分「真的处置了」和「没动账号」，否则管理员会收到一封说自己被禁用的邮件
func TestBanCyberPolicyUserReportsWhetherItActed(t *testing.T) {
	database := setupCyberGuardTest(t)
	admin := &model.User{Username: "acted-admin", AffCode: "acted-admin-aff", Status: common.UserStatusEnabled, Role: common.RoleAdminUser}
	require.NoError(t, database.Create(admin).Error)
	banned, err := banCyberPolicyUser(newCyberRelayInfo(admin.Id))
	require.NoError(t, err)
	assert.False(t, banned, "管理员没有被禁用，不能当成清退成功")

	user := &model.User{Username: "acted-user", AffCode: "acted-user-aff", Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, database.Create(user).Error)
	banned, err = banCyberPolicyUser(newCyberRelayInfo(user.Id))
	require.NoError(t, err)
	assert.True(t, banned)
}

func TestRecordCyberPolicyViolationKeepsRootAccount(t *testing.T) {
	database := setupCyberGuardTest(t)
	enableCyberGuard(t, 1, 10)
	root := &model.User{Username: "cyber-root", Status: common.UserStatusEnabled, Role: common.RoleRootUser}
	require.NoError(t, database.Create(root).Error)
	cyberGuardState.reset(root.Id)
	t.Cleanup(func() { cyberGuardState.reset(root.Id) })

	// 第 2 次超过阈值，才会走到「是否可清退」的判断
	RecordCyberPolicyViolation(newCyberRelayInfo(root.Id))
	RecordCyberPolicyViolation(newCyberRelayInfo(root.Id))

	loaded, err := model.GetUserById(root.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusEnabled, loaded.Status, "超级管理员不能被自动清退")
}

func TestRecordCyberPolicyViolationKeepsAdminAccount(t *testing.T) {
	database := setupCyberGuardTest(t)
	enableCyberGuard(t, 1, 10)
	admin := &model.User{Username: "cyber-admin", Status: common.UserStatusEnabled, Role: common.RoleAdminUser}
	require.NoError(t, database.Create(admin).Error)
	cyberGuardState.reset(admin.Id)
	t.Cleanup(func() { cyberGuardState.reset(admin.Id) })

	RecordCyberPolicyViolation(newCyberRelayInfo(admin.Id))
	RecordCyberPolicyViolation(newCyberRelayInfo(admin.Id))

	loaded, err := model.GetUserById(admin.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusEnabled, loaded.Status, "管理员不能被自动清退")
}

func TestRecordCyberPolicyViolationIgnoresOtherErrors(t *testing.T) {
	database := setupCyberGuardTest(t)
	enableCyberGuard(t, 1, 10)
	user := &model.User{Username: "quiet-user", Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, database.Create(user).Error)
	cyberGuardState.reset(user.Id)
	t.Cleanup(func() { cyberGuardState.reset(user.Id) })

	info := &relaycommon.RelayInfo{
		UserId:      user.Id,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{CyberGuardEnabled: true}},
	}
	info.LastError = types.NewErrorWithStatusCode(io.ErrUnexpectedEOF, types.ErrorCodeBadResponse, http.StatusBadGateway)
	RecordCyberPolicyViolation(info)

	loaded, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusEnabled, loaded.Status, "非 cyber_policy 的失败不应触发清退")
}

func TestRecordCyberPolicyViolationCountsOnlyCyberPolicyErrors(t *testing.T) {
	database := setupCyberGuardTest(t)
	enableCyberGuard(t, 2, 10)
	user := &model.User{Username: "counted-user", Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, database.Create(user).Error)
	cyberGuardState.reset(user.Id)
	t.Cleanup(func() { cyberGuardState.reset(user.Id) })

	info := &relaycommon.RelayInfo{UserId: user.Id}
	info.LastError = cyberPolicyError()
	RecordCyberPolicyViolation(info)

	loaded, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusEnabled, loaded.Status, "一次拒绝还没到阈值")
}

// 开关在渠道上：没给这个渠道打开时，即使拒绝次数远超阈值也不处置
func TestRecordCyberPolicyViolationSkipsChannelsWithoutTheSwitch(t *testing.T) {
	database := setupCyberGuardTest(t)
	enableCyberGuard(t, 1, 10)
	user := &model.User{Username: "unarmed-channel-user", Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, database.Create(user).Error)
	cyberGuardState.reset(user.Id)
	t.Cleanup(func() { cyberGuardState.reset(user.Id) })

	info := &relaycommon.RelayInfo{UserId: user.Id}
	info.CyberPolicyViolation = true
	RecordCyberPolicyViolation(info)

	loaded, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusEnabled, loaded.Status, "渠道未开启 cyber 策略管控时不应计数")
}

func TestDetectCyberPolicyCodeIgnoresCaseAndSpaces(t *testing.T) {
	assert.True(t, dto.IsCyberPolicyCode("cyber_policy"))
	assert.True(t, dto.IsCyberPolicyCode(" CYBER_POLICY "))
	assert.False(t, dto.IsCyberPolicyCode("bad_request"))
}

func TestCyberGuardWindowDropsOldHits(t *testing.T) {
	guard := cyberGuard{hits: map[int][]int64{}}
	now := time.Now().Unix()
	assert.Equal(t, 1, guard.record(7, now, 600))
	assert.Equal(t, 2, guard.record(7, now, 600))
	assert.Equal(t, 1, guard.record(7, now+700, 600), "窗口外的拒绝不再计入")
}

// 清退通知要说清楚被清退的事实和联系管理员的方式
func TestCyberPolicyBanNoticeTellsTheUserWhy(t *testing.T) {
	subject, content := cyberPolicyBanNotice("zh-CN", 3, 10)

	assert.Equal(t, "您的账号已被禁用", subject)
	assert.Contains(t, content, "3")
	assert.Contains(t, content, "10")
	assert.Contains(t, content, "联系站点管理员")

	englishSubject, englishContent := cyberPolicyBanNotice("en", 3, 10)
	assert.Equal(t, "Your account has been suspended", englishSubject)
	assert.Contains(t, englishContent, "3")
	assert.Contains(t, englishContent, "contact the site administrator")
}
