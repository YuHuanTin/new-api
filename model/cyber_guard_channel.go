package model

import "gorm.io/gorm"

// ListCyberGuardChannels 返回开启了 cyber 策略计数的渠道
func ListCyberGuardChannels() ([]*Channel, error) {
	var channels []*Channel
	if err := DB.Select("id", "name", "status", "setting").
		Where("setting LIKE ?", "%cyber_guard_enabled%").
		Find(&channels).Error; err != nil {
		return nil, err
	}
	enabled := make([]*Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.GetSetting().CyberGuardEnabled {
			enabled = append(enabled, channel)
		}
	}
	return enabled, nil
}

// SetChannelCyberGuard 打开或关闭单个渠道的 cyber 策略计数。读取-修改-写回放在
// 同一个事务里，避免和同渠道其它设置的并发修改互相覆盖
func SetChannelCyberGuard(id int, enabled bool) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).First(&channel, id).Error; err != nil {
			return err
		}
		setting := channel.GetSetting()
		setting.CyberGuardEnabled = enabled
		channel.SetSetting(setting)
		return tx.Model(&Channel{}).Where("id = ?", id).Update("setting", channel.Setting).Error
	})
	if err != nil {
		return err
	}
	InitChannelCache()
	return nil
}
