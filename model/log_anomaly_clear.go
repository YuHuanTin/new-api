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

// clearAllAnomalyCaptureBatchSize 每批清理的行数，限制一次读进内存的现场体积
const clearAllAnomalyCaptureBatchSize = 50

// ClearAllLogAnomalyCapture 清除所有日志行保存的抓包现场，日志记录本身保留。
// SQL 数据库整批放在一个事务里，失败时回滚
func ClearAllLogAnomalyCapture() (int64, error) {
	return clearLogAnomalyCapture(0)
}

// PurgeExpiredLogAnomalyCapture 清掉创建时间早于 createdBefore 的抓包现场，
// 供保留天数到期后的后台任务调用
func PurgeExpiredLogAnomalyCapture(createdBefore int64) (int64, error) {
	return clearLogAnomalyCapture(createdBefore)
}

func clearLogAnomalyCapture(createdBefore int64) (int64, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return clearAllAnomalyCapture(LOG_DB, true, createdBefore)
	}
	var cleared int64
	err := LOG_DB.Transaction(func(tx *gorm.DB) error {
		var err error
		cleared, err = clearAllAnomalyCapture(tx, false, createdBefore)
		return err
	})
	if err != nil {
		return 0, err
	}
	return cleared, nil
}

// clearAllAnomalyCapture 按游标分批清理。SQL 用自增 id 做游标，ClickHouse 的
// id 恒为 0，只能用建表排序键 (created_at, request_id)；游标保证解析不了的
// 行也不会让循环卡住。createdBefore 大于 0 时只处理更早的日志
func clearAllAnomalyCapture(db *gorm.DB, clickHouse bool, createdBefore int64) (int64, error) {
	var cleared int64
	lastID := 0
	var lastCreatedAt int64
	lastRequestID := ""
	for {
		var logs []*Log
		query := db.Select("id", "request_id", "created_at", "other").
			Where("other LIKE ? AND other LIKE ?", "%\"admin_info\"%", "%\"anomaly\"%")
		if createdBefore > 0 {
			query = query.Where("created_at < ?", createdBefore)
		}
		if clickHouse {
			// ClickHouse 的 id 恒为 0，只能用 (created_at, request_id) 做游标；
			// 没有 request_id 的行定位不到，跳过以免游标卡住
			query = query.Where("request_id != ''")
			if lastRequestID != "" {
				query = query.Where("(created_at, request_id) > (?, ?)", lastCreatedAt, lastRequestID)
			}
			query = query.Order("created_at, request_id")
		} else {
			query = query.Where("id > ?", lastID).Order("id")
		}
		if err := query.Limit(clearAllAnomalyCaptureBatchSize).Find(&logs).Error; err != nil {
			return cleared, err
		}
		if len(logs) == 0 {
			return cleared, nil
		}
		for _, log := range logs {
			var locator LogLocator
			if clickHouse {
				lastCreatedAt, lastRequestID = log.CreatedAt, log.RequestId
				locator = LogLocator{RequestId: log.RequestId, CreatedAt: log.CreatedAt}
			} else {
				lastID = log.Id
				locator = LogLocator{Id: log.Id}
			}
			other, changed := stripLogAnomalyCapture(log.Other)
			if !changed {
				continue
			}
			if err := updateLogOtherColumn(db, locator, other); err != nil {
				return cleared, err
			}
			cleared++
		}
	}
}

// stripLogAnomalyCapture 从 logs.other 里去掉管理员可见的 anomaly，其它字段原样
// 保留。存进去的是压缩过的字符串，这里直接丢掉即可，不需要解开
func stripLogAnomalyCapture(other string) (string, bool) {
	return stripLogAnomalyCaptureJSON(other, false)
}

// stripLogAnomalyCaptureForList 剥离现场并留下 anomaly_captured，供列表提示
func stripLogAnomalyCaptureForList(other string) (string, bool) {
	return stripLogAnomalyCaptureJSON(other, true)
}

func stripLogAnomalyCaptureJSON(other string, markCaptured bool) (string, bool) {
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
	if markCaptured {
		adminInfo[logOtherAnomalyCapturedKey] = json.RawMessage("true")
	}
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
