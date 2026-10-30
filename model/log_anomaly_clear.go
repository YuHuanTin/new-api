package model

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ClearLogAnomalyCapture 清除指定日志行保存的抓包现场，只删 admin_info.anomaly，
// 日志记录本身保留。整批清理在同一个事务里完成，失败时不留下清了一半的状态。
// ClickHouse 没有事务，只能逐条同步执行
func ClearLogAnomalyCapture(locators []LogLocator) (int, error) {
	if len(locators) == 0 {
		return 0, nil
	}
	cleared := 0
	clearCapture := func(tx *gorm.DB) error {
		for _, locator := range locators {
			if !locator.Valid() {
				continue
			}
			condition, args := locator.condition()
			var logs []*Log
			if err := tx.Select("id", "other").Where(condition, args...).Limit(1).Find(&logs).Error; err != nil {
				return err
			}
			if len(logs) == 0 {
				continue
			}
			other, changed := stripLogAnomalyCapture(logs[0].Other)
			if !changed {
				continue
			}
			if err := updateLogOtherColumn(tx, locator, other); err != nil {
				return err
			}
			cleared++
		}
		return nil
	}
	var err error
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		err = clearCapture(LOG_DB)
	} else {
		err = LOG_DB.Transaction(clearCapture)
	}
	if err != nil {
		return 0, err
	}
	return cleared, nil
}

// stripLogAnomalyCapture 从 logs.other 里去掉管理员可见的 anomaly，其它字段原样
// 保留。存进去的是压缩过的字符串，这里直接丢掉即可，不需要解开
func stripLogAnomalyCapture(other string) (string, bool) {
	if other == "" || !strings.Contains(other, logOtherAnomalyKey) {
		return other, false
	}
	var values map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(other, &values); err != nil {
		return other, false
	}
	rawAdminInfo, ok := values[logOtherAdminInfoKey]
	if !ok {
		return other, false
	}
	var adminInfo map[string]json.RawMessage
	if err := common.Unmarshal(rawAdminInfo, &adminInfo); err != nil {
		return other, false
	}
	if _, ok := adminInfo[logOtherAnomalyKey]; !ok {
		return other, false
	}
	delete(adminInfo, logOtherAnomalyKey)
	if len(adminInfo) == 0 {
		delete(values, logOtherAdminInfoKey)
	} else {
		encodedAdminInfo, err := common.Marshal(adminInfo)
		if err != nil {
			return other, false
		}
		values[logOtherAdminInfoKey] = encodedAdminInfo
	}
	encoded, err := common.Marshal(values)
	if err != nil {
		return other, false
	}
	return string(encoded), true
}

// updateLogOtherColumn 更新一行的 other 列。ClickHouse 的更新是一次同步 mutation
func updateLogOtherColumn(tx *gorm.DB, locator LogLocator, other string) error {
	condition, args := locator.condition()
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		args = append([]any{other}, args...)
		return tx.Exec(
			"ALTER TABLE logs UPDATE other = ? WHERE "+condition+" SETTINGS mutations_sync = 1",
			args...,
		).Error
	}
	return tx.Model(&Log{}).Where(condition, args...).Update("other", other).Error
}
