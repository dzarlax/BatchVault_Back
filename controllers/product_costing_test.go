package controllers

import (
	"encoding/json"
	"fmt"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"
	"testing"
	"time"
)

func setupCostingBusiness(t *testing.T) workspaceBusinessFixture {
	t.Helper()
	f := setupWorkspaceBusinessTest(t)
	for _, op := range []func() error{
		func() error {
			return database.DB.Model(&models.Recipe{}).Where("id = ?", f.PersonalRecipe.ID).Update("planned_yield_g", "300").Error
		},
		func() error {
			return database.DB.Model(&models.Package{}).Where("id = ?", f.PersonalPackage.ID).Updates(map[string]any{"net_weight_g": "100", "unit_cost": "10"}).Error
		},
		func() error {
			return database.DB.Create(&models.RecipeIngredient{RecipeID: f.PersonalRecipe.ID, IngredientID: f.Ingredient.ID, Quantity: "1", Unit: "kg"}).Error
		},
		func() error {
			return database.DB.Create(&models.Price{IngredientID: f.Ingredient.ID, Price: 900, Quantity: 1, Unit: "kg", WorkspaceID: &f.PersonalWorkspace.ID, Date: time.Now()}).Error
		},
	} {
		if err := op(); err != nil {
			t.Fatal(err)
		}
	}
	return f
}
func enableCalculatedProduct(t *testing.T, f workspaceBusinessFixture) {
	t.Helper()
	if err := database.DB.Model(&models.Product{}).Where("id = ?", f.PersonalProduct.ID).Updates(map[string]any{"cost_mode": "recipe", "cost_recipe_id": f.PersonalRecipe.ID}).Error; err != nil {
		t.Fatal(err)
	}
}
func TestCostPreviewAndProductSave(t *testing.T) {
	f := setupCostingBusiness(t)
	body := map[string]any{"cost_mode": "recipe", "cost_recipe_id": f.PersonalRecipe.ID, "package_id": f.PersonalPackage.ID}
	response := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, PreviewProductCost, "POST", "/products/cost-preview", "/products/cost-preview", body)
	if response.Code != 200 {
		t.Fatalf("preview %d %s", response.Code, response.Body)
	}
	var e models.CostEstimate
	if err := json.Unmarshal(response.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.TotalCost == nil || *e.TotalCost != 310 {
		t.Fatalf("estimate %+v", e)
	}
	body["name"], body["price"] = "Calculated", 650
	response = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, CreateProduct, "POST", "/products", "/products", body)
	if response.Code != 201 {
		t.Fatalf("create %d %s", response.Code, response.Body)
	}
	var p models.Product
	json.Unmarshal(response.Body.Bytes(), &p)
	if p.CostEstimate == nil || *p.CostEstimate.TotalCost != 310 || p.Cost != 0 {
		t.Fatalf("product %+v", p)
	}
	body["cost"] = 4
	response = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", fmt.Sprintf("/products/%d", p.ID), body)
	if response.Code != 409 {
		t.Fatalf("expected read-only conflict, got %d %s", response.Code, response.Body)
	}
	body["cost_recipe_id"] = f.SecondRecipe.ID
	delete(body, "cost")
	response = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, PreviewProductCost, "POST", "/products/cost-preview", "/products/cost-preview", body)
	if response.Code != 400 {
		t.Fatalf("expected workspace rejection, got %d %s", response.Code, response.Body)
	}
}
func TestPackageInitializationAndEstablishedWeightGuard(t *testing.T) {
	f := setupWorkspaceBusinessTest(t)
	target := fmt.Sprintf("/packages/%d", f.PersonalPackage.ID)
	for _, tc := range []struct {
		body map[string]any
		want int
	}{
		{map[string]any{"net_weight_g": 100, "unit_cost": 0}, 409},
		{map[string]any{"net_weight_g": 100, "unit_cost": 0, "confirm_weight_initialization": true}, 200},
		{map[string]any{"net_weight_g": 100}, 200},
		{map[string]any{"net_weight_g": 200}, 409},
		{map[string]any{"net_weight_g": nil}, 409},
		{map[string]any{"unit_cost": -1}, 400},
	} {
		r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdatePackage, "PATCH", "/packages/:id", target, tc.body)
		if r.Code != tc.want {
			t.Fatalf("%v: %d %s", tc.body, r.Code, r.Body)
		}
	}
	var p models.Product
	database.DB.First(&p, f.PersonalProduct.ID)
	if p.CostMode != "manual" || p.Cost != 5 {
		t.Fatal("initialization changed manual costing")
	}
}
func TestRecipeYieldPatchPrecisionAndNull(t *testing.T) {
	f := setupWorkspaceBusinessTest(t)
	target := fmt.Sprintf("/recipes/%d", f.PersonalRecipe.ID)
	for _, tc := range []struct {
		value any
		want  int
	}{{0, 400}, {-1, 400}, {0.0001, 400}, {300.125, 200}, {nil, 200}} {
		r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateRecipeYield, "PATCH", "/recipes/:id", target, map[string]any{"planned_yield_g": tc.value})
		if r.Code != tc.want {
			t.Fatalf("%v: %d %s", tc.value, r.Code, r.Body)
		}
	}
}
func TestExplicitZeroIngredientPriceCreation(t *testing.T) {
	f := setupWorkspacePriceTest(t)
	body := map[string]any{"ingredient_id": f.Ingredient.ID, "price": 0, "quantity": 1, "unit": "kg"}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, AddPrice, http.MethodPost, "/prices", "/prices", body)
	if r.Code != 201 {
		t.Fatalf("zero price: %d %s", r.Code, r.Body)
	}
	for _, v := range []any{nil, -1} {
		body["price"] = v
		r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, AddPrice, "POST", "/prices", "/prices", body)
		if r.Code != 400 {
			t.Fatalf("invalid price %v accepted", v)
		}
	}
	delete(body, "price")
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, AddPrice, "POST", "/prices", "/prices", body)
	if r.Code != 400 {
		t.Fatal("missing price accepted")
	}
}
func TestAssortmentSaveAndMismatchRollback(t *testing.T) {
	f := setupCostingBusiness(t)
	second := models.Recipe{Name: "Second flavour", UserID: f.User.ID, WorkspaceID: &f.PersonalWorkspace.ID}
	database.DB.Create(&second)
	body := map[string]any{"name": "Mix", "price": 650, "package_id": f.PersonalPackage.ID, "cost_mode": "assortment", "components": []map[string]any{{"recipe_id": f.PersonalRecipe.ID, "quantity_g": 50}, {"recipe_id": second.ID, "quantity_g": 50}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, CreateProduct, "POST", "/products", "/products", body)
	if r.Code != 201 {
		t.Fatalf("assortment create %d %s", r.Code, r.Body)
	}
	var p models.Product
	json.Unmarshal(r.Body.Bytes(), &p)
	if len(p.CostComponents) != 2 || p.CostEstimate.TotalCost != nil {
		t.Fatal("incomplete assortment response incorrect")
	}
	body["components"] = []map[string]any{{"recipe_id": f.PersonalRecipe.ID, "quantity_g": 60}, {"recipe_id": second.ID, "quantity_g": 50}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", fmt.Sprintf("/products/%d", p.ID), body)
	if r.Code != 400 {
		t.Fatalf("mismatch accepted: %d %s", r.Code, r.Body)
	}
	var rows []models.ProductCostComponent
	database.DB.Where("product_id = ?", p.ID).Find(&rows)
	if len(rows) != 2 || rows[0].QuantityG != "50" {
		t.Fatal("invalid update changed saved composition")
	}
}

func TestPackageInitializationRejectsExistingAssortmentMismatch(t *testing.T) {
	f := setupCostingBusiness(t)
	database.DB.Model(&models.Package{}).Where("id = ?", f.PersonalPackage.ID).Update("net_weight_g", nil)
	second := models.Recipe{Name: "Second", UserID: f.User.ID, WorkspaceID: &f.PersonalWorkspace.ID}
	database.DB.Create(&second)
	database.DB.Model(&models.Product{}).Where("id = ?", f.PersonalProduct.ID).Update("cost_mode", "assortment")
	for _, id := range []uint{f.PersonalRecipe.ID, second.ID} {
		if err := database.DB.Create(&models.ProductCostComponent{WorkspaceID: f.PersonalWorkspace.ID, ProductID: f.PersonalProduct.ID, RecipeID: id, QuantityG: "50"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdatePackage, "PATCH", "/packages/:id", fmt.Sprintf("/packages/%d", f.PersonalPackage.ID), map[string]any{"net_weight_g": 250, "confirm_weight_initialization": true})
	if r.Code != 409 {
		t.Fatalf("mismatched initialization accepted: %d %s", r.Code, r.Body)
	}
	var pkg models.Package
	database.DB.First(&pkg, f.PersonalPackage.ID)
	if pkg.NetWeightG != nil {
		t.Fatal("failed initialization changed package")
	}
}

func TestCostPreviewDatabaseFailureIsServerError(t *testing.T) {
	f := setupCostingBusiness(t)
	if err := database.DB.Migrator().DropTable(&models.Price{}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"cost_mode": "recipe", "cost_recipe_id": f.PersonalRecipe.ID, "package_id": f.PersonalPackage.ID}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, PreviewProductCost, "POST", "/products/cost-preview", "/products/cost-preview", body)
	if r.Code != 500 {
		t.Fatalf("database error became a partial or input result: %d %s", r.Code, r.Body)
	}
}
