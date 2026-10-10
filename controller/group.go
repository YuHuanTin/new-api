package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

const groupCommentsOptionKey = "group_ratio_setting.group_comments"

func GetGroups(c *gin.Context) {
	groupRatios := ratio_setting.GetGroupRatioCopy()
	groupNames := make([]string, 0)
	for groupName := range groupRatios {
		groupNames = append(groupNames, groupName)
	}

	common.OptionMapRWMutex.RLock()
	rawGroupComments := common.OptionMap[groupCommentsOptionKey]
	common.OptionMapRWMutex.RUnlock()
	groupComments := make(map[string]string)
	if strings.TrimSpace(rawGroupComments) != "" {
		if err := common.UnmarshalJsonStr(rawGroupComments, &groupComments); err != nil {
			groupComments = make(map[string]string)
		}
	}
	if groupComments == nil {
		groupComments = make(map[string]string)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"message":        "",
		"data":           groupNames,
		"group_ratio":    groupRatios,
		"group_comments": groupComments,
	})
}

func GetUserGroups(c *gin.Context) {
	usableGroups := make(map[string]map[string]any)
	userGroup := ""
	userId := c.GetInt("id")
	userGroup, _ = model.GetUserGroup(userId, false)
	userUsableGroups := service.GetUserUsableGroups(userGroup)
	for groupName, _ := range ratio_setting.GetGroupRatioCopy() {
		// UserUsableGroups contains the groups that the user can use
		if desc, ok := userUsableGroups[groupName]; ok {
			usableGroups[groupName] = map[string]any{
				"ratio": service.GetUserGroupRatio(userGroup, groupName),
				"desc":  desc,
			}
		}
	}
	if _, ok := userUsableGroups["auto"]; ok {
		usableGroups["auto"] = map[string]any{
			"ratio": "auto",
			"desc":  setting.GetUsableGroupDescription("auto"),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    usableGroups,
	})
}
