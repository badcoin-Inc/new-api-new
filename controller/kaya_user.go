package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// GetKayaSelf returns the current session user for Kaya callbacks.
// It intentionally relies on the dashboard session cookie and avoids UserAuth,
// because UserAuth also requires the New-Api-User header for API clients.
func GetKayaSelf(c *gin.Context) {
	session := sessions.Default(c)
	id, ok := session.Get("id").(int)
	if !ok || id == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "not logged in",
		})
		return
	}

	user, err := model.GetUserById(id, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	user.Remark = ""

	// Ensure all default tokens exist and get them
	_, err = model.EnsureKayaDefaultTokens(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Get all default tokens for this user
	defaultTokens, err := model.GetKayaDefaultTokens(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	// Convert tokens to response format
	var defaultTokensData []map[string]interface{}
	for _, token := range defaultTokens {
		defaultTokensData = append(defaultTokensData, map[string]interface{}{
			"id":                   token.Id,
			"user_id":              token.UserId,
			"key":                  token.Key,
			"status":               token.Status,
			"name":                 token.Name,
			"created_time":         token.CreatedTime,
			"accessed_time":        token.AccessedTime,
			"expired_time":         token.ExpiredTime,
			"remain_quota":         token.RemainQuota,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits_enabled": token.ModelLimitsEnabled,
			"model_limits":         token.ModelLimits,
			"allow_ips":            token.AllowIps,
			"used_quota":           token.UsedQuota,
			"group":                token.Group,
			"cross_group_retry":    token.CrossGroupRetry,
		})
	}

	// For backward compatibility, also include the first token as default_token
	var defaultTokenData any
	if len(defaultTokensData) > 0 {
		defaultTokenData = defaultTokensData[0]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": map[string]interface{}{
			"id":             user.Id,
			"username":       user.Username,
			"display_name":   user.DisplayName,
			"role":           user.Role,
			"status":         user.Status,
			"email":          user.Email,
			"group":          user.Group,
			"quota":          user.Quota,
			"used_quota":     user.UsedQuota,
			"request_count":  user.RequestCount,
			"default_token":  defaultTokenData,
			"default_tokens": defaultTokensData,
		},
	})
}
