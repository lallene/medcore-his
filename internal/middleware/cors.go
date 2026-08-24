package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS(origin string) gin.HandlerFunc {
	allowedOrigin := strings.TrimSpace(origin)

	return func(c *gin.Context) {
		requestOrigin := c.GetHeader("Origin")

		if requestOrigin != "" && requestOrigin == allowedOrigin {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Vary", "Origin")
		}

		c.Writer.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)
		c.Writer.Header().Set(
			"Access-Control-Allow-Methods",
			"GET, POST, PUT, PATCH, DELETE, OPTIONS",
		)

		if c.Request.Method == http.MethodOptions {
			if requestOrigin != "" && requestOrigin != allowedOrigin {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}

			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
