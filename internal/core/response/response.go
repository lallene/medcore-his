package response

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

type APIResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	Meta      any    `json:"meta,omitempty"`
	Error     any    `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId,omitempty"`
}

func Success(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, APIResponse{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func Created(c *gin.Context, message string, data any) {
	c.JSON(http.StatusCreated, APIResponse{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func SuccessWithMeta(c *gin.Context, message string, data any, meta any) {
	c.JSON(http.StatusOK, APIResponse{
		Success:   true,
		Message:   message,
		Data:      data,
		Meta:      meta,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func Error(c *gin.Context, err error) {
	if appErr, ok := err.(*coreerrors.AppError); ok {
		c.JSON(appErr.Status, APIResponse{
			Success:   false,
			Message:   appErr.Message,
			Error:     appErr,
			Timestamp: time.Now().Format(time.RFC3339),
		})
		return
	}

	c.JSON(http.StatusInternalServerError, APIResponse{
		Success:   false,
		Message:   "Erreur interne du serveur",
		Error:     err.Error(),
		Timestamp: time.Now().Format(time.RFC3339),
	})
}
