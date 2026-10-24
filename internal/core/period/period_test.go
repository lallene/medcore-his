package period

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ctx(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/x?"+rawQuery, nil)
	return c
}

func TestParse(t *testing.T) {
	f, err := Parse(ctx("serviceId=2&from=2026-09-25&to=2026-10-25"))
	if err != nil || f.ServiceID == nil || *f.ServiceID != 2 || f.From != "2026-09-25" || f.To != "2026-10-25" || !f.HasPeriod() {
		t.Fatalf("parse inattendu: %+v %v", f, err)
	}
	if f, err := Parse(ctx("")); err != nil || f.ServiceID != nil || f.HasPeriod() {
		t.Fatalf("filtre vide inattendu: %+v %v", f, err)
	}
	for _, q := range []string{"serviceId=0", "serviceId=-1", "serviceId=x", "from=2026-13-01", "to=24/10/2026", "from=2026-10-24&to=2026-10-24", "from=2026-10-25&to=2026-10-24"} {
		if _, err := Parse(ctx(q)); err == nil {
			t.Fatalf("%q devrait être rejeté", q)
		}
	}
}

func TestApplyHalfOpenCivilPeriod(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 dbname=dryrun"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	id := uint(3)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var n int64
		return Filter{ServiceID: &id, From: "2026-10-18", To: "2026-10-25"}.Apply(tx.Table("x"), "d", "s").Count(&n)
	})
	for _, part := range []string{"s = 3", "d >= '2026-10-18'::date", "d < '2026-10-25'::date"} {
		if !strings.Contains(sql, part) {
			t.Fatalf("SQL sans %q : %s", part, sql)
		}
	}
}
