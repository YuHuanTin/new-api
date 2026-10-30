package model

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// restoreCompressedAnomaly 解开写入时整块压缩的异常现场。旧数据的 anomaly 本身
// 就是对象，解不出来时保持原样
func restoreCompressedAnomaly(values map[string]json.RawMessage) bool {
	rawAdminInfo, ok := values[logOtherAdminInfoKey]
	if !ok {
		return false
	}
	var adminInfo map[string]json.RawMessage
	if err := common.Unmarshal(rawAdminInfo, &adminInfo); err != nil {
		return false
	}
	var encoded string
	if err := common.Unmarshal(adminInfo[logOtherAnomalyKey], &encoded); err != nil {
		return false
	}
	var anomaly map[string]any
	if err := common.UnmarshalJsonStr(common.DecompressText(encoded), &anomaly); err != nil {
		return false
	}
	restoredAnomaly, err := common.Marshal(anomaly)
	if err != nil {
		return false
	}
	adminInfo[logOtherAnomalyKey] = restoredAnomaly
	restoredAdminInfo, err := common.Marshal(adminInfo)
	if err != nil {
		return false
	}
	values[logOtherAdminInfoKey] = restoredAdminInfo
	return true
}
