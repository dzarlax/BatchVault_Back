package controllers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"mobile-backend-go/controllers"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
)

// Exercise the actual package/product handlers against independent PostgreSQL connections.
func TestPackageMutationObservesProductCommittedWhileWaiting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		weight  *models.Decimal
		target  int
		confirm bool
		want    int
	}{
		{"established_used_weight", decimal("100"), 200, false, 409},
		{"initialization_requires_review", nil, 100, false, 409},
		{"initialization_checks_new_assortment", nil, 200, true, 409},
		{"matching_reviewed_initialization", nil, 100, true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, admin, schema := packageRaceDatabase(t)
			previous := database.DB
			database.DB = db
			t.Cleanup(func() { database.DB = previous })
			must := func(value any) {
				t.Helper()
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			user := models.User{Username: "synthetic-package-race", Password: "unused"}
			must(&user)
			ws := models.Workspace{Name: "Synthetic review", Slug: "synthetic-review"}
			must(&ws)
			pkg := models.Package{Name: "Synthetic format", UserID: user.ID, WorkspaceID: &ws.ID, NetWeightG: tc.weight, UnitCost: decimal("0")}
			must(&pkg)
			a := models.Recipe{Name: "A", UserID: user.ID, WorkspaceID: &ws.ID}
			b := models.Recipe{Name: "B", UserID: user.ID, WorkspaceID: &ws.ID}
			must(&a)
			must(&b)
			inserted, release := make(chan struct{}), make(chan struct{})
			var insertOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			if err := db.Callback().Create().After("gorm:create").Register("test:pause_product", func(tx *gorm.DB) {
				if tx.Statement.Table == "products" {
					insertOnce.Do(func() { close(inserted); <-release })
				}
			}); err != nil {
				t.Fatal(err)
			}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("userID", user.ID); c.Set("workspaceID", ws.ID) })
			router.POST("/products", controllers.CreateProduct)
			router.PATCH("/packages/:id", controllers.UpdatePackage)
			request := func(method, path string, v any) *httptest.ResponseRecorder {
				data, _ := json.Marshal(v)
				req := httptest.NewRequest(method, path, bytes.NewReader(data))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			created, patched := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			go func() {
				created <- request("POST", "/products", map[string]any{"name": "Synthetic assortment", "price": 10, "package_id": pkg.ID, "cost_mode": "assortment", "components": []any{map[string]any{"recipe_id": a.ID, "quantity_g": 50}, map[string]any{"recipe_id": b.ID, "quantity_g": 50}}})
			}()
			select {
			case <-inserted:
			case <-time.After(5 * time.Second):
				t.Fatal("product did not reach the test barrier")
			}
			go func() {
				patched <- request("PATCH", fmt.Sprintf("/packages/%d", pkg.ID), map[string]any{"net_weight_g": tc.target, "confirm_weight_initialization": tc.confirm})
			}()
			blocked := false
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				var count int64
				if err := admin.Raw("SELECT count(*) FROM pg_stat_activity WHERE application_name = ? AND wait_event_type = 'Lock'", schema).Scan(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count > 0 {
					blocked = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			unblock()
			var cr, pr *httptest.ResponseRecorder
			select {
			case cr = <-created:
			case <-time.After(5 * time.Second):
				t.Fatal("product request did not finish")
			}
			select {
			case pr = <-patched:
			case <-time.After(5 * time.Second):
				t.Fatal("package request did not finish")
			}
			if !blocked {
				t.Fatal("test did not exercise the package lock wait")
			}
			if cr.Code != 201 {
				t.Fatalf("create: %d %s", cr.Code, cr.Body)
			}
			if pr.Code != tc.want {
				t.Fatalf("package: %d %s; want %d", pr.Code, pr.Body, tc.want)
			}
			var saved models.Package
			if err := db.First(&saved, pkg.ID).Error; err != nil {
				t.Fatal(err)
			}
			want := tc.weight
			if tc.want == 200 {
				want = decimal(fmt.Sprint(tc.target))
			}
			if (saved.NetWeightG == nil) != (want == nil) {
				t.Fatalf("unexpected initialized weight: %v", saved.NetWeightG)
			}
			if want != nil {
				var actual string
				if err := db.Raw("SELECT net_weight_g::text FROM packages WHERE id = ?", pkg.ID).Scan(&actual).Error; err != nil {
					t.Fatal(err)
				}
				if actual != fmt.Sprintf("%s.000", *want) {
					t.Fatalf("weight = %s, want %s", actual, *want)
				}
			}
		})
	}
}

func decimal(s string) *models.Decimal { d := models.Decimal(s); return &d }

func packageRaceDatabase(t *testing.T) (*gorm.DB, *gorm.DB, string) {
	t.Helper()
	raw := os.Getenv("COSTING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("COSTING_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Path != "/costing_test" {
		t.Fatal("test requires the disposable localhost costing_test database")
	}
	admin, err := gorm.Open(postgres.Open(raw), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("package_race_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); sqlDB, _ := admin.DB(); _ = sqlDB.Close() })
	query := parsed.Query()
	query.Set("search_path", schema)
	query.Set("application_name", schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.Ingredient{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Price{}, &models.Package{}, &models.Product{}, &models.ProductOption{}, &models.ProductCostComponent{}); err != nil {
		t.Fatal(err)
	}
	return db, admin, schema
}
