package model

import (
	"github.com/QuantumNous/new-api/common"
)

// StripLogAnomalyCapture 从列表行里去掉抓包现场（一条可达 4 MB），详情按需拉取
func StripLogAnomalyCapture(logs []*Log) {
	for _, log := range logs {
		if log == nil {
			continue
		}
		if other, changed := stripLogAnomalyCapture(log.Other); changed {
			log.Other = other
		}
	}
}

// GetLogAnomalyCapture 返回解压后的抓包现场；found 为 false 表示日志不存在
func GetLogAnomalyCapture(id int) (anomaly map[string]any, found bool, err error) {
	var logs []*Log
	if err := LOG_DB.Select("id", "other").Where("id = ?", id).Limit(1).Find(&logs).Error; err != nil {
		return nil, false, err
	}
	if len(logs) == 0 {
		return nil, false, nil
	}
	FormatAdminLogs(logs)
	stored, err := common.StrToMap(logs[0].Other)
	if err != nil {
		return nil, true, err
	}
	adminInfo, _ := stored[logOtherAdminInfoKey].(map[string]any)
	anomaly, _ = adminInfo[logOtherAnomalyKey].(map[string]any)
	return anomaly, true, nil
}
