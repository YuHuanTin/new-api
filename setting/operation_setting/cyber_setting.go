package operation_setting

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/setting/config"
)

// CyberSetting 是 cyber_policy 拒绝的自动处置参数
//
// 上游（sub2api 这类网关）会把上游账号的 cyber_policy 拒绝原样转发，
// new-api 需要自己认得这个拒绝，才能统计次数并在超阈值时清退账号
//
// 是否统计按渠道开启（ChannelSettings.CyberGuardEnabled，默认全关）；
// 这里只放各渠道共用的判定参数
type CyberSetting struct {
	// AutoBan 超阈值时真的禁用用户账号；关掉时只统计不处置
	AutoBan bool `json:"auto_ban"`
	// WindowMinutes 统计窗口（分钟）
	WindowMinutes int `json:"window_minutes"`
	// Threshold 窗口内允许多少次 cyber_policy 拒绝，超过就处置
	Threshold int `json:"threshold"`
}

const (
	DefaultCyberWindowMinutes = 10
	DefaultCyberThreshold     = 3
	MaxCyberWindowMinutes     = 1440
	MaxCyberThreshold         = 1000
)

var cyberSetting = CyberSetting{
	AutoBan:       true,
	WindowMinutes: DefaultCyberWindowMinutes,
	Threshold:     DefaultCyberThreshold,
}

func init() {
	config.GlobalConfig.Register("cyber_setting", &cyberSetting)
}

// GetCyberSetting 返回当前设置。越界取值在读取处用 Normalize* 夹回可用范围，
// 这里不改写全局设置，避免和并发读取产生数据竞争
func GetCyberSetting() *CyberSetting {
	return &cyberSetting
}

func NormalizeCyberWindowMinutes(value int) int {
	if value < 1 {
		return DefaultCyberWindowMinutes
	}
	if value > MaxCyberWindowMinutes {
		return MaxCyberWindowMinutes
	}
	return value
}

func NormalizeCyberThreshold(value int) int {
	if value < 1 {
		return DefaultCyberThreshold
	}
	if value > MaxCyberThreshold {
		return MaxCyberThreshold
	}
	return value
}

// ValidateCyberWindowMinutes 校验统计窗口的取值，供设置接口使用
func ValidateCyberWindowMinutes(value string) error {
	minutes, err := strconv.Atoi(value)
	if err != nil || minutes < 1 || minutes > MaxCyberWindowMinutes {
		return fmt.Errorf("cyber window minutes must be between 1 and %d", MaxCyberWindowMinutes)
	}
	return nil
}

// ValidateCyberThreshold 校验阈值的取值，供设置接口使用
func ValidateCyberThreshold(value string) error {
	threshold, err := strconv.Atoi(value)
	if err != nil || threshold < 1 || threshold > MaxCyberThreshold {
		return fmt.Errorf("cyber threshold must be between 1 and %d", MaxCyberThreshold)
	}
	return nil
}
