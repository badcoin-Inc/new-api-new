package controller

import (
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// KayaModelCatalogGroupInfo represents availability info for a specific group
type KayaModelCatalogGroupInfo struct {
	Group     string `json:"group"`
	Available bool   `json:"available"`
}

type KayaModelCatalogItem struct {
	Id            string                      `json:"id"`
	Name          string                      `json:"name"`
	Description   string                      `json:"description,omitempty"`
	Icon          string                      `json:"icon,omitempty"`
	Tags          []string                    `json:"tags"`
	Groups        []string                    `json:"groups"`
	GroupsInfo    []KayaModelCatalogGroupInfo `json:"groups_info"`
	Available     bool                        `json:"available"`
	Locked        bool                        `json:"locked"`
	Reason        string                      `json:"reason,omitempty"`
	Pricing       map[string]any              `json:"pricing"`
	Capabilities  []string                    `json:"capabilities"`
	Status        int                         `json:"status"`
	Sort          int                         `json:"sort"`
	BoundChannels []model.BoundChannel        `json:"bound_channels,omitempty"`
}

func GetKayaModelCatalog(c *gin.Context) {
	session := sessions.Default(c)
	userId, ok := session.Get("id").(int)
	if !ok || userId == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "not logged in",
		})
		return
	}

	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Get key parameter to determine which group family to show
	keyParam := c.Query("key")
	var tokenGroup string
	var token *model.Token

	if keyParam != "" {
		// Validate the token belongs to this user
		token, err = model.GetTokenByKey(strings.TrimPrefix(keyParam, "sk-"), false)
		if err != nil || token == nil || token.UserId != user.Id {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid token",
			})
			return
		}
		tokenGroup = token.Group
	} else {
		// Fallback to default token
		token, err = model.EnsureKayaDefaultToken(user.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if token != nil {
			tokenGroup = token.Group
		}
	}

	if tokenGroup == "" {
		tokenGroup = user.Group
	}

	// Get the group family (e.g., "image" -> ["image", "image_vip"])
	groupFamily := getGroupFamily(tokenGroup)

	// Get user's usable groups to determine availability
	userUsableGroups := service.GetUserUsableGroups(user.Group)

	// Build available models map per group
	availableByGroup := make(map[string]map[string]bool)
	for _, group := range groupFamily {
		availableByGroup[group] = make(map[string]bool)
		for _, modelName := range model.GetGroupEnabledModels(group) {
			availableByGroup[group][modelName] = true
		}
	}

	// If token has model limits enabled, override availability
	if token != nil && token.ModelLimitsEnabled {
		tokenModelLimits := make(map[string]bool)
		for _, modelName := range token.GetModelLimits() {
			tokenModelLimits[modelName] = true
		}
		// Filter available models by token limits
		for group := range availableByGroup {
			for modelName := range availableByGroup[group] {
				if !tokenModelLimits[modelName] {
					delete(availableByGroup[group], modelName)
				}
			}
		}
	}

	// Get all models in the group family
	models := []*model.Model{}
	if err := model.DB.Where("status = ?", common.ChannelStatusEnabled).Order("id desc").Find(&models).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	// Filter models that belong to the group family
	filteredModels := make([]*model.Model, 0)
	for _, item := range models {
		for _, group := range groupFamily {
			if availableByGroup[group][item.ModelName] {
				filteredModels = append(filteredModels, item)
				break
			}
		}
	}

	modelNames := make([]string, 0, len(filteredModels))
	for _, item := range filteredModels {
		modelNames = append(modelNames, item.ModelName)
	}
	boundChannels, _ := model.GetBoundChannelsByModelsMap(modelNames)
	groupsByModel := getKayaCatalogGroupsByModel(modelNames)

	items := make([]KayaModelCatalogItem, 0, len(filteredModels))
	for index, item := range filteredModels {
		groups := groupsByModel[item.ModelName]
		price, hasPrice := ratio_setting.GetModelPrice(item.ModelName, false)
		modelRatio, hasModelRatio, _ := ratio_setting.GetModelRatio(item.ModelName)

		// Build groups info with availability status
		groupsInfo := make([]KayaModelCatalogGroupInfo, 0)
		isAvailable := false
		for _, group := range groupFamily {
			if availableByGroup[group][item.ModelName] {
				_, groupUsable := userUsableGroups[group]
				groupsInfo = append(groupsInfo, KayaModelCatalogGroupInfo{
					Group:     group,
					Available: groupUsable,
				})
				if groupUsable {
					isAvailable = true
				}
			}
		}

		catalogItem := KayaModelCatalogItem{
			Id:            item.ModelName,
			Name:          item.ModelName,
			Description:   item.Description,
			Icon:          item.Icon,
			Tags:          splitCatalogCSV(item.Tags),
			Groups:        groups,
			GroupsInfo:    groupsInfo,
			Available:     isAvailable,
			Locked:        !isAvailable,
			Capabilities:  endpointTypesToStrings(model.GetModelSupportEndpointTypes(item.ModelName)),
			Status:        item.Status,
			Sort:          index,
			BoundChannels: boundChannels[item.ModelName],
			Pricing: map[string]any{
				"model_price":       price,
				"model_price_found": hasPrice,
				"model_ratio":       modelRatio,
				"model_ratio_found": hasModelRatio,
				"completion_ratio":  ratio_setting.GetCompletionRatio(item.ModelName),
			},
		}
		if !isAvailable {
			catalogItem.Reason = "当前分组暂不可用"
		}
		items = append(items, catalogItem)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"models":       items,
			"user_group":   user.Group,
			"token_group":  tokenGroup,
			"group_family": groupFamily,
		},
	})
}

// getGroupFamily returns the group family for a given group.
// e.g., "image" -> ["image", "image_vip"], "default" -> ["default", "default_vip"]
func getGroupFamily(group string) []string {
	// Remove _vip suffix if present to get base group
	baseGroup := strings.TrimSuffix(group, "_vip")

	family := []string{baseGroup}
	vipGroup := baseGroup + "_vip"

	// Check if vip group exists in the system
	if ratio_setting.ContainsGroupRatio(vipGroup) {
		family = append(family, vipGroup)
	}

	return family
}

func endpointTypesToStrings(endpointTypes []constant.EndpointType) []string {
	result := make([]string, 0, len(endpointTypes))
	for _, endpointType := range endpointTypes {
		result = append(result, string(endpointType))
	}
	return result
}

func getKayaCatalogGroupsByModel(modelNames []string) map[string][]string {
	result := make(map[string][]string)
	if len(modelNames) == 0 {
		return result
	}

	type row struct {
		Model string
		Group string
	}
	var rows []row
	if err := model.DB.Table("abilities").
		Select("model, \"group\" as group").
		Where("model IN ? AND enabled = ?", modelNames, true).
		Distinct().
		Scan(&rows).Error; err != nil {
		return result
	}

	for _, r := range rows {
		if r.Group == "" {
			continue
		}
		result[r.Model] = append(result[r.Model], r.Group)
	}
	for modelName := range result {
		sort.Strings(result[modelName])
	}
	return result
}

func splitCatalogCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
