package middleware

import "github.com/gin-gonic/gin"

const (
	// Security headers emitted by the API. HSTS is ignored by browsers on a
	// cleartext response and takes effect only when the platform edge delivers
	// this same response over HTTPS. It has no includeSubDomains and no preload.
	apiHSTS                  = "max-age=31536000"
	apiContentTypeOptions    = "nosniff"
	apiFrameOptions          = "DENY"
	apiReferrerPolicy        = "strict-origin-when-cross-origin"
	apiPermissionsPolicy     = "camera=(), microphone=(), geolocation=(), clipboard=(), fullscreen=(), payment=()"
	apiContentSecurityPolicy = "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'"
	swaggerCSP               = "base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'"
	apiCacheControl          = "no-store"
)

// SecurityHeaders sets the API response policy. It does not read
// X-Forwarded-* and does not change CORS.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("Strict-Transport-Security", apiHSTS)
		header.Set("X-Content-Type-Options", apiContentTypeOptions)
		header.Set("X-Frame-Options", apiFrameOptions)
		header.Set("Referrer-Policy", apiReferrerPolicy)
		header.Set("Permissions-Policy", apiPermissionsPolicy)
		path := c.Request.URL.Path
		if len(path) >= 8 && path[:8] == "/swagger" {
			header.Set("Content-Security-Policy", swaggerCSP)
		} else {
			header.Set("Content-Security-Policy", apiContentSecurityPolicy)
		}
		if path == "/" || path == "/health" || len(path) >= 4 && path[:4] == "/api" {
			header.Set("Cache-Control", apiCacheControl)
		}
		c.Next()
	}
}
