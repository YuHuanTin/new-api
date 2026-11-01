package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetExceptionSetting 返回异常管理的抓包设置
func GetExceptionSetting(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"options": model.CurrentExceptionOptions()}})
}

// UpdateExceptionSetting 保存异常管理的抓包设置
func UpdateExceptionSetting(c *gin.Context) {
	var request struct {
		Options map[string]string `json:"options"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := model.UpdateExceptionOptions(request.Options); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	GetExceptionSetting(c)
}
