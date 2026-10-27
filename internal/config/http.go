package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HTTP server limits of the API (GRC15-B07). The API runs behind a trusted
// edge (GRC15-B05); these limits are its own defense in depth when the edge
// is missing or misconfigured. Every variable is optional: unset means the
// default below, an explicit invalid value is an error (never clamped).
const (
	EnvHTTPReadHeaderTimeout = "MEDCORE_HTTP_READ_HEADER_TIMEOUT"
	EnvHTTPReadTimeout       = "MEDCORE_HTTP_READ_TIMEOUT"
	EnvHTTPWriteTimeout      = "MEDCORE_HTTP_WRITE_TIMEOUT"
	EnvHTTPIdleTimeout       = "MEDCORE_HTTP_IDLE_TIMEOUT"
	EnvHTTPShutdownTimeout   = "MEDCORE_HTTP_SHUTDOWN_TIMEOUT"
	EnvHTTPMaxHeaderBytes    = "MEDCORE_HTTP_MAX_HEADER_BYTES"
	EnvHTTPMaxBodyBytes      = "MEDCORE_HTTP_MAX_BODY_BYTES"
	EnvHTTPMaxUploadBytes    = "MEDCORE_HTTP_MAX_UPLOAD_BYTES"
)

// TicketAttachmentBodyBytes is the largest body the only multipart route
// accepts (ticketing: 5 MiB file + 1 KiB). The upload ceiling may never be
// configured below it, or legitimate attachments would break.
const TicketAttachmentBodyBytes = 5<<20 + 1024

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	// MaxBodyBytes bounds every non-multipart request body (JSON).
	MaxBodyBytes int64
	// MaxUploadBytes bounds multipart/form-data bodies; route limits apply below it.
	MaxUploadBytes int64
}

type durationSetting struct {
	env                string
	target             *time.Duration
	fallback, min, max time.Duration
}

type sizeSetting struct {
	env                string
	target             *int64
	fallback, min, max int64
}

// LoadHTTPConfig reads the HTTP limits through getenv (os.Getenv in
// production, a map in tests).
func LoadHTTPConfig(getenv func(string) string) (HTTPConfig, error) {
	var c HTTPConfig
	for _, s := range []durationSetting{
		{EnvHTTPReadHeaderTimeout, &c.ReadHeaderTimeout, 10 * time.Second, time.Second, time.Minute},
		{EnvHTTPReadTimeout, &c.ReadTimeout, 2 * time.Minute, 5 * time.Second, 10 * time.Minute},
		{EnvHTTPWriteTimeout, &c.WriteTimeout, 2 * time.Minute, 5 * time.Second, 10 * time.Minute},
		{EnvHTTPIdleTimeout, &c.IdleTimeout, 2 * time.Minute, 5 * time.Second, 10 * time.Minute},
		{EnvHTTPShutdownTimeout, &c.ShutdownTimeout, 25 * time.Second, time.Second, 2 * time.Minute},
	} {
		value, err := parseDurationEnv(getenv(s.env), s.env, s.fallback, s.min, s.max)
		if err != nil {
			return HTTPConfig{}, err
		}
		*s.target = value
	}

	var header int64
	for _, s := range []sizeSetting{
		{EnvHTTPMaxHeaderBytes, &header, 64 << 10, 8 << 10, 1 << 20},
		{EnvHTTPMaxBodyBytes, &c.MaxBodyBytes, 1 << 20, 64 << 10, 64 << 20},
		{EnvHTTPMaxUploadBytes, &c.MaxUploadBytes, 8 << 20, TicketAttachmentBodyBytes, 64 << 20},
	} {
		value, err := parseSizeEnv(getenv(s.env), s.env, s.fallback, s.min, s.max)
		if err != nil {
			return HTTPConfig{}, err
		}
		*s.target = value
	}
	c.MaxHeaderBytes = int(header)

	if c.MaxUploadBytes < c.MaxBodyBytes {
		return HTTPConfig{}, fmt.Errorf("%s doit être supérieur ou égal à %s", EnvHTTPMaxUploadBytes, EnvHTTPMaxBodyBytes)
	}
	if c.ReadHeaderTimeout > c.ReadTimeout {
		return HTTPConfig{}, fmt.Errorf("%s doit être inférieur ou égal à %s", EnvHTTPReadHeaderTimeout, EnvHTTPReadTimeout)
	}
	return c, nil
}

// parseDurationEnv accepts Go durations such as "10s" or "2m". Errors name
// the variable and the allowed range, never the raw value.
func parseDurationEnv(raw, env string, fallback, min, max time.Duration) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: durée invalide (exemple 10s)", env)
	}
	if value < min || value > max {
		return 0, fmt.Errorf("%s: doit être compris entre %s et %s", env, min, max)
	}
	return value, nil
}

// parseSizeEnv accepts a positive number of bytes.
func parseSizeEnv(raw, env string, fallback, min, max int64) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: nombre d'octets invalide", env)
	}
	if value < min || value > max {
		return 0, fmt.Errorf("%s: doit être compris entre %d et %d octets", env, min, max)
	}
	return value, nil
}
