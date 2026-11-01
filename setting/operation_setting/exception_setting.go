package operation_setting

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/setting/config"
)

// ExceptionSetting 是异常管理的抓包参数
type ExceptionSetting struct {
	// CaptureRetentionDays 抓包数据保留天数，超过后由后台任务清理
	CaptureRetentionDays int `json:"capture_retention_days"`
}

const (
	DefaultCaptureRetentionDays = 3
	MaxCaptureRetentionDays     = 3650
)

var exceptionSetting = ExceptionSetting{
	CaptureRetentionDays: DefaultCaptureRetentionDays,
}

func init() {
	config.GlobalConfig.Register("exception_setting", &exceptionSetting)
}

// GetExceptionSetting 返回当前设置，越界值在读取处用 Normalize 夹回可用范围
func GetExceptionSetting() *ExceptionSetting {
	return &exceptionSetting
}

func NormalizeCaptureRetentionDays(value int) int {
	if value < 1 {
		return DefaultCaptureRetentionDays
	}
	if value > MaxCaptureRetentionDays {
		return MaxCaptureRetentionDays
	}
	return value
}

// ValidateCaptureRetentionDays 校验保留天数，供设置接口使用
func ValidateCaptureRetentionDays(value string) error {
	days, err := strconv.Atoi(value)
	if err != nil || days < 1 || days > MaxCaptureRetentionDays {
		return fmt.Errorf("capture retention days must be between 1 and %d", MaxCaptureRetentionDays)
	}
	return nil
}
