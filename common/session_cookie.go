package common

import (
	"net/http"
	"strings"
)

func SessionCookieSameSiteMode() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(GetEnvOrDefaultString("SESSION_COOKIE_SAMESITE", "strict"))) {
	case "lax":
		return http.SameSiteLaxMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteStrictMode
	}
}
