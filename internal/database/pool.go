package database

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Connection pool bounds of every process opening PostgreSQL through
// Connect (API, workers, migrate, seed, qa-import) — GRC15-B07. Unset means
// the default; an explicit invalid value is an error (never clamped).
//
// When MaxOpenConns is reached, database/sql makes the caller wait for a free
// connection (until the query context ends); there is no application queue.
const (
	EnvDBMaxOpenConns    = "MEDCORE_DB_MAX_OPEN_CONNS"
	EnvDBMaxIdleConns    = "MEDCORE_DB_MAX_IDLE_CONNS"
	EnvDBConnMaxLifetime = "MEDCORE_DB_CONN_MAX_LIFETIME"
	EnvDBConnMaxIdleTime = "MEDCORE_DB_CONN_MAX_IDLE_TIME"
)

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultPoolConfig keeps one process to 10 connections.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{MaxOpenConns: 10, MaxIdleConns: 5, ConnMaxLifetime: 30 * time.Minute, ConnMaxIdleTime: 5 * time.Minute}
}

// LoadPoolConfig reads the pool bounds through getenv and validates them:
// 1 <= MaxOpen <= 200, 0 <= MaxIdle <= MaxOpen, 1m <= lifetime <= 24h and
// 30s <= idle time <= lifetime.
func LoadPoolConfig(getenv func(string) string) (PoolConfig, error) {
	c := DefaultPoolConfig()
	var err error
	if c.MaxOpenConns, err = poolInt(getenv(EnvDBMaxOpenConns), EnvDBMaxOpenConns, c.MaxOpenConns, 1, 200); err != nil {
		return PoolConfig{}, err
	}
	if c.MaxIdleConns, err = poolInt(getenv(EnvDBMaxIdleConns), EnvDBMaxIdleConns, min(c.MaxIdleConns, c.MaxOpenConns), 0, 200); err != nil {
		return PoolConfig{}, err
	}
	if c.ConnMaxLifetime, err = poolDuration(getenv(EnvDBConnMaxLifetime), EnvDBConnMaxLifetime, c.ConnMaxLifetime, time.Minute, 24*time.Hour); err != nil {
		return PoolConfig{}, err
	}
	if c.ConnMaxIdleTime, err = poolDuration(getenv(EnvDBConnMaxIdleTime), EnvDBConnMaxIdleTime, c.ConnMaxIdleTime, 30*time.Second, 24*time.Hour); err != nil {
		return PoolConfig{}, err
	}
	if c.MaxIdleConns > c.MaxOpenConns {
		return PoolConfig{}, fmt.Errorf("%s doit être inférieur ou égal à %s", EnvDBMaxIdleConns, EnvDBMaxOpenConns)
	}
	if c.ConnMaxIdleTime > c.ConnMaxLifetime {
		return PoolConfig{}, fmt.Errorf("%s doit être inférieur ou égal à %s", EnvDBConnMaxIdleTime, EnvDBConnMaxLifetime)
	}
	return c, nil
}

// Apply sets the bounds on a database/sql pool.
func (c PoolConfig) Apply(db *sql.DB) {
	db.SetMaxOpenConns(c.MaxOpenConns)
	db.SetMaxIdleConns(c.MaxIdleConns)
	db.SetConnMaxLifetime(c.ConnMaxLifetime)
	db.SetConnMaxIdleTime(c.ConnMaxIdleTime)
}

func poolInt(raw, env string, fallback, minValue, maxValue int) (int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: entier invalide", env)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s: doit être compris entre %d et %d", env, minValue, maxValue)
	}
	return value, nil
}

func poolDuration(raw, env string, fallback, minValue, maxValue time.Duration) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: durée invalide (exemple 30m)", env)
	}
	if value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s: doit être compris entre %s et %s", env, minValue, maxValue)
	}
	return value, nil
}
