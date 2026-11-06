package controllers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"mobile-backend-go/controllers"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
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
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.Ingredient{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Price{}, &models.Package{}, &models.Product{}, &models.ProductOption{}, &models.ProductCostComponent{}, &models.ProductAssortmentComponent{}); err != nil {
		t.Fatal(err)
	}
	return db, admin, schema
}

func TestProductDeleteWaitsForNewAssortmentReference(t *testing.T) {
	db, admin, schema := packageRaceDatabase(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	user := models.User{Username: "graph-race", Password: "unused"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	ws := models.Workspace{Name: "Graph", Slug: "graph"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.Package{Name: "Pack", UserID: user.ID, WorkspaceID: &ws.ID, NetWeightG: decimal("100"), UnitCost: decimal("0")}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}
	a := models.Product{Name: "A", Price: 10, ProductType: "standard", PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: user.ID}
	b := models.Product{Name: "B", Price: 10, ProductType: "standard", PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: user.ID}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatal(err)
	}
	inserted, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	if err := db.Callback().Create().After("gorm:create").Register("test:pause_graph_reference", func(tx *gorm.DB) {
		if tx.Statement.Table == "product_assortment_components" {
			once.Do(func() { close(inserted); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", user.ID); c.Set("workspaceID", ws.ID) })
	router.POST("/products", controllers.CreateProduct)
	router.DELETE("/products/:id", controllers.DeleteProduct)
	request := func(method, path string, v any) *httptest.ResponseRecorder {
		data, _ := json.Marshal(v)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	created, deleted := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	go func() {
		created <- request("POST", "/products", map[string]any{"name": "Mix", "price": 10, "package_id": pkg.ID, "product_type": "assortment", "composition_basis": "products", "cost_mode": "manual", "cost": 0, "assortment_components": []any{map[string]any{"component_product_id": a.ID, "quantity_g": 50}, map[string]any{"component_product_id": b.ID, "quantity_g": 50}}})
	}()
	select {
	case <-inserted:
	case <-time.After(5 * time.Second):
		t.Fatal("create did not reach barrier")
	}
	go func() { deleted <- request("DELETE", fmt.Sprintf("/products/%d", a.ID), nil) }()
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
	cr, dr := <-created, <-deleted
	if !blocked || cr.Code != 201 || dr.Code != 409 {
		t.Fatalf("blocked=%v create=%d %s delete=%d %s", blocked, cr.Code, cr.Body, dr.Code, dr.Body)
	}
}

func TestCostingRepeatableReadKeepsYieldAndPriceSnapshot(t *testing.T) {
	db, _, _ := packageRaceDatabase(t)
	u := models.User{Username: "rr-costing", Password: "unused"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	ws := models.Workspace{Name: "RR", Slug: "rr"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	ingredient := models.Ingredient{Name: "content", Type: "meat"}
	if err := db.Create(&ingredient).Error; err != nil {
		t.Fatal(err)
	}
	recipe := models.Recipe{Name: "recipe", UserID: u.ID, WorkspaceID: &ws.ID, PlannedYieldG: decimal("100")}
	if err := db.Create(&recipe).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.RecipeIngredient{RecipeID: recipe.ID, IngredientID: ingredient.ID, Quantity: "1", Unit: "kg"}).Error; err != nil {
		t.Fatal(err)
	}
	price := models.Price{IngredientID: ingredient.ID, UserID: u.ID, Price: 100, Quantity: 1, Unit: "kg", WorkspaceID: &ws.ID, Date: time.Now()}
	if err := db.Create(&price).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.Package{Name: "pack", UserID: u.ID, WorkspaceID: &ws.ID, NetWeightG: decimal("100"), UnitCost: decimal("0")}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}
	product := models.Product{Name: "product", Price: 10, CostMode: "recipe", CostRecipeID: &recipe.ID, PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: u.ID}
	if err := db.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	anchored, changed := make(chan struct{}), make(chan error, 1)
	result := make(chan error, 1)
	go func() {
		result <- database.CostingTransaction(db, true, func(tx *gorm.DB) error {
			var p models.Product
			if err := tx.First(&p, product.ID).Error; err != nil {
				return err
			}
			close(anchored)
			if err := <-changed; err != nil {
				return err
			}
			rows := []models.Product{p}
			if err := services.NewCosting(tx, ws.ID).AttachProducts(rows); err != nil {
				return err
			}
			if rows[0].CostEstimate.TotalCost == nil || *rows[0].CostEstimate.TotalCost != 100 {
				return fmt.Errorf("snapshot estimate: %+v", rows[0].CostEstimate)
			}
			return nil
		})
	}()
	<-anchored
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&recipe).Update("planned_yield_g", "200").Error; err != nil {
			return err
		}
		return tx.Model(&price).Update("price", 400).Error
	})
	changed <- err
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestProductVersionCASConcurrentUpdates(t *testing.T) {
	db, _, _ := packageRaceDatabase(t)
	previous := database.DB
	database.DB = db
	t.Cleanup(func() { database.DB = previous })
	u := models.User{Username: "cas", Password: "unused"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	ws := models.Workspace{Name: "CAS", Slug: "cas"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	pkg := models.Package{Name: "pack", UserID: u.ID, WorkspaceID: &ws.ID}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}
	p := models.Product{Name: "source", Price: 10, ProductType: "standard", CostMode: "manual", PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: u.ID}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", u.ID); c.Set("workspaceID", ws.ID) })
	router.PUT("/products/:id", controllers.UpdateProduct)
	results := make(chan int, 2)
	start := make(chan struct{})
	for _, name := range []string{"first", "second"} {
		go func(name string) {
			<-start
			data, _ := json.Marshal(map[string]any{"name": name, "price": 10, "package_id": pkg.ID, "product_type": "standard", "cost_mode": "manual", "cost": 0, "configuration_version": p.ConfigurationVersion})
			req := httptest.NewRequest("PUT", fmt.Sprintf("/products/%d", p.ID), bytes.NewReader(data))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results <- w.Code
		}(name)
	}
	close(start)
	a, b := <-results, <-results
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("results %d, %d", a, b)
	}
}

func TestProductWriteResponseUsesFreshCostingSnapshot(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		t.Run(method, func(t *testing.T) {
			db, _, _ := packageRaceDatabase(t)
			previous := database.DB
			database.DB = db
			t.Cleanup(func() { database.DB = previous })
			u := models.User{Username: "response", Password: "unused"}
			if err := db.Create(&u).Error; err != nil {
				t.Fatal(err)
			}
			ws := models.Workspace{Name: "Response", Slug: "response"}
			if err := db.Create(&ws).Error; err != nil {
				t.Fatal(err)
			}
			ingredient := models.Ingredient{Name: "content", Type: "meat"}
			if err := db.Create(&ingredient).Error; err != nil {
				t.Fatal(err)
			}
			recipe := models.Recipe{Name: "recipe", UserID: u.ID, WorkspaceID: &ws.ID, PlannedYieldG: decimal("100")}
			if err := db.Create(&recipe).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.RecipeIngredient{RecipeID: recipe.ID, IngredientID: ingredient.ID, Quantity: "1", Unit: "kg"}).Error; err != nil {
				t.Fatal(err)
			}
			price := models.Price{IngredientID: ingredient.ID, UserID: u.ID, Price: 100, Quantity: 1, Unit: "kg", WorkspaceID: &ws.ID, Date: time.Now()}
			if err := db.Create(&price).Error; err != nil {
				t.Fatal(err)
			}
			pkg := models.Package{Name: "pack", UserID: u.ID, WorkspaceID: &ws.ID, NetWeightG: decimal("100"), UnitCost: decimal("0")}
			if err := db.Create(&pkg).Error; err != nil {
				t.Fatal(err)
			}
			p := models.Product{Name: "old", Price: 10, ProductType: "standard", CostMode: "recipe", CostRecipeID: &recipe.ID, PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: u.ID}
			if method == "PUT" {
				if err := db.Create(&p).Error; err != nil {
					t.Fatal(err)
				}
			}
			reached, release := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			var wrote atomic.Bool
			readReached, readRelease := make(chan struct{}), make(chan struct{})
			var readOnce, readReleaseOnce sync.Once
			releaseRead := func() { readReleaseOnce.Do(func() { close(readRelease) }) }
			t.Cleanup(releaseRead)
			if err := db.Callback().Query().After("gorm:query").Register("test:response_read_barrier", func(tx *gorm.DB) {
				if wrote.Load() && tx.Statement.Table == "products" {
					readOnce.Do(func() { close(readReached); <-readRelease })
				}
			}); err != nil {
				t.Fatal(err)
			}
			callback := func(tx *gorm.DB) {
				if tx.Statement.Table == "products" {
					once.Do(func() { close(reached); <-release; wrote.Store(true) })
				}
			}
			if method == "POST" {
				if err := db.Callback().Create().After("gorm:create").Register("test:response_barrier", callback); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Callback().Update().After("gorm:update").Register("test:response_barrier", callback); err != nil {
					t.Fatal(err)
				}
			}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("userID", u.ID); c.Set("workspaceID", ws.ID) })
			router.POST("/products", controllers.CreateProduct)
			router.PUT("/products/:id", controllers.UpdateProduct)
			body := map[string]any{"name": "saved", "price": 10, "package_id": pkg.ID, "product_type": "standard", "cost_mode": "recipe", "recipe_id": recipe.ID, "cost_recipe_id": recipe.ID}
			path := "/products"
			if method == "PUT" {
				path = fmt.Sprintf("/products/%d", p.ID)
				body["configuration_version"] = p.ConfigurationVersion
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				data, _ := json.Marshal(body)
				req := httptest.NewRequest(method, path, bytes.NewReader(data))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				done <- w
			}()
			select {
			case <-reached:
			case <-time.After(5 * time.Second):
				t.Fatal("write did not reach barrier")
			}
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&recipe).Update("planned_yield_g", "200").Error; err != nil {
					return err
				}
				return tx.Model(&price).Update("price", 400).Error
			})
			if err != nil {
				unblock()
				t.Fatal(err)
			}
			unblock()
			select {
			case <-readReached:
			case <-time.After(5 * time.Second):
				t.Fatal("fresh response read did not reach barrier")
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&recipe).Update("planned_yield_g", "400").Error; err != nil {
					return err
				}
				return tx.Model(&price).Update("price", 1600).Error
			})
			if err != nil {
				releaseRead()
				t.Fatal(err)
			}
			releaseRead()
			var response *httptest.ResponseRecorder
			select {
			case response = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("write did not finish")
			}
			want := 201
			if method == "PUT" {
				want = 200
			}
			if response.Code != want {
				t.Fatalf("response %d %s", response.Code, response.Body)
			}
			var saved models.Product
			if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
				t.Fatal(err)
			}
			if saved.CostEstimate == nil || saved.CostEstimate.TotalCost == nil || *saved.CostEstimate.TotalCost != 200 {
				t.Fatalf("response reused old or mixed snapshot: %s", response.Body)
			}
		})
	}
}
