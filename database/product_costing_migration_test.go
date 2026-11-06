package database_test

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"mobile-backend-go/services"
)

// This integration test is deliberately restricted to a disposable localhost database.
func TestProductCostingPostgresMigrationAndCalculation(t *testing.T) {
	raw := os.Getenv("COSTING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("COSTING_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || parsed.Path != "/costing_test" {
		t.Fatal("integration tests require the disposable localhost costing_test database")
	}
	admin, err := gorm.Open(postgres.Open(raw), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("costing_test_%d", time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); sqlDB, _ := admin.DB(); sqlDB.Close() })
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.Ingredient{}, &models.Price{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Package{}, &models.Product{}, &models.ProductCostComponent{}, &models.Client{}, &models.Order{}, &models.OrderItem{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.MigrateProductCosting(db); err != nil {
			t.Fatal(err)
		}
	}
	must := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	d := func(v string) *models.Decimal { x := models.Decimal(v); return &x }
	user := models.User{Username: "isolated-costing", Password: "hashed"}
	must(&user)
	ws := models.Workspace{Name: "Isolated costing", Slug: "isolated-costing"}
	must(&ws)
	ing := models.Ingredient{Name: "Test content", Type: "meat"}
	must(&ing)
	recipe := models.Recipe{Name: "Test recipe", UserID: user.ID, WorkspaceID: &ws.ID, PlannedYieldG: d("2")}
	must(&recipe)
	must(&models.RecipeIngredient{RecipeID: recipe.ID, IngredientID: ing.ID, Quantity: "1", Unit: "kg"})
	must(&models.Price{IngredientID: ing.ID, Price: 1.13, UserID: user.ID, Quantity: 1, Unit: "kg", Date: time.Now(), WorkspaceID: &ws.ID})
	pkg := models.Package{Name: "Test package", WorkspaceID: &ws.ID, UserID: user.ID, NetWeightG: d("1"), UnitCost: d("0")}
	must(&pkg)
	manual := models.Product{Name: "Legacy manual", Price: 10, Cost: 5.123, PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: user.ID}
	must(&manual)
	if err := database.MigrateProductCosting(db); err != nil {
		t.Fatal(err)
	}
	var saved models.Product
	if err := db.First(&saved, manual.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Cost != 5.123 || saved.CostMode != "manual" {
		t.Fatal("migration changed manual amount or mode")
	}
	product := models.Product{Name: "Calculated", Price: 5, CostMode: "recipe", CostRecipeID: &recipe.ID, PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: user.ID}
	must(&product)
	var estimate *models.CostEstimate
	if err := database.CostingTransaction(db, true, func(tx *gorm.DB) error {
		rows := []models.Product{product}
		if err := services.NewCosting(tx, ws.ID).AttachProducts(rows); err != nil {
			return err
		}
		estimate = rows[0].CostEstimate
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if estimate.TotalCost == nil || *estimate.TotalCost != 0.57 {
		t.Fatalf("PostgreSQL decimal tie: %+v", estimate)
	}
	var packageReload models.Package
	db.First(&packageReload, pkg.ID)
	if packageReload.NetWeightG == nil || !strings.HasPrefix(string(*packageReload.NetWeightG), "1") {
		t.Fatal("numeric scan failed")
	}
	component := models.ProductCostComponent{WorkspaceID: ws.ID, ProductID: product.ID, RecipeID: recipe.ID, QuantityG: "0.125"}
	must(&component)
	duplicate := models.ProductCostComponent{WorkspaceID: ws.ID, ProductID: product.ID, RecipeID: recipe.ID, QuantityG: "0.125"}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("active duplicate recipe accepted")
	}
	if err := db.Delete(&component).Error; err != nil {
		t.Fatal(err)
	}
	duplicate.ID = 0
	must(&duplicate)
	client := models.Client{Name: "Test", Surname: "Client", UserID: user.ID, WorkspaceID: &ws.ID}
	must(&client)
	order := models.Order{ClientID: client.ID, Date: time.Now(), Status: "new", UserID: user.ID, WorkspaceID: &ws.ID}
	must(&order)
	item := models.OrderItem{OrderID: order.ID, ProductID: product.ID, Quantity: 1, Price: 5, Cost_price: 0.57, CostSnapshot: services.NewCostSnapshot("recipe", 0.57, estimate)}
	must(&item)
	var restored models.OrderItem
	if err := db.First(&restored, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.CostSnapshot == nil || restored.CostSnapshot.Version != 1 || restored.CostSnapshot.Amount != 0.57 {
		t.Fatal("jsonb snapshot did not round-trip")
	}
	if err := db.Model(&models.Package{}).Where("id = ?", pkg.ID).Update("unit_cost", "-1").Error; err == nil {
		t.Fatal("negative package cost accepted")
	}
}
