package services

import (
	"encoding/json"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"math"
	"mobile-backend-go/models"
	"testing"
	"time"
)

func decimalForTest(s string) *models.Decimal { d := models.Decimal(s); return &d }

type costingFixture struct {
	db               *gorm.DB
	workspace        models.Workspace
	recipeA, recipeB models.Recipe
	ingredientA      models.Ingredient
	pkg              models.Package
}

func newCostingFixture(t *testing.T) costingFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}, &models.Workspace{}, &models.Ingredient{}, &models.Recipe{}, &models.RecipeIngredient{}, &models.Price{}, &models.Package{}, &models.Product{}, &models.ProductCostComponent{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "costing-user", Password: "hashed"}
	db.Create(&user)
	ws := models.Workspace{Name: "Costing", Slug: "costing"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	a := models.Ingredient{Name: "Ingredient A", Type: "meat"}
	b := models.Ingredient{Name: "Ingredient B", Type: "meat"}
	db.Create(&a)
	db.Create(&b)
	ra := models.Recipe{Name: "A", UserID: user.ID, WorkspaceID: &ws.ID, PlannedYieldG: decimalForTest("300")}
	rb := models.Recipe{Name: "B", UserID: user.ID, WorkspaceID: &ws.ID, PlannedYieldG: decimalForTest("300")}
	db.Create(&ra)
	db.Create(&rb)
	for _, row := range []models.RecipeIngredient{{RecipeID: ra.ID, IngredientID: a.ID, Quantity: "1", Unit: "kg"}, {RecipeID: rb.ID, IngredientID: b.ID, Quantity: "1", Unit: "kg"}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []models.Price{{IngredientID: a.ID, Price: 900, Quantity: 1, Unit: "kg", WorkspaceID: &ws.ID, Date: time.Now()}, {IngredientID: b.ID, Price: 600, Quantity: 1, Unit: "kg", WorkspaceID: &ws.ID, Date: time.Now()}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	pkg := models.Package{Name: "100 g", UserID: user.ID, WorkspaceID: &ws.ID, NetWeightG: decimalForTest("100"), UnitCost: decimalForTest("10")}
	if err := db.Create(&pkg).Error; err != nil {
		t.Fatal(err)
	}
	return costingFixture{db, ws, ra, rb, a, pkg}
}
func estimateForTest(t *testing.T, f costingFixture, p models.Product) *models.CostEstimate {
	t.Helper()
	rows := []models.Product{p}
	if err := NewCosting(f.db, f.workspace.ID).AttachProducts(rows); err != nil {
		t.Fatal(err)
	}
	return rows[0].CostEstimate
}
func assertCost(t *testing.T, e *models.CostEstimate, want float64) {
	t.Helper()
	if e.Status != "complete" || e.TotalCost == nil || math.Abs(*e.TotalCost-want) > 0.0000001 {
		t.Fatalf("estimate=%+v, want %.2f", e, want)
	}
}
func TestCostingOrdinaryAssortmentAndManual(t *testing.T) {
	f := newCostingFixture(t)
	assertCost(t, estimateForTest(t, f, models.Product{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID}), 310)
	p := models.Product{CostMode: "assortment", PackageID: f.pkg.ID, CostComponents: []models.ProductCostComponent{{RecipeID: f.recipeA.ID, QuantityG: "50"}, {RecipeID: f.recipeB.ID, QuantityG: "50"}}}
	assertCost(t, estimateForTest(t, f, p), 260)
	assertCost(t, estimateForTest(t, f, models.Product{CostMode: "manual", Cost: 0, PackageID: f.pkg.ID}), 0)
	assertCost(t, estimateForTest(t, f, models.Product{CostMode: "manual", Cost: 77, PackageID: f.pkg.ID}), 77)
}
func TestCostingDecimalDivisionTieAndZeroPackage(t *testing.T) {
	f := newCostingFixture(t)
	f.db.Model(&models.Price{}).Where("ingredient_id = ?", f.ingredientA.ID).Update("price", 1.13)
	f.db.Model(&models.Recipe{}).Where("id = ?", f.recipeA.ID).Update("planned_yield_g", "2")
	f.db.Model(&models.Package{}).Where("id = ?", f.pkg.ID).Updates(map[string]any{"net_weight_g": "1", "unit_cost": "0"})
	assertCost(t, estimateForTest(t, f, models.Product{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID}), 0.57)
}
func TestCostingIncompleteAndPackagingGuard(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(costingFixture)
	}{
		{"missing_yield", "missing_yield", func(f costingFixture) {
			f.db.Model(&models.Recipe{}).Where("id = ?", f.recipeA.ID).Update("planned_yield_g", nil)
		}},
		{"missing_price", "missing_price", func(f costingFixture) { f.db.Where("ingredient_id = ?", f.ingredientA.ID).Delete(&models.Price{}) }},
		{"missing_package_cost", "missing_package_cost", func(f costingFixture) {
			f.db.Model(&models.Package{}).Where("id = ?", f.pkg.ID).Update("unit_cost", nil)
		}},
		{"packaging", "packaging_in_recipe", func(f costingFixture) {
			f.db.Model(&models.Ingredient{}).Where("id = ?", f.ingredientA.ID).Update("type", " Packaging ")
			f.db.Model(&models.Price{}).Where("ingredient_id = ?", f.ingredientA.ID).Update("price", 0)
		}},
		{"units", "incompatible_units", func(f costingFixture) {
			f.db.Model(&models.Price{}).Where("ingredient_id = ?", f.ingredientA.ID).Update("unit", "ml")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCostingFixture(t)
			tc.change(f)
			e := estimateForTest(t, f, models.Product{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID})
			if e.TotalCost != nil || e.Status != "incomplete" {
				t.Fatalf("unexpected estimate %+v", e)
			}
			found := false
			for _, i := range e.Issues {
				found = found || i.Code == tc.code
			}
			if !found {
				t.Fatalf("missing %s: %+v", tc.code, e.Issues)
			}
		})
	}
}
func TestCostingUsesLatestWorkspacePrice(t *testing.T) {
	f := newCostingFixture(t)
	other := models.Workspace{Name: "Other", Slug: "other"}
	f.db.Create(&other)
	future := time.Now().Add(time.Hour)
	f.db.Create(&models.Price{IngredientID: f.ingredientA.ID, Price: 999999, Quantity: 1, Unit: "kg", WorkspaceID: &other.ID, Date: future})
	assertCost(t, estimateForTest(t, f, models.Product{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID}), 310)
	f.db.Create(&models.Price{IngredientID: f.ingredientA.ID, Price: 1200, Quantity: 1, Unit: "kg", WorkspaceID: &f.workspace.ID, Date: future})
	assertCost(t, estimateForTest(t, f, models.Product{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID}), 410)
}
func TestCostingDoesNotSwallowDatabaseErrors(t *testing.T) {
	f := newCostingFixture(t)
	f.db.Migrator().DropTable(&models.Price{})
	rows := []models.Product{{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID}}
	if err := NewCosting(f.db, f.workspace.ID).AttachProducts(rows); err == nil {
		t.Fatal("expected database error")
	}
}
func TestCostDecimalPrecisionAndRounding(t *testing.T) {
	for _, tc := range []struct {
		input string
		cents int64
	}{{"0.565", 57}, {"0.564999", 56}, {"0.565001", 57}, {"-0.565", -57}, {"0", 0}, {"0.333333333333333", 33}} {
		r, err := ParseCostDecimal(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		got, err := CostCents(r)
		if err != nil || got != tc.cents {
			t.Fatalf("%s -> %d (%v)", tc.input, got, err)
		}
	}
	for _, bad := range []string{"NaN", "Inf", "1/2", "1e999999999", ""} {
		if _, err := ParseCostDecimal(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if ValidateCostDecimal(decimalForTest("0.0001"), 3, false) == nil || ValidateCostDecimal(decimalForTest("0"), 3, false) == nil {
		t.Fatal("invalid weight accepted")
	}
	if !CostAssertionMatches(0.57, 0.57) || CostAssertionMatches(0.565, 0.57) || CostAssertionMatches(310, 320) {
		t.Fatal("incorrect assertion comparison")
	}
}

func TestCostingExtremeLegacyNumbersStayIncomplete(t *testing.T) {
	f := newCostingFixture(t)
	if err := f.db.Model(&models.Price{}).Where("ingredient_id = ?", f.ingredientA.ID).Updates(map[string]any{"price": 1e308, "quantity": 1e-308}).Error; err != nil {
		t.Fatal(err)
	}
	e := estimateForTest(t, f, models.Product{CostMode: "recipe", CostRecipeID: &f.recipeA.ID, PackageID: f.pkg.ID})
	if e.Status != "incomplete" || e.TotalCost != nil {
		t.Fatalf("out-of-range legacy input produced a valid cost: %+v", e)
	}
	if _, err := json.Marshal(e); err != nil {
		t.Fatalf("incomplete estimate cannot be encoded: %v", err)
	}
}

func TestCostingFiveFlavoursUsesOneCommonPackage(t *testing.T) {
	f := newCostingFixture(t)
	if err := f.db.Model(&models.Package{}).Where("id = ?", f.pkg.ID).Updates(map[string]any{"net_weight_g": "250", "unit_cost": "20"}).Error; err != nil {
		t.Fatal(err)
	}
	p := models.Product{CostMode: "assortment", PackageID: f.pkg.ID}
	for _, name := range []string{"One", "Two", "Three", "Four", "Five"} {
		recipe := models.Recipe{Name: name, UserID: 1, WorkspaceID: &f.workspace.ID, PlannedYieldG: decimalForTest("300")}
		if err := f.db.Create(&recipe).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.db.Create(&models.RecipeIngredient{RecipeID: recipe.ID, IngredientID: f.ingredientA.ID, Quantity: "1", Unit: "kg"}).Error; err != nil {
			t.Fatal(err)
		}
		p.CostComponents = append(p.CostComponents, models.ProductCostComponent{RecipeID: recipe.ID, QuantityG: "50"})
	}
	e := estimateForTest(t, f, p)
	assertCost(t, e, 770)
	if e.PackagingCost == nil || *e.PackagingCost != 20 || e.ContentCost == nil || *e.ContentCost != 750 || len(e.Components) != 5 {
		t.Fatalf("packaging counted more than once: %+v", e)
	}
}
