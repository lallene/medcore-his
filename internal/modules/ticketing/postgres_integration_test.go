package ticketing

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ticketingPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: tests PostgreSQL Ticketing ignorés")
	}
	admin, e := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("ticketing_%d", time.Now().UnixNano())
	if e = admin.Exec(`CREATE SCHEMA "` + schema + `"`).Error; e != nil {
		t.Fatal(e)
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	db, e := gorm.Open(postgres.Open(dsn+sep+"search_path="+url.QueryEscape(schema)), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close(); admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`) })
	if e = db.AutoMigrate(&Category{}, &SLA{}, &Ticket{}, &Comment{}, &Attachment{}, &Assignment{}, &History{}, &Notification{}); e != nil {
		t.Fatal(e)
	}
	return db
}

func TestPostgresConcurrentTicketReferences(t *testing.T) {
	db := ticketingPostgres(t)
	service := NewService(db)
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	const count = 12
	refs := make(chan string, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := db.Transaction(func(tx *gorm.DB) error {
				ref, e := service.nextReference(tx, "INCIDENT", now)
				if e != nil {
					return e
				}
				ticket := Ticket{Reference: ref, Type: "INCIDENT", Title: "Concurrency", Description: "Test", Status: "NEW", Priority: "P3", Impact: "SERVICE", Urgency: "MEDIUM", RequesterUserID: 1, ResponseDueAt: now.Add(time.Hour), ResolutionDueAt: now.Add(4 * time.Hour), CreatedAt: now, UpdatedAt: now}
				if e = tx.Create(&ticket).Error; e != nil {
					return e
				}
				refs <- ref
				return nil
			})
			errs <- e
		}()
	}
	wg.Wait()
	close(refs)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	seen := map[string]bool{}
	for ref := range refs {
		if seen[ref] {
			t.Fatalf("duplicate reference %s", ref)
		}
		seen[ref] = true
	}
	if len(seen) != count {
		t.Fatalf("got %d references", len(seen))
	}
}

func TestPostgresInternalCommentsAndRollback(t *testing.T) {
	db := ticketingPostgres(t)
	now := time.Now()
	ticket := Ticket{Reference: "INC-2026-000001", Type: "INCIDENT", Title: "RBAC", Description: "Test", Status: "NEW", Priority: "P3", Impact: "SERVICE", Urgency: "MEDIUM", RequesterUserID: 1, ResponseDueAt: now.Add(time.Hour), ResolutionDueAt: now.Add(4 * time.Hour)}
	if e := db.Create(&ticket).Error; e != nil {
		t.Fatal(e)
	}
	service := NewService(db)
	if _, e := service.AddComment(ticket.ID, CommentRequest{Content: "hidden", Visibility: "INTERNAL"}, Access{UserID: 1, Permissions: map[string]bool{"ticket.read.own": true, "ticket.comment": true}}); e == nil {
		t.Fatal("internal comment accepted without permission")
	}
	var n int64
	db.Model(&Comment{}).Count(&n)
	if n != 0 {
		t.Fatal("forbidden comment was persisted")
	}
}
