package openapi

type SuccessResponse struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data"`
	Meta      interface{} `json:"meta,omitempty"`
	Timestamp string      `json:"timestamp"`
}

type ErrorDetail struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

type ErrorResponse struct {
	Success   bool        `json:"success"`
	Message   string      `json:"message,omitempty"`
	Error     ErrorDetail `json:"error"`
	Timestamp string      `json:"timestamp"`
}
