package model

import (
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
		if other, changed := stripLogAnomalyCapture(log.Other); changed {
			log.Other = other
		}
	}
}

// LogAnomalyCaptureUsage 抓包数据占用的统计
type LogAnomalyCaptureUsage struct {
	Records int64 `json:"records"`
	Bytes   int64 `json:"bytes"`
}

// GetLogAnomalyCaptureUsage 统计抓包数据的条数和占用字节数。抓包内容在
// admin_info.anomaly 里，按各数据库自带的 JSON 函数在库里直接聚合，不把现场
// 读到内存，也不需要额外建表或索引
func GetLogAnomalyCaptureUsage() (LogAnomalyCaptureUsage, error) {
	var usage LogAnomalyCaptureUsage
	var columns, condition string
	switch common.LogDatabaseType() {
	case common.DatabaseTypeMySQL:
		columns = "COUNT(*) AS records, COALESCE(SUM(LENGTH(JSON_UNQUOTE(JSON_EXTRACT(other, '$.admin_info.anomaly')))), 0) AS bytes"
		condition = "JSON_VALID(other) AND JSON_EXTRACT(other, '$.admin_info.anomaly') IS NOT NULL"
	case common.DatabaseTypePostgreSQL:
		columns = "COUNT(*) AS records, COALESCE(SUM(octet_length(other::jsonb #>> '{admin_info,anomaly}')), 0) AS bytes"
		condition = "jsonb_typeof(other::jsonb -> 'admin_info' -> 'anomaly') = 'string'"
	case common.DatabaseTypeClickHouse:
		columns = "count() AS records, sum(length(JSONExtractString(other, 'admin_info', 'anomaly'))) AS bytes"
		condition = "JSONHas(other, 'admin_info')"
	default:
		columns = "COUNT(*) AS records, COALESCE(SUM(LENGTH(CAST(json_extract(other, '$.admin_info.anomaly') AS BLOB))), 0) AS bytes"
		condition = "json_valid(other) AND json_extract(other, '$.admin_info.anomaly') IS NOT NULL"
	}
	err := LOG_DB.Table("logs").Select(columns).Where(condition).
		Row().Scan(&usage.Records, &usage.Bytes)
	return usage, err
}

// GetLogAnomalyCapture 返回解压后的抓包现场；found 为 false 表示日志不存在
func GetLogAnomalyCapture(locator LogLocator) (anomaly map[string]any, found bool, err error) {
	condition, args := locator.condition()
	var logs []*Log
	if err := LOG_DB.Select("id", "other").Where(condition, args...).Limit(1).Find(&logs).Error; err != nil {
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
