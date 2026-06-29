package errors

import "fmt"

type AppError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *AppError) Error() string {
	return e.Message
}

func New(status int, code string, message string, details any) *AppError {
	return &AppError{
		Status:  status,
		Code:    code,
		Message: message,
		Details: details,
	}
}

func BadRequest(message string) *AppError {
	return New(400, "BAD_REQUEST", message, nil)
}

func Unauthorized(message string) *AppError {
	return New(401, "UNAUTHORIZED", message, nil)
}

func Forbidden(message string) *AppError {
	return New(403, "FORBIDDEN", message, nil)
}

func NotFound(resource string) *AppError {
	return New(
		404,
		fmt.Sprintf("%s_NOT_FOUND", resource),
		fmt.Sprintf("%s introuvable", resource),
		nil,
	)
}

func Conflict(message string) *AppError {
	return New(409, "CONFLICT", message, nil)
}

func Validation(message string, details any) *AppError {
	return New(422, "VALIDATION_ERROR", message, details)
}

func Internal(message string) *AppError {
	return New(500, "INTERNAL_ERROR", message, nil)
}
