package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

const captureCleanupInterval = time.Hour

// StartLogAnomalyCaptureCleanup 按保留天数清理过期的抓包现场，只删抓包不改日志
func StartLogAnomalyCaptureCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		cleanupExpiredCapture()
		ticker := time.NewTicker(captureCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupExpiredCapture()
		}
	}()
}

func cleanupExpiredCapture() {
	days := operation_setting.NormalizeCaptureRetentionDays(
		operation_setting.GetExceptionSetting().CaptureRetentionDays,
	)
	before := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	cleared, err := model.PurgeExpiredLogAnomalyCapture(before)
	if err != nil {
		common.SysError(common.LogText("failed to purge expired capture evidence: %s", err.Error()))
		return
	}
	if cleared > 0 {
		common.SysLog(common.LogText("purged %d expired capture evidence records", cleared))
	}
}
