package database_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/lallene/medcore-his/backend/internal/database"
)

// Proves production Connect enforces business timezone on every physical pooled
// connection (RuntimeParams + AfterConnect set_config). Holds MaxOpenConns
// distinct pooled connections concurrently via sql.DB.Conn and checks
// current_setting('TimeZone') on each — without a post-hoc SET TIME ZONE on
// the assertion handle.
func TestConnectAppliesBusinessTimezoneToPooledConnections(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent")
	}
	const want = "America/New_York"
	const n = 4
	db := database.Connect(dsn, want)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := sqlDB.Close(); e != nil {
			t.Errorf("sqlDB.Close: %v", e)
		}
	})
	sqlDB.SetMaxOpenConns(n)
	sqlDB.SetMaxIdleConns(n)

	ctx := context.Background()
	conns := make([]*sql.Conn, 0, n)
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})
	for i := 0; i < n; i++ {
		c, e := sqlDB.Conn(ctx)
		if e != nil {
			t.Fatalf("Conn %d: %v", i, e)
		}
		conns = append(conns, c)
	}
	if len(conns) != n {
		t.Fatalf("held %d connections want %d", len(conns), n)
	}
	for i, c := range conns {
		var tz string
		if e := c.QueryRowContext(ctx, "SELECT current_setting('TimeZone')").Scan(&tz); e != nil {
			t.Fatalf("conn %d: %v", i, e)
		}
		if tz != want {
			t.Fatalf("conn %d TimeZone=%q want %q", i, tz, want)
		}
	}
}
