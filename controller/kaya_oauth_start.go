package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const oauthReturnURLSessionKey = "oauth_return_url"
const oauthModeSessionKey = "oauth_mode"

// StartOAuth starts an OAuth flow on behalf of an external frontend such as Kaya.
// It keeps the existing callback handler intact and only stores a validated return_url
// in the session so the callback can redirect after successful login.
func StartOAuth(c *gin.Context) {
	providerName := c.Param("provider")
	provider := oauth.GetProvider(providerName)
	if provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgOAuthUnknownProvider),
		})
		return
	}
	if !provider.IsEnabled() {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(provider.GetName()))
		return
	}

	session := sessions.Default(c)
	state := common.GetRandomString(12)
	session.Set("oauth_state", state)
	session.Set(oauthModeSessionKey, "login")

	returnURL := c.Query("return_url")
	if returnURL != "" {
		if err := common.ValidateRedirectURL(returnURL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": err.Error(),
			})
			return
		}
		session.Set(oauthReturnURLSessionKey, returnURL)
	}

	if affCode := c.Query("aff"); affCode != "" {
		session.Set("aff", affCode)
	}

	if err := session.Save(); err != nil {
		common.ApiError(c, err)
		return
	}

	authorizeURL, err := buildOAuthAuthorizeURL(c, providerName, state)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.Redirect(http.StatusFound, authorizeURL)
}

func buildOAuthAuthorizeURL(c *gin.Context, providerName string, state string) (string, error) {
	switch providerName {
	case "github":
		u, err := url.Parse("https://github.com/login/oauth/authorize")
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("client_id", common.GitHubClientId)
		q.Set("redirect_uri", fmt.Sprintf("%s/api/oauth/github", externalServerAddress(c)))
		q.Set("state", state)
		q.Set("scope", "user:email")
		u.RawQuery = q.Encode()
		return u.String(), nil
	case "oidc":
		settings := system_setting.GetOIDCSettings()
		u, err := url.Parse(settings.AuthorizationEndpoint)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("client_id", settings.ClientId)
		q.Set("redirect_uri", fmt.Sprintf("%s/oauth/oidc", system_setting.ServerAddress))
		q.Set("response_type", "code")
		q.Set("scope", "openid profile email")
		q.Set("state", state)
		u.RawQuery = q.Encode()
		return u.String(), nil
	default:
		provider := oauth.GetProvider(providerName)
		genericProvider, ok := provider.(*oauth.GenericOAuthProvider)
		if !ok {
			return "", fmt.Errorf("unsupported oauth provider: %s", providerName)
		}
		config := genericProvider.GetConfig()
		u, err := url.Parse(config.AuthorizationEndpoint)
		if err != nil {
			return "", err
		}
		q := u.Query()
		q.Set("client_id", config.ClientId)
		q.Set("redirect_uri", fmt.Sprintf("%s/oauth/%s", system_setting.ServerAddress, config.Slug))
		q.Set("response_type", "code")
		q.Set("state", state)
		if config.Scopes != "" {
			q.Set("scope", config.Scopes)
		}
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
}

func externalServerAddress(c *gin.Context) string {
	if serverAddress := strings.TrimSpace(os.Getenv("KAYA_OAUTH_SERVER_ADDRESS")); serverAddress != "" {
		return strings.TrimRight(serverAddress, "/")
	}

	proto := c.GetHeader("X-Forwarded-Proto")
	if proto == "" {
		proto = c.GetHeader("X-Forwarded-Protocol")
	}
	if proto == "" {
		proto = "http"
		if c.Request.TLS != nil {
			proto = "https"
		}
	}

	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}

	return strings.TrimRight(fmt.Sprintf("%s://%s", proto, host), "/")
}
