package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type KayaOAuthExchangeRequest struct {
	Code string `json:"code"`
}

func ExchangeKayaOAuthCode(c *gin.Context) {
	var request KayaOAuthExchangeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}

	data, err := model.ConsumeKayaOAuthExchangeCode(strings.TrimSpace(request.Code))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "invalid or expired code",
		})
		return
	}

	user, err := model.GetUserById(data.UserId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Status != common.UserStatusEnabled {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "user is disabled",
		})
		return
	}

	tokens, err := model.EnsureKayaDefaultTokensForApp(user.Id, data.AppName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(tokens) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "no token group configured for app",
		})
		return
	}

	defaultToken := tokens[0]
	responseTokens := make([]gin.H, 0, len(tokens))
	for _, token := range tokens {
		responseTokens = append(responseTokens, gin.H{
			"id":    token.Id,
			"key":   withSkPrefix(token.GetFullKey()),
			"name":  token.Name,
			"group": token.Group,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"app":          data.AppName,
			"access_token": withSkPrefix(defaultToken.GetFullKey()),
			"token_type":   "Bearer",
			"tokens":       responseTokens,
			"user":         loginSuccessData(user),
		},
	})
}

func withSkPrefix(key string) string {
	if key == "" || strings.HasPrefix(key, "sk-") {
		return key
	}
	return "sk-" + key
}
