package database

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestPrepareProductTypesBackfillsBeforeDefaultsAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:product_types_migration?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.Exec("CREATE TABLE products (id integer primary key, cost_mode text)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO products (id, cost_mode) VALUES (1, 'manual'), (2, 'assortment')").Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareProductTypes(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE products SET cost_mode = 'manual' WHERE id = 2").Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareProductTypes(db); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID               int
		ProductType      string
		CompositionBasis *string
	}
	if err := db.Table("products").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ProductType != "standard" || rows[0].CompositionBasis != nil || rows[1].ProductType != "assortment" || rows[1].CompositionBasis == nil || *rows[1].CompositionBasis != "legacy_recipes" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestPrepareProductTypesBeforeCostModeExists(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:product_types_before_costing?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.Exec("CREATE TABLE products (id integer primary key, cost numeric)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO products (id, cost) VALUES (1, 5.123), (2, 0)").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := PrepareProductTypes(db); err != nil {
			t.Fatal(err)
		}
	}
	var rows []struct {
		ID               int
		Cost             float64
		ProductType      string
		CompositionBasis *string
	}
	if err := db.Table("products").Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Cost != 5.123 || rows[1].Cost != 0 {
		t.Fatalf("manual values changed: %+v", rows)
	}
	for _, row := range rows {
		if row.ProductType != "standard" || row.CompositionBasis != nil {
			t.Fatalf("old manual product was not preserved: %+v", row)
		}
	}
}
