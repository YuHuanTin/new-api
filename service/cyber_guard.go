package service

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

// cyberGuard 记录每个用户最近的 cyber_policy 拒绝时刻，用于窗口内计数
// 计数只存在进程内：它服务于"短时间内反复触发"的处置判定，重启后重新计数
// 不会让滥用者获得额外额度
type cyberGuard struct {
	mu      sync.Mutex
	hits    map[int][]int64
	sinceGC int
}

var cyberGuardState = cyberGuard{hits: map[int][]int64{}}

// record 记一次拒绝，返回窗口内的拒绝次数
func (g *cyberGuard) record(userId int, now int64, windowSeconds int64) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	deadline := now - windowSeconds
	kept := make([]int64, 0, len(g.hits[userId])+1)
	for _, at := range g.hits[userId] {
		if at > deadline {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	g.hits[userId] = kept

	// 顺带清掉其它用户已经过期的记录，避免长时间运行后残留
	g.sinceGC++
	if g.sinceGC >= 100 {
		g.sinceGC = 0
		for id, times := range g.hits {
			alive := times[:0]
			for _, at := range times {
				if at > deadline {
					alive = append(alive, at)
				}
			}
			if len(alive) == 0 {
				delete(g.hits, id)
				continue
			}
			g.hits[id] = alive
		}
	}
	return len(kept)
}

// reset 清掉某个用户的计数，用于处置之后避免重复处置
func (g *cyberGuard) reset(userId int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.hits, userId)
}

// RecordCyberPolicyViolation 统计一次 cyber_policy 拒绝。只有开启了该渠道的
// cyber 策略管控才计数；窗口内次数超过阈值时禁用用户账号，并给用户发一封邮件，
// 说明被清退的事实和联系管理员的方式
func RecordCyberPolicyViolation(relayInfo *relaycommon.RelayInfo) {
	if relayInfo == nil || relayInfo.UserId <= 0 {
		return
	}
	// 开关在渠道上：默认全关，只有管理员明确给某个渠道打开后才计数
	if relayInfo.ChannelMeta == nil || !relayInfo.ChannelSetting.CyberGuardEnabled {
		return
	}
	cyberSetting := operation_setting.GetCyberSetting()
	if !relayInfo.CyberPolicyViolation && !isCyberPolicyError(relayInfo.LastError) {
		return
	}

	windowMinutes := operation_setting.NormalizeCyberWindowMinutes(cyberSetting.WindowMinutes)
	threshold := operation_setting.NormalizeCyberThreshold(cyberSetting.Threshold)
	window := time.Duration(windowMinutes) * time.Minute
	count := cyberGuardState.record(relayInfo.UserId, time.Now().Unix(), int64(window.Seconds()))
	common.SysLog(common.LogText("cyber_policy rejection recorded for user %d: %d/%d within %d minutes",
		relayInfo.UserId, count, threshold, windowMinutes))
	// threshold 是窗口内允许的次数，超过（第 threshold+1 次）才处置
	if count <= threshold || !cyberSetting.AutoBan {
		return
	}

	banned, err := banCyberPolicyUser(relayInfo)
	if err != nil {
		// 清退失败时保留计数，下一次拒绝会继续尝试，不会白白重置窗口
		common.SysError(common.LogText("failed to ban user %d for repeated cyber_policy rejections: %s", relayInfo.UserId, err.Error()))
		return
	}
	if !banned {
		// 管理员或已禁用的账号没有真的处置，不能发清退通知
		return
	}
	cyberGuardState.reset(relayInfo.UserId)
	gopool.Go(func() {
		sendCyberPolicyBanNotice(relayInfo, count, windowMinutes)
	})
}

// isCyberPolicyError 判断这次请求的错误是不是 cyber_policy 拒绝。上游给的
// 错误码大小写和空白都可能不同，比较时统一忽略
func isCyberPolicyError(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	return dto.IsCyberPolicyCode(string(err.GetErrorCode()))
}

// banCyberPolicyUser 禁用反复触发 cyber_policy 拒绝的账号，返回是否真的做了处置。
// 管理员和超级管理员不处理，否则一次误判就会让站点失去管理入口
func banCyberPolicyUser(relayInfo *relaycommon.RelayInfo) (bool, error) {
	user, err := model.GetUserById(relayInfo.UserId, true)
	if err != nil {
		return false, err
	}
	if user == nil || user.Status != common.UserStatusEnabled {
		return false, nil
	}
	if user.Role >= common.RoleAdminUser {
		common.SysLog(common.LogText("skip banning privileged user %d for repeated cyber_policy rejections", user.Id))
		return false, nil
	}
	user.Status = common.UserStatusDisabled
	if err := updateUserStatusWithRetry(user); err != nil {
		return false, err
	}
	if err := model.InvalidateUserTokensCache(user.Id); err != nil {
		common.SysLog(common.LogText("failed to invalidate tokens cache for user %d: %s", user.Id, err.Error()))
	}
	common.SysLog(common.LogText("user %d banned for repeated cyber_policy rejections", user.Id))
	return true, nil
}

// updateUserStatusWithRetry 保存禁用状态。SQLite 只允许一个写入者，清退发生在
// 请求收尾、日志还在写的时候，偶发 database is locked；短暂重试就能过去
func updateUserStatusWithRetry(user *model.User) error {
	var err error
	for attempt := range 5 {
		if err = user.Update(false); err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "database is locked") {
			return err
		}
		time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
	}
	return err
}

// cyberPolicyBanNotice 拼出清退通知的标题和正文，用用户保存的语言
func cyberPolicyBanNotice(lang string, count int, minutes int) (string, string) {
	subject := i18n.Translate(lang, i18n.MsgNotifyCyberBanSubject)
	content := i18n.Translate(lang, i18n.MsgNotifyCyberBanContent, map[string]any{
		"Count":   count,
		"Minutes": minutes,
		"System":  common.SystemName,
	})
	return subject, content
}

// sendCyberPolicyBanNotice 给用户发邮件，告知账号已被清退以及联系管理员的方式
func sendCyberPolicyBanNotice(relayInfo *relaycommon.RelayInfo, count int, minutes int) {
	receiver := relayInfo.UserSetting.NotificationEmail
	if receiver == "" {
		receiver = relayInfo.UserEmail
	}
	if receiver == "" {
		common.SysLog(common.LogText("user %d has no email, skip sending cyber_policy ban notice", relayInfo.UserId))
		return
	}
	subject, content := cyberPolicyBanNotice(relayInfo.UserSetting.Language, count, minutes)
	if err := common.SendEmail(subject, receiver, content); err != nil {
		common.SysError(common.LogText("failed to send cyber_policy ban notice to user %d: %s", relayInfo.UserId, err.Error()))
	}
}
