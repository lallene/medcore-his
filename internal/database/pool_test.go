package database

import (
	"database/sql"
	"strings"
	"testing"
	"time"
)

func poolEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadPoolConfigDefaultsAreBounded(t *testing.T) {
	c, err := LoadPoolConfig(poolEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c != DefaultPoolConfig() || c.MaxOpenConns != 10 || c.MaxIdleConns != 5 ||
		c.ConnMaxLifetime != 30*time.Minute || c.ConnMaxIdleTime != 5*time.Minute {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestLoadPoolConfigOverridesAndValidation(t *testing.T) {
	c, err := LoadPoolConfig(poolEnv(map[string]string{
		EnvDBMaxOpenConns: "20", EnvDBMaxIdleConns: "20", EnvDBConnMaxLifetime: "1h", EnvDBConnMaxIdleTime: "1h",
	}))
	if err != nil || c.MaxOpenConns != 20 || c.MaxIdleConns != 20 || c.ConnMaxLifetime != time.Hour || c.ConnMaxIdleTime != time.Hour {
		t.Fatalf("overrides: %v %+v", err, c)
	}
	// MaxOpen below the default idle count lowers the idle default instead of failing.
	c, err = LoadPoolConfig(poolEnv(map[string]string{EnvDBMaxOpenConns: "2"}))
	if err != nil || c.MaxOpenConns != 2 || c.MaxIdleConns != 2 {
		t.Fatalf("small pool: %v %+v", err, c)
	}

	for name, values := range map[string]map[string]string{
		"zero open":            {EnvDBMaxOpenConns: "0"},
		"negative open":        {EnvDBMaxOpenConns: "-3"},
		"open too large":       {EnvDBMaxOpenConns: "1000"},
		"open not a number":    {EnvDBMaxOpenConns: "ten"},
		"negative idle":        {EnvDBMaxIdleConns: "-1"},
		"idle above open":      {EnvDBMaxOpenConns: "4", EnvDBMaxIdleConns: "5"},
		"malformed lifetime":   {EnvDBConnMaxLifetime: "forever"},
		"lifetime too short":   {EnvDBConnMaxLifetime: "1s"},
		"idle time too short":  {EnvDBConnMaxIdleTime: "1s"},
		"idle above lifetime":  {EnvDBConnMaxLifetime: "10m", EnvDBConnMaxIdleTime: "20m"},
		"bare number lifetime": {EnvDBConnMaxLifetime: "30"},
	} {
		if _, err := LoadPoolConfig(poolEnv(values)); err == nil {
			t.Errorf("%s accepted", name)
		} else if strings.Contains(err.Error(), "forever") {
			t.Errorf("%s: error echoes the raw value", name)
		}
	}
}

func TestPoolConfigApply(t *testing.T) {
	// The pgx stdlib driver opens lazily: no connection is attempted here.
	db, err := sql.Open("pgx", "postgres://user:secret@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	PoolConfig{MaxOpenConns: 7, MaxIdleConns: 3, ConnMaxLifetime: time.Hour, ConnMaxIdleTime: time.Minute}.Apply(db)
	if got := db.Stats().MaxOpenConnections; got != 7 {
		t.Fatalf("MaxOpenConnections=%d, want 7", got)
	}
}
