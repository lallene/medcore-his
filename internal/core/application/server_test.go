package application

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/config"
)

func testHTTPConfig() config.HTTPConfig {
	c, err := config.LoadHTTPConfig(func(string) string { return "" })
	if err != nil {
		panic(err)
	}
	return c
}

func TestNewHTTPServerAppliesLimits(t *testing.T) {
	cfg := testHTTPConfig()
	srv := newHTTPServer(cfg, ":8080", http.NewServeMux())
	if srv.Addr != ":8080" || srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 2*time.Minute ||
		srv.WriteTimeout != 2*time.Minute || srv.IdleTimeout != 2*time.Minute || srv.MaxHeaderBytes != 64<<10 {
		t.Fatalf("server = %+v", srv)
	}
	if srv.TLSConfig != nil {
		t.Fatal("TLS belongs to the edge")
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestServeDrainsInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(150 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	})
	ln := listen(t)
	srv := newHTTPServer(testHTTPConfig(), ln.Addr().String(), handler)
	ctx, cancel := context.WithCancel(context.Background())
	var drained atomic.Bool
	result := make(chan error, 1)
	go func() { result <- serve(ctx, srv, ln, 2*time.Second, func() { drained.Store(true) }) }()

	response := make(chan string, 1)
	go func() {
		res, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			response <- "error: " + err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		response <- string(body)
	}()
	<-started
	cancel() // SIGINT/SIGTERM path

	if got := <-response; got != "done" {
		t.Fatalf("in-flight request: %s", got)
	}
	if err := <-result; err != nil {
		t.Fatalf("clean stop returned %v", err)
	}
	if !drained.Load() {
		t.Fatal("readiness was not switched to draining")
	}
	// New connections are refused after the stop.
	if _, err := http.Get("http://" + ln.Addr().String() + "/"); err == nil {
		t.Fatal("server still accepts connections")
	}
}

func TestServeForcesExitAfterShutdownTimeout(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})
	defer close(release)
	ln := listen(t)
	srv := newHTTPServer(testHTTPConfig(), ln.Addr().String(), handler)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- serve(ctx, srv, ln, 100*time.Millisecond, nil) }()
	go func() { _, _ = http.Get("http://" + ln.Addr().String() + "/") }()
	<-started
	begin := time.Now()
	cancel()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "forcé") {
			t.Fatalf("forced stop error = %v", err)
		}
		if elapsed := time.Since(begin); elapsed > time.Second {
			t.Fatalf("shutdown not bounded: %s", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after the shutdown timeout")
	}
}

func TestServeReportsUnexpectedStop(t *testing.T) {
	ln := listen(t)
	srv := newHTTPServer(testHTTPConfig(), ln.Addr().String(), http.NewServeMux())
	_ = ln.Close() // Serve fails immediately on a closed listener
	if err := serve(context.Background(), srv, ln, time.Second, nil); err == nil {
		t.Fatal("a failed listener must not be a clean stop")
	}
}

func healthApp(t *testing.T) (*Application, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	app := &Application{DB: db, Router: gin.New()}
	app.registerCoreRoutes()
	return app, db
}

func getHealth(app *Application) (int, string) {
	req, _ := http.NewRequest(http.MethodGet, "/health", nil)
	w := &recorder{header: http.Header{}}
	app.Router.ServeHTTP(w, req)
	return w.code, w.body.String()
}

type recorder struct {
	header http.Header
	code   int
	body   strings.Builder
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(b)
}
func (r *recorder) WriteHeader(code int) { r.code = code }

func TestHealthReadiness(t *testing.T) {
	app, db := healthApp(t)

	if code, body := getHealth(app); code != http.StatusOK || !strings.Contains(body, `"db":"up"`) {
		t.Fatalf("ready: %d %s", code, body)
	}
	app.draining.Store(true)
	if code, body := getHealth(app); code != http.StatusServiceUnavailable || !strings.Contains(body, "draining") {
		t.Fatalf("draining: %d %s", code, body)
	}
	app.draining.Store(false)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	_ = sqlDB.Close()
	code, body := getHealth(app)
	if code != http.StatusServiceUnavailable || body != `{"db":"down","status":"error"}` {
		t.Fatalf("database down: %d %s", code, body)
	}
}
