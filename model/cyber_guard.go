package model

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const cyberGuardOptionPrefix = "cyber_setting."

// CurrentCyberGuardOptions 返回 cyber_policy 清退的当前设置，键带 cyber_setting. 前缀
func CurrentCyberGuardOptions() map[string]string {
	options := map[string]string{}
	for key, value := range config.GlobalConfig.ExportAllConfigs() {
		if strings.HasPrefix(key, cyberGuardOptionPrefix) {
			options[key] = value
		}
	}
	return options
}

// UpdateCyberGuardOptions 校验并保存 cyber_policy 清退的设置。任何一项不合法时
// 整批不写入，避免只存进去一半
func UpdateCyberGuardOptions(values map[string]string) error {
	for key, value := range values {
		if !strings.HasPrefix(key, cyberGuardOptionPrefix) {
			return fmt.Errorf("not a cyber guard option: %s", key)
		}
		switch strings.TrimPrefix(key, cyberGuardOptionPrefix) {
		case "auto_ban":
			if _, err := strconv.ParseBool(value); err != nil {
				return fmt.Errorf("invalid boolean: %s", key)
			}
		case "window_minutes":
			if err := operation_setting.ValidateCyberWindowMinutes(value); err != nil {
				return err
			}
		case "threshold":
			if err := operation_setting.ValidateCyberThreshold(value); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown cyber guard option: %s", key)
		}
	}
	return UpdateOptionsBulk(values)
}
