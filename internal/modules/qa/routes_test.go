package qa

import (
	"github.com/gin-gonic/gin"
	"testing"
)

func TestQATableNamesAndReadOnlyRoutes(t *testing.T) {
	if (Campaign{}).TableName() != "qa_campaigns" || (TestResult{}).TableName() != "qa_test_results" || (Artifact{}).TableName() != "qa_artifacts" {
		t.Fatal("explicit QA table names missing")
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	RegisterRoutes(g, NewHandler(NewService(qaDB(t))))
	for _, route := range r.Routes() {
		if route.Method != "GET" {
			t.Fatalf("QA route %s permits mutation %s", route.Path, route.Method)
		}
	}
}
