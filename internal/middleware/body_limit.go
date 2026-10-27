package middleware

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

// BodyLimit bounds every request body (GRC15-B07). multipart/form-data
// requests get maxUpload, every other body maxBody; route limits (ticket
// attachments) still apply below the ceiling.
//
// The limit is enforced while the body is read, so a missing or false
// Content-Length and chunked bodies are bounded too. An honest oversize
// Content-Length is refused before the handler runs. When a handler hits the
// limit while reading, its own error response is replaced by 413.
func BodyLimit(maxBody, maxUpload int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		limit := maxBody
		if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
			limit = maxUpload
		}
		if c.Request.ContentLength > limit {
			// Close the connection: the unread body must not be parsed as the next request.
			c.Header("Connection", "close")
			response.Error(c, payloadTooLarge())
			c.Abort()
			return
		}

		limited := &limitedBody{ReadCloser: http.MaxBytesReader(c.Writer, c.Request.Body, limit)}
		c.Request.Body = limited
		c.Writer = &overflowWriter{ResponseWriter: c.Writer, body: limited}
		c.Next()
	}
}

func payloadTooLarge() *coreerrors.AppError {
	return coreerrors.New(http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "Corps de requête trop volumineux", nil)
}

// limitedBody records that the ceiling was reached while reading.
type limitedBody struct {
	io.ReadCloser
	exceeded bool
}

func (b *limitedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		b.exceeded = true
	}
	return n, err
}

// overflowWriter turns the handler's response into a 413 once the body
// ceiling has been exceeded, whatever error the handler derived from it.
type overflowWriter struct {
	gin.ResponseWriter
	body     *limitedBody
	replaced bool
}

func (w *overflowWriter) WriteHeader(code int) {
	if w.body.exceeded {
		code = http.StatusRequestEntityTooLarge
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *overflowWriter) Write(data []byte) (int, error) {
	if !w.body.exceeded {
		return w.ResponseWriter.Write(data)
	}
	if !w.replaced {
		w.replaced = true
		w.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.ResponseWriter.Header().Set("Connection", "close")
		w.ResponseWriter.WriteHeader(http.StatusRequestEntityTooLarge)
		if _, err := w.ResponseWriter.Write(payloadTooLargeBody); err != nil {
			return 0, err
		}
	}
	// The handler's own body is dropped; report it as written.
	return len(data), nil
}

func (w *overflowWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

var payloadTooLargeBody = []byte(`{"success":false,"message":"Corps de requête trop volumineux","error":{"code":"PAYLOAD_TOO_LARGE","message":"Corps de requête trop volumineux"}}`)
