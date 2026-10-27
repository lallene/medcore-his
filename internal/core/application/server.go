package application

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/lallene/medcore-his/backend/internal/config"
)

// newHTTPServer builds the API server with explicit limits (GRC15-B07). TLS
// stays with the edge (GRC15-B05): the API listens in plain HTTP on a private
// port. The handler is the unchanged Gin router.
func newHTTPServer(cfg config.HTTPConfig, addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}
}

// serve runs srv on ln until ctx is cancelled (SIGINT/SIGTERM), then calls
// onDrain (readiness turns 503), stops accepting connections and waits for
// in-flight requests at most shutdownTimeout before closing the remaining
// connections. It returns nil on a clean stop.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration, onDrain func()) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		// The server stopped on its own: never a clean stop.
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return errors.New("serveur HTTP arrêté de façon inattendue")
		}
		return err
	case <-ctx.Done():
	}

	if onDrain != nil {
		onDrain()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Bound reached: force the remaining connections closed.
		_ = srv.Close()
		<-serveErr
		return errors.New("arrêt HTTP forcé après le délai de grâce")
	}
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
