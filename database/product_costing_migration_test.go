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
	query.Set("default_query_exec_mode", "simple_protocol")
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); sqlDB.Close() })
	// Product costing itself may not have been installed on an older database.
	if err := db.Exec("CREATE TABLE products (id integer primary key, cost numeric)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO products (id, cost) VALUES (1, 5.123), (2, 0)").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.PrepareProductTypes(db); err != nil {
			t.Fatal(err)
		}
	}
	// GORM installs the missing costing column after product type preparation.
	if err := db.Migrator().AddColumn(&models.Product{}, "CostMode"); err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareProductTypes(db); err != nil {
		t.Fatal(err)
	}
	var oldRows []struct {
		ID               uint
		Cost             float64
		CostMode         string
		ProductType      string
		CompositionBasis *string
	}
	if err := db.Table("products").Order("id").Find(&oldRows).Error; err != nil {
		t.Fatal(err)
	}
	if len(oldRows) != 2 || oldRows[0].Cost != 5.123 || oldRows[1].Cost != 0 {
		t.Fatalf("old manual costs changed: %+v", oldRows)
	}
	for _, row := range oldRows {
		if row.CostMode != "manual" || row.ProductType != "standard" || row.CompositionBasis != nil {
			t.Fatalf("old manual product migration changed: %+v", row)
		}
	}
	if err := db.Exec("DROP TABLE products").Error; err != nil {
		t.Fatal(err)
	}
	// Exercise preparation against a pre-product-type schema before GORM defaults.
	if err := db.Exec("CREATE TABLE products (id integer primary key, cost_mode text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO products VALUES (1, 'manual'), (2, 'assortment')").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareProductTypes(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE products SET cost_mode = 'manual' WHERE id = 2").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.PrepareProductTypes(db); err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		ProductType      string
		CompositionBasis *string
	}
	if err := db.Table("products").Where("id = 2").Take(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.ProductType != "assortment" || legacy.CompositionBasis == nil || *legacy.CompositionBasis != "legacy_recipes" {
		t.Fatalf("legacy backfill: %+v", legacy)
	}
	if err := db.Exec("DROP TABLE products").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.Ingredient{}, &models.Price{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Package{}, &models.Product{}, &models.ProductCostComponent{}, &models.ProductAssortmentComponent{}, &models.Client{}, &models.Order{}, &models.OrderItem{}); err != nil {
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
	// Reconstruct the preceding schema by removing only the new columns and table
	// from the real model schema, while retaining representative old data.
	recipeB := models.Recipe{Name: "Second", UserID: user.ID, WorkspaceID: &ws.ID, PlannedYieldG: d("2")}
	must(&recipeB)
	legacyProduct := models.Product{Name: "Legacy assortment", Price: 12, Cost: 19.25, CostMode: "assortment", ProductType: "assortment", PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: user.ID}
	must(&legacyProduct)
	must(&models.ProductCostComponent{WorkspaceID: ws.ID, ProductID: legacyProduct.ID, RecipeID: recipe.ID, QuantityG: "0.500", Position: 0})
	must(&models.ProductCostComponent{WorkspaceID: ws.ID, ProductID: legacyProduct.ID, RecipeID: recipeB.ID, QuantityG: "0.500", Position: 1})
	var snapshotBefore string
	if err := db.Raw("SELECT cost_snapshot::text FROM order_items WHERE id = ?", item.ID).Scan(&snapshotBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE product_assortment_components").Error; err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"product_type", "composition_basis", "configuration_version"} {
		if err := db.Exec("ALTER TABLE products DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.PrepareProductTypes(db); err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.Ingredient{}, &models.Price{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Package{}, &models.Product{}, &models.ProductCostComponent{}, &models.ProductAssortmentComponent{}, &models.Client{}, &models.Order{}, &models.OrderItem{}); err != nil {
			t.Fatal(err)
		}
		if err := database.MigrateProductCosting(db); err != nil {
			t.Fatal(err)
		}
	}
	migrate()
	var migrated models.Product
	if err := db.Preload("CostComponents").First(&migrated, legacyProduct.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.ProductType != "assortment" || migrated.CompositionBasis == nil || *migrated.CompositionBasis != "legacy_recipes" || migrated.Cost != 19.25 || len(migrated.CostComponents) != 2 {
		t.Fatalf("legacy assortment changed: %+v", migrated)
	}
	var manualAfter models.Product
	if err := db.First(&manualAfter, manual.ID).Error; err != nil {
		t.Fatal(err)
	}
	if manualAfter.Cost != 5.123 || manualAfter.ProductType != "standard" {
		t.Fatalf("manual changed: %+v", manualAfter)
	}
	basis := "products"
	modern := models.Product{Name: "Manual assortment", Price: 10, Cost: 290, CostMode: "manual", ProductType: "assortment", CompositionBasis: &basis, PackageID: pkg.ID, WorkspaceID: &ws.ID, UserID: user.ID}
	must(&modern)
	must(&models.ProductAssortmentComponent{WorkspaceID: ws.ID, ProductID: modern.ID, ComponentProductID: manual.ID, QuantityG: "0.500"})
	must(&models.ProductAssortmentComponent{WorkspaceID: ws.ID, ProductID: modern.ID, ComponentProductID: product.ID, QuantityG: "0.500"})
	migrate()
	migrated = models.Product{}
	if err := db.Preload("AssortmentComponents").First(&migrated, modern.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migrated.ProductType != "assortment" || migrated.CostMode != "manual" || migrated.Cost != 290 || migrated.CompositionBasis == nil || *migrated.CompositionBasis != "products" || len(migrated.AssortmentComponents) != 2 {
		t.Fatalf("modern manual changed: %+v", migrated)
	}
	var snapshotAfter string
	if err := db.Raw("SELECT cost_snapshot::text FROM order_items WHERE id = ?", item.ID).Scan(&snapshotAfter).Error; err != nil {
		t.Fatal(err)
	}
	if snapshotBefore != snapshotAfter {
		t.Fatal("historical snapshot changed during migration")
	}
	var sourceFK int64
	if err := db.Raw(`SELECT count(*) FROM pg_constraint c JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey) WHERE c.contype='f' AND c.conrelid='product_assortment_components'::regclass AND c.confrelid='products'::regclass AND a.attname='component_product_id'`).Scan(&sourceFK).Error; err != nil {
		t.Fatal(err)
	}
	if sourceFK != 1 {
		t.Fatalf("expected source product foreign key, got %d", sourceFK)
	}
	if err := db.Model(&models.Package{}).Where("id = ?", pkg.ID).Update("unit_cost", "-1").Error; err == nil {
		t.Fatal("negative package cost accepted")
	}
}
