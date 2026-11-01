package model

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const exceptionOptionPrefix = "exception_setting."

// CurrentExceptionOptions 返回异常管理的当前设置，键带 exception_setting. 前缀
func CurrentExceptionOptions() map[string]string {
	options := map[string]string{}
	for key, value := range config.GlobalConfig.ExportAllConfigs() {
		if strings.HasPrefix(key, exceptionOptionPrefix) {
			options[key] = value
		}
	}
	return options
}

// UpdateExceptionOptions 校验并保存异常管理的设置，任何一项不合法时整批不写入
func UpdateExceptionOptions(values map[string]string) error {
	for key, value := range values {
		if !strings.HasPrefix(key, exceptionOptionPrefix) {
			return fmt.Errorf("not an exception option: %s", key)
		}
		switch strings.TrimPrefix(key, exceptionOptionPrefix) {
		case "capture_retention_days":
			if err := operation_setting.ValidateCaptureRetentionDays(value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown exception option: %s", key)
		}
	}
	return UpdateOptionsBulk(values)
}
