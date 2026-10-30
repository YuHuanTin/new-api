package controller

import (
	"net/http"

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
