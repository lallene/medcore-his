package middleware

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const (
	testMaxBody   = 1024
	testMaxUpload = 4096
)

// bodyRouter echoes the size read, and answers 400 when reading fails, as a
// real handler binding JSON would.
func bodyRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(testMaxBody, testMaxUpload))
	read := func(c *gin.Context) {
		data, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "invalide"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"read": len(data)})
	}
	r.POST("/json", read)
	r.POST("/upload", func(c *gin.Context) {
		// Route limit below the global upload ceiling, as for ticket attachments.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3000)
		if _, _, err := c.Request.FormFile("file"); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"message": "pièce jointe invalide"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})
	r.GET("/get", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

func send(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func jsonRequest(size int, chunked bool) *http.Request {
	body := strings.Repeat("a", size)
	req := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if chunked {
		// No Content-Length: the limit must hold while reading.
		req.ContentLength = -1
		req.Body = io.NopCloser(strings.NewReader(body))
	}
	return req
}

func multipartRequest(t *testing.T, path string, size int) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "proof.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), size)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestBodyLimitJSON(t *testing.T) {
	r := bodyRouter()

	if w := send(r, jsonRequest(100, false)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"read":100`) {
		t.Fatalf("under limit: %d %s", w.Code, w.Body.String())
	}
	// Exactly at the ceiling: accepted.
	if w := send(r, jsonRequest(testMaxBody, false)); w.Code != http.StatusOK {
		t.Fatalf("at the limit: %d %s", w.Code, w.Body.String())
	}
	// Honest Content-Length above the ceiling: refused before the handler.
	w := send(r, jsonRequest(testMaxBody+1, false))
	if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "PAYLOAD_TOO_LARGE") ||
		w.Header().Get("Connection") != "close" {
		t.Fatalf("over limit: %d %s", w.Code, w.Body.String())
	}
	// Chunked / unknown length: bounded while reading, 413 instead of the handler's 400.
	w = send(r, jsonRequest(testMaxBody*3, true))
	if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "PAYLOAD_TOO_LARGE") ||
		strings.Contains(w.Body.String(), "invalide") {
		t.Fatalf("chunked over limit: %d %s", w.Code, w.Body.String())
	}
	if w := send(r, jsonRequest(testMaxBody, true)); w.Code != http.StatusOK {
		t.Fatalf("chunked at the limit: %d %s", w.Code, w.Body.String())
	}
	// A lying Content-Length smaller than the body is still bounded.
	req := jsonRequest(testMaxBody*2, false)
	req.ContentLength = 10
	if w := send(r, req); w.Code == http.StatusOK && strings.Contains(w.Body.String(), `"read":2048`) {
		t.Fatalf("lying Content-Length read the whole body: %s", w.Body.String())
	}
	if w := send(r, httptest.NewRequest(http.MethodGet, "/get", nil)); w.Code != http.StatusOK {
		t.Fatalf("GET without body: %d", w.Code)
	}
}

func TestBodyLimitUploads(t *testing.T) {
	r := bodyRouter()

	// A multipart body above the JSON ceiling but within the upload ceiling and
	// the route limit is accepted: the JSON limit does not break uploads.
	if w := send(r, multipartRequest(t, "/upload", 2000)); w.Code != http.StatusCreated {
		t.Fatalf("upload within limits: %d %s", w.Code, w.Body.String())
	}
	// The route limit (3000) still applies below the global upload ceiling and
	// keeps the route's own error contract (ticketing answers 400 itself).
	w := send(r, multipartRequest(t, "/upload", 3500))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("upload over the route limit: %d %s", w.Code, w.Body.String())
	}
	// Above the global upload ceiling: refused before the handler.
	w = send(r, multipartRequest(t, "/upload", testMaxUpload+100))
	if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "PAYLOAD_TOO_LARGE") {
		t.Fatalf("upload over the ceiling: %d %s", w.Code, w.Body.String())
	}
	// A JSON body cannot use the upload ceiling.
	if w := send(r, jsonRequest(2000, false)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("JSON above its ceiling: %d", w.Code)
	}
}
