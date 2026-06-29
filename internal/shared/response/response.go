package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Meta    interface{} `json:"meta,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

func Success(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func Created(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusCreated, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func SuccessWithMeta(c *gin.Context, message string, data interface{}, meta interface{}) {
	c.JSON(http.StatusOK, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
		Meta:    meta,
	})
}

func Error(c *gin.Context, status int, message string, err interface{}) {
	c.JSON(status, APIResponse{
		Success: false,
		Message: message,
		Error:   err,
	})
}
