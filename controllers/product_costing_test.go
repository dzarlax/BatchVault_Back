package controllers

import (
	"encoding/json"
	"errors"
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

func TestProductAssortmentCreateSwitchAndLegacyProtection(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	other := models.Product{Name: "Other", Price: 10, Cost: 999, CostMode: "manual", ProductType: "standard", CostRecipeID: &f.PersonalRecipe.ID, PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "Assortment", "price": 100, "package_id": f.PersonalPackage.ID, "product_type": "assortment", "composition_basis": "products", "cost_mode": "assortment", "assortment_components": []map[string]any{{"component_product_id": f.PersonalProduct.ID, "quantity_g": 50, "position": 0}, {"component_product_id": other.ID, "quantity_g": 50, "position": 1}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, CreateProduct, "POST", "/products", "/products", body)
	if r.Code != 201 {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	var p models.Product
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.CostEstimate == nil || p.CostEstimate.TotalCost == nil || *p.CostEstimate.TotalCost != 310 || len(p.CostEstimate.CompositionDetails) != 2 {
		t.Fatalf("unexpected product: %+v", p)
	}
	body["configuration_version"] = p.ConfigurationVersion
	body["cost_mode"] = "manual"
	body["cost"] = 290
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", fmt.Sprintf("/products/%d", p.ID), body)
	if r.Code != 200 {
		t.Fatalf("manual switch: %d %s", r.Code, r.Body)
	}
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.CostEstimate == nil || *p.CostEstimate.TotalCost != 290 || len(p.AssortmentComponents) != 2 {
		t.Fatalf("lost composition: %+v", p)
	}
	legacy := map[string]any{"name": "Assortment", "price": 100, "package_id": f.PersonalPackage.ID, "cost_mode": "manual", "components": nil}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", fmt.Sprintf("/products/%d", p.ID), legacy)
	if r.Code != 409 {
		t.Fatalf("legacy erase: %d %s", r.Code, r.Body)
	}
}

func TestLegacyModeChangeAndStaleVersionAreRejected(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	target := fmt.Sprintf("/products/%d", f.PersonalProduct.ID)
	body := map[string]any{"name": f.PersonalProduct.Name, "price": f.PersonalProduct.Price, "package_id": f.PersonalPackage.ID, "cost_mode": "manual"}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 409 {
		t.Fatalf("legacy transition: %d %s", r.Code, r.Body)
	}
	var p models.Product
	if err := database.DB.First(&p, f.PersonalProduct.ID).Error; err != nil {
		t.Fatal(err)
	}
	if p.CostRecipeID == nil || *p.CostRecipeID != f.PersonalRecipe.ID || p.CostMode != "recipe" {
		t.Fatalf("legacy request changed config: %+v", p)
	}
	body["product_type"] = "standard"
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 400 {
		t.Fatalf("missing version: %d %s", r.Code, r.Body)
	}
	body["recipe_id"] = f.PersonalRecipe.ID
	body["configuration_version"] = p.ConfigurationVersion + 1
	body["cost"] = 42
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 409 {
		t.Fatalf("stale version: %d %s", r.Code, r.Body)
	}
	body["configuration_version"] = p.ConfigurationVersion
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 200 {
		t.Fatalf("current transition: %d %s", r.Code, r.Body)
	}
	if err := database.DB.First(&p, f.PersonalProduct.ID).Error; err != nil {
		t.Fatal(err)
	}
	if p.CostMode != "manual" || p.CostRecipeID == nil || *p.CostRecipeID != f.PersonalRecipe.ID {
		t.Fatalf("new request lost recipe: %+v", p)
	}
}

func TestCommittedProductReadFailureDoesNotInviteResave(t *testing.T) {
	f := setupCostingBusiness(t)
	previous := readSavedCostedProduct
	readSavedCostedProduct = func(uint, uint) (models.Product, error) { return models.Product{}, errors.New("injected read failure") }
	defer func() { readSavedCostedProduct = previous }()
	body := map[string]any{"name": "Committed", "price": 100, "package_id": f.PersonalPackage.ID, "product_type": "standard", "cost_mode": "manual", "cost": 0}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, CreateProduct, "POST", "/products", "/products", body)
	if r.Code != 201 {
		t.Fatalf("response: %d %s", r.Code, r.Body)
	}
	var result map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["refresh_required"] != true || result["cost_estimate"] != nil || result["id"] == nil {
		t.Fatalf("response: %+v", result)
	}
	var count int64
	if err := database.DB.Model(&models.Product{}).Where("name = ?", "Committed").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("commit: %d %v", count, err)
	}
}

func TestProductTypesFeatureGateBlocksWritesButNotReads(t *testing.T) {
	f := setupCostingBusiness(t)
	t.Setenv("PRODUCT_TYPES_V1_ENABLED", "false")
	body := map[string]any{"name": "Disabled", "price": 100, "package_id": f.PersonalPackage.ID, "product_type": "standard", "cost_mode": "manual", "cost": 1}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, CreateProduct, "POST", "/products", "/products", body)
	if r.Code != 409 {
		t.Fatalf("write: %d %s", r.Code, r.Body)
	}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, GetProducts, "GET", "/products", "/products", nil)
	if r.Code != 200 {
		t.Fatalf("read: %d %s", r.Code, r.Body)
	}
	if workspaceCapabilities().ProductTypesV1 {
		t.Fatal("disabled capability reported enabled")
	}
}

func TestLegacySerializerNoopAndEraseProtection(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	source := models.Product{Name: "second source", Price: 10, CostMode: "manual", ProductType: "standard", CostRecipeID: &f.PersonalRecipe.ID, PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "mix", "price": 100, "package_id": f.PersonalPackage.ID, "product_type": "assortment", "composition_basis": "products", "cost_mode": "assortment", "assortment_components": []any{map[string]any{"component_product_id": f.PersonalProduct.ID, "quantity_g": 50}, map[string]any{"component_product_id": source.ID, "quantity_g": 50}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, CreateProduct, "POST", "/products", "/products", body)
	if r.Code != 201 {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	var p models.Product
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("/products/%d", p.ID)
	old := map[string]any{"name": "renamed", "price": 100, "package_id": f.PersonalPackage.ID, "cost_mode": "assortment", "cost": 0, "cost_recipe_id": nil, "components": []any{}, "recipe_ids": []any{}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, old)
	if r.Code != 200 {
		t.Fatalf("old serializer no-op: %d %s", r.Code, r.Body)
	}
	var rows int64
	if err := database.DB.Model(&models.ProductAssortmentComponent{}).Where("product_id = ?", p.ID).Count(&rows).Error; err != nil || rows != 2 {
		t.Fatalf("composition lost: %d %v", rows, err)
	}
	if err := database.DB.Create(&models.ProductOption{ProductID: p.ID, RecipeID: f.PersonalRecipe.ID, UserID: f.User.ID}).Error; err != nil {
		t.Fatal(err)
	}
	old["recipe_ids"] = []uint{f.PersonalRecipe.ID}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, old)
	if r.Code != 200 {
		t.Fatalf("same options: %d %s", r.Code, r.Body)
	}
	old["recipe_ids"] = []any{}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, old)
	if r.Code != 409 {
		t.Fatalf("option erase: %d %s", r.Code, r.Body)
	}
	standard := models.Product{Name: "manual retained", Price: 10, CostMode: "manual", ProductType: "standard", CostRecipeID: &f.PersonalRecipe.ID, PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&standard).Error; err != nil {
		t.Fatal(err)
	}
	erase := map[string]any{"name": "manual retained", "price": 10, "package_id": f.PersonalPackage.ID, "cost_mode": "manual", "cost_recipe_id": nil}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", fmt.Sprintf("/products/%d", standard.ID), erase)
	if r.Code != 409 {
		t.Fatalf("retained recipe erase: %d %s", r.Code, r.Body)
	}
}

func TestReferencedSourceCannotBecomeAssortmentViaLegacyOrCurrentClient(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	secondRecipe := models.Recipe{Name: "Second local recipe", UserID: f.User.ID, WorkspaceID: &f.PersonalWorkspace.ID}
	if err := database.DB.Create(&secondRecipe).Error; err != nil {
		t.Fatal(err)
	}
	secondSource := models.Product{Name: "Second source", Price: 10, ProductType: "standard", CostMode: "manual", CostRecipeID: &f.PersonalRecipe.ID, PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&secondSource).Error; err != nil {
		t.Fatal(err)
	}
	basis := "products"
	parent := models.Product{Name: "Parent", Price: 100, ProductType: "assortment", CompositionBasis: &basis, CostMode: "assortment", PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	for i, id := range []uint{f.PersonalProduct.ID, secondSource.ID} {
		if err := database.DB.Create(&models.ProductAssortmentComponent{WorkspaceID: f.PersonalWorkspace.ID, ProductID: parent.ID, ComponentProductID: id, QuantityG: "50", Position: i}).Error; err != nil {
			t.Fatal(err)
		}
	}
	getParent := func() models.Product {
		t.Helper()
		r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, GetProductByID, "GET", "/products/:id", fmt.Sprintf("/products/%d", parent.ID), nil)
		if r.Code != 200 {
			t.Fatalf("parent read: %d %s", r.Code, r.Body)
		}
		var p models.Product
		if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	before := getParent()
	if before.CostEstimate == nil || before.CostEstimate.TotalCost == nil {
		t.Fatal("expected complete parent estimate")
	}
	amount := *before.CostEstimate.TotalCost
	var sourceBefore models.Product
	if err := database.DB.First(&sourceBefore, f.PersonalProduct.ID).Error; err != nil {
		t.Fatal(err)
	}
	target := fmt.Sprintf("/products/%d", sourceBefore.ID)
	legacy := map[string]any{"name": sourceBefore.Name, "price": sourceBefore.Price, "package_id": f.PersonalPackage.ID, "cost_mode": "assortment", "components": []any{map[string]any{"recipe_id": f.PersonalRecipe.ID, "quantity_g": 50}, map[string]any{"recipe_id": secondRecipe.ID, "quantity_g": 50}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, legacy)
	if r.Code != 409 {
		t.Fatalf("legacy transition: %d %s", r.Code, r.Body)
	}
	modern := map[string]any{"name": sourceBefore.Name, "price": sourceBefore.Price, "package_id": f.PersonalPackage.ID, "product_type": "assortment", "composition_basis": "products", "configuration_version": sourceBefore.ConfigurationVersion, "cost_mode": "assortment", "assortment_components": []any{map[string]any{"component_product_id": sourceBefore.ID, "quantity_g": 50}, map[string]any{"component_product_id": secondSource.ID, "quantity_g": 50}}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, modern)
	if r.Code != 409 {
		t.Fatalf("current transition: %d %s", r.Code, r.Body)
	}
	var after models.Product
	if err := database.DB.First(&after, sourceBefore.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.ProductType != sourceBefore.ProductType || after.CostMode != sourceBefore.CostMode || after.ConfigurationVersion != sourceBefore.ConfigurationVersion {
		t.Fatalf("source changed: before=%+v after=%+v", sourceBefore, after)
	}
	final := getParent()
	if final.CostEstimate == nil || final.CostEstimate.TotalCost == nil || *final.CostEstimate.TotalCost != amount {
		t.Fatalf("parent estimate changed: %+v", final.CostEstimate)
	}
}

func TestLegacyCompositionRequiresProductConversion(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	recipeB := models.Recipe{Name: "Second local", UserID: f.User.ID, WorkspaceID: &f.PersonalWorkspace.ID}
	if err := database.DB.Create(&recipeB).Error; err != nil {
		t.Fatal(err)
	}
	sourceB := models.Product{Name: "Second source", Price: 10, ProductType: "standard", CostMode: "manual", CostRecipeID: &recipeB.ID, PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&sourceB).Error; err != nil {
		t.Fatal(err)
	}
	basis := "legacy_recipes"
	parent := models.Product{Name: "Legacy", Price: 100, ProductType: "assortment", CompositionBasis: &basis, CostMode: "assortment", PackageID: f.PersonalPackage.ID, WorkspaceID: &f.PersonalWorkspace.ID, UserID: f.User.ID}
	if err := database.DB.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	for i, id := range []uint{f.PersonalRecipe.ID, recipeB.ID} {
		if err := database.DB.Create(&models.ProductCostComponent{WorkspaceID: f.PersonalWorkspace.ID, ProductID: parent.ID, RecipeID: id, QuantityG: "50", Position: i}).Error; err != nil {
			t.Fatal(err)
		}
	}
	target := fmt.Sprintf("/products/%d", parent.ID)
	body := map[string]any{"name": "Legacy", "price": 100, "package_id": f.PersonalPackage.ID, "product_type": "assortment", "composition_basis": "legacy_recipes", "configuration_version": parent.ConfigurationVersion, "cost_mode": "assortment", "components": []any{map[string]any{"recipe_id": f.PersonalRecipe.ID, "quantity_g": 60}, map[string]any{"recipe_id": recipeB.ID, "quantity_g": 40}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 409 {
		t.Fatalf("changed composition: %d %s", r.Code, r.Body)
	}
	var errBody map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody["code"] != "legacy_composition_requires_conversion" {
		t.Fatalf("unexpected conflict: %s", r.Body)
	}
	body["components"] = nil
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 409 {
		t.Fatalf("null erase: %d %s", r.Code, r.Body)
	}
	body["components"] = []any{map[string]any{"recipe_id": f.PersonalRecipe.ID, "quantity_g": 50.000}, map[string]any{"recipe_id": recipeB.ID, "quantity_g": 50}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 200 {
		t.Fatalf("identical composition: %d %s", r.Code, r.Body)
	}
	var updated models.Product
	if err := json.Unmarshal(r.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	body["configuration_version"] = updated.ConfigurationVersion
	body["composition_basis"] = "products"
	body["components"] = []any{}
	body["assortment_components"] = []any{map[string]any{"component_product_id": f.PersonalProduct.ID, "quantity_g": 50}, map[string]any{"component_product_id": sourceB.ID, "quantity_g": 50}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateProduct, "PUT", "/products/:id", target, body)
	if r.Code != 200 {
		t.Fatalf("explicit conversion: %d %s", r.Code, r.Body)
	}
	var converted models.Product
	if err := json.Unmarshal(r.Body.Bytes(), &converted); err != nil {
		t.Fatal(err)
	}
	if converted.CompositionBasis == nil || *converted.CompositionBasis != "products" || len(converted.AssortmentComponents) != 2 || len(converted.CostComponents) != 0 {
		t.Fatalf("conversion result: %+v", converted)
	}
	var count int64
	if err := database.DB.Model(&models.ProductCostComponent{}).Where("product_id = ?", parent.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("legacy rows not retired: %d %v", count, err)
	}
}
