package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetCyberGuardSetting 返回 cyber_policy 清退的设置
func GetCyberGuardSetting(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"options": model.CurrentCyberGuardOptions()}})
}

// UpdateCyberGuardSetting 保存 cyber_policy 清退的设置
func UpdateCyberGuardSetting(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdateCyberGuardOptions(request.Options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	GetCyberGuardSetting(c)
}

// maxClearExceptionCaptureIDs 限制一次清理的行数，避免一次写入过多行
const maxClearExceptionCaptureIDs = 200

// ClearExceptionCapture 清除选中日志保存的抓包现场，日志记录本身保留
func ClearExceptionCapture(c *gin.Context) {
	var request struct {
		Items []model.LogLocator `json:"items"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "items is required"})
		return
	}
	if len(request.Items) > maxClearExceptionCaptureIDs {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "too many items"})
		return
	}
	cleared, err := model.ClearLogAnomalyCapture(request.Items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"cleared": cleared}})
}

// GetExceptionCapture 按需读取一条日志保存的抓包现场。日志列表不带这块数据
// （一条最多 4 MB），只有打开详情时才拉取。ClickHouse 的 log id 不可用，
// 前端同时带上 request_id 和 created_at
func GetExceptionCapture(c *gin.Context) {
	id, _ := strconv.Atoi(c.Query("id"))
	createdAt, _ := strconv.ParseInt(c.Query("created_at"), 10, 64)
	locator := model.LogLocator{Id: id, RequestId: c.Query("request_id"), CreatedAt: createdAt}
	if !locator.Valid() {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid log locator"})
		return
	}
	anomaly, found, err := model.GetLogAnomalyCapture(locator)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "log not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"anomaly": anomaly}})
}

// ListCyberGuardChannels 返回开启了 cyber 策略计数的渠道，异常管理用来展示和快捷关闭
func ListCyberGuardChannels(c *gin.Context) {
	channels, err := model.ListCyberGuardChannels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(channels))
	for _, channel := range channels {
		items = append(items, gin.H{"id": channel.Id, "name": channel.Name, "status": channel.Status})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"items": items}})
}

// SetChannelCyberGuard 打开或关闭一个渠道的 cyber 策略计数
func SetChannelCyberGuard(c *gin.Context) {
	var request struct {
		Id      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id"})
		return
	}
	if err := model.SetChannelCyberGuard(request.Id, request.Enabled); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
