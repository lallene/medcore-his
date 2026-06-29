package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/shared/logger"
)

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start)

		logger.Info(
			c.Request.Method + " " +
				c.Request.URL.Path + " " +
				c.ClientIP() + " " +
				duration.String(),
		)
	}
}
