package model

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// LogLocator 定位一条日志。ClickHouse 的 log id 恒为 0（列表显示的是合成序号），
// 只能靠建表排序键 (created_at, request_id) 定位；其它数据库用自增 id
type LogLocator struct {
	Id        int    `json:"id"`
	RequestId string `json:"request_id"`
	CreatedAt int64  `json:"created_at"`
}

// Valid 判断定位信息是否足够查询
func (l LogLocator) Valid() bool {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return l.RequestId != "" && l.CreatedAt > 0
	}
	return l.Id > 0
}

// condition 返回定位这条日志的 WHERE 条件和参数
func (l LogLocator) condition() (string, []any) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return "created_at = ? AND request_id = ?", []any{l.CreatedAt, l.RequestId}
	}
	return "id = ?", []any{l.Id}
}

// StripLogAnomalyCapture 从列表行里去掉抓包现场（一条可达 4 MB），详情按需拉取
func StripLogAnomalyCapture(logs []*Log) {
	for _, log := range logs {
		if log == nil {
			continue
		}
		if other, changed := stripLogAnomalyCaptureForList(log.Other); changed {
			log.Other = other
		}
	}
}

// LogAnomalyCaptureUsage 抓包数据占用的统计
type LogAnomalyCaptureUsage struct {
	Records int64 `json:"records"`
	Bytes   int64 `json:"bytes"`
}

// GetLogAnomalyCaptureUsage 统计抓包数据的条数和占用字节数。现场在
// admin_info.anomaly 里，按日志行分批解析，不依赖各数据库的 JSON 函数，
// 解析不了的行直接跳过，也不需要额外建表或索引。统计会扫一遍 logs 表，
// 只给管理员面板按需调用
func GetLogAnomalyCaptureUsage() (LogAnomalyCaptureUsage, error) {
	var usage LogAnomalyCaptureUsage
	err := forEachAnomalyCaptureRow(LOG_DB, common.UsingLogDatabase(common.DatabaseTypeClickHouse), 0,
		func(log *Log, _ LogLocator) error {
			size, ok := logAnomalyCaptureSize(log.Other)
			if !ok {
				return nil
			}
			usage.Records++
			usage.Bytes += size
			return nil
		})
	return usage, err
}

// logAnomalyCaptureSize 取出 admin_info.anomaly 保存内容的字节数，解析不了时返回 false
func logAnomalyCaptureSize(other string) (int64, bool) {
	if other == "" || !strings.Contains(other, logOtherAnomalyKey) {
		return 0, false
	}
	var values map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(other, &values); err != nil {
		return 0, false
	}
	rawAdminInfo, ok := values[logOtherAdminInfoKey]
	if !ok {
		return 0, false
	}
	var adminInfo map[string]json.RawMessage
	if err := common.Unmarshal(rawAdminInfo, &adminInfo); err != nil {
		return 0, false
	}
	rawAnomaly, ok := adminInfo[logOtherAnomalyKey]
	if !ok {
		return 0, false
	}
	var anomaly string
	if err := common.Unmarshal(rawAnomaly, &anomaly); err != nil {
		// 旧数据的现场是对象，按原始 JSON 文本计长度
		return int64(len(rawAnomaly)), true
	}
	return int64(len(anomaly)), true
}

// GetLogAnomalyCapture 原样返回 gzip+Base64，不在后端解压；旧对象仅在读取时压缩，不改写记录。
func GetLogAnomalyCapture(locator LogLocator) (anomaly string, found bool, err error) {
	condition, args := locator.condition()
	var logs []*Log
	if err := LOG_DB.Select("id", "other").Where(condition, args...).Limit(1).Find(&logs).Error; err != nil {
		return "", false, err
	}
	if len(logs) == 0 {
		return "", false, nil
	}
	var stored map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(logs[0].Other, &stored); err != nil {
		return "", true, err
	}
	if len(stored[logOtherAdminInfoKey]) == 0 {
		return "", true, nil
	}
	var adminInfo map[string]json.RawMessage
	if err := common.Unmarshal(stored[logOtherAdminInfoKey], &adminInfo); err != nil {
		return "", true, err
	}
	raw := adminInfo[logOtherAnomalyKey]
	if len(raw) == 0 || string(raw) == "null" {
		return "", true, nil
	}
	var encoded string
	if err := common.Unmarshal(raw, &encoded); err == nil {
		if strings.HasPrefix(encoded, "gz:") {
			return encoded, true, nil
		}
		return common.CompressText(encoded), true, nil
	}
	return common.CompressText(string(raw)), true, nil
}
