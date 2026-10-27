package config

import (
	"strings"
	"testing"
	"time"
)

func envMap(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadHTTPConfigDefaults(t *testing.T) {
	c, err := LoadHTTPConfig(envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := HTTPConfig{
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute,
		IdleTimeout: 2 * time.Minute, ShutdownTimeout: 25 * time.Second,
		MaxHeaderBytes: 64 << 10, MaxBodyBytes: 1 << 20, MaxUploadBytes: 8 << 20,
	}
	if c != want {
		t.Fatalf("defaults = %+v, want %+v", c, want)
	}
	// The upload ceiling never breaks the 5 MiB ticket attachment route.
	if c.MaxUploadBytes < TicketAttachmentBodyBytes {
		t.Fatal("default upload ceiling below the ticket attachment limit")
	}
}

func TestLoadHTTPConfigOverrides(t *testing.T) {
	c, err := LoadHTTPConfig(envMap(map[string]string{
		EnvHTTPReadHeaderTimeout: " 5s ", EnvHTTPReadTimeout: "1m", EnvHTTPWriteTimeout: "90s",
		EnvHTTPIdleTimeout: "30s", EnvHTTPShutdownTimeout: "10s",
		EnvHTTPMaxHeaderBytes: "32768", EnvHTTPMaxBodyBytes: "2097152", EnvHTTPMaxUploadBytes: "10485760",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ReadHeaderTimeout != 5*time.Second || c.ReadTimeout != time.Minute || c.WriteTimeout != 90*time.Second ||
		c.IdleTimeout != 30*time.Second || c.ShutdownTimeout != 10*time.Second ||
		c.MaxHeaderBytes != 32768 || c.MaxBodyBytes != 2<<20 || c.MaxUploadBytes != 10<<20 {
		t.Fatalf("overrides = %+v", c)
	}
}

func TestLoadHTTPConfigRejectsInvalidValues(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"malformed duration":        {EnvHTTPReadTimeout: "soon-secret"},
		"bare number duration":      {EnvHTTPIdleTimeout: "30"},
		"zero header timeout":       {EnvHTTPReadHeaderTimeout: "0s"},
		"negative write timeout":    {EnvHTTPWriteTimeout: "-5s"},
		"shutdown too long":         {EnvHTTPShutdownTimeout: "1h"},
		"header too small":          {EnvHTTPMaxHeaderBytes: "1024"},
		"header not a number":       {EnvHTTPMaxHeaderBytes: "64k"},
		"body negative":             {EnvHTTPMaxBodyBytes: "-1"},
		"body too large":            {EnvHTTPMaxBodyBytes: "1073741824"},
		"upload below attachments":  {EnvHTTPMaxUploadBytes: "1048576"},
		"upload below body":         {EnvHTTPMaxBodyBytes: "16777216", EnvHTTPMaxUploadBytes: "8388608"},
		"header after read timeout": {EnvHTTPReadHeaderTimeout: "30s", EnvHTTPReadTimeout: "10s"},
	} {
		_, err := LoadHTTPConfig(envMap(values))
		if err == nil {
			t.Errorf("%s accepted", name)
			continue
		}
		for _, raw := range values {
			if strings.Contains(err.Error(), "secret") && strings.Contains(raw, "secret") {
				t.Errorf("%s: error echoes the raw value: %v", name, err)
			}
		}
	}
}
