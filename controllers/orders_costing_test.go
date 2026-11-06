package controllers

import (
	"encoding/json"
	"fmt"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"testing"
)

func createCostedOrderForTest(t *testing.T, f workspaceBusinessFixture, cost any) models.Order {
	t.Helper()
	row := map[string]any{"product_id": f.PersonalProduct.ID, "quantity": 1, "price": 650}
	if cost != nil {
		row["cost_price"] = cost
	}
	body := map[string]any{"client_id": f.PersonalClient.ID, "items": []any{row}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, AddOrder, "POST", "/orders", "/orders", body)
	if r.Code != 201 {
		t.Fatalf("create order: %d %s", r.Code, r.Body)
	}
	var order models.Order
	if err := json.Unmarshal(r.Body.Bytes(), &order); err != nil {
		t.Fatal(err)
	}
	return order
}
func TestCalculatedOrderSnapshotAndStaleAssertion(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	order := createCostedOrderForTest(t, f, 310)
	if len(order.Items) != 1 || order.Items[0].Cost_price != 310 || order.Items[0].CostSnapshot == nil || order.Items[0].CostSnapshot.Source != "recipe" {
		t.Fatalf("missing snapshot: %+v", order.Items)
	}
	database.DB.Model(&models.Package{}).Where("id = ?", f.PersonalPackage.ID).Update("unit_cost", "20")
	var before, after int64
	database.DB.Model(&models.Order{}).Count(&before)
	body := map[string]any{"client_id": f.PersonalClient.ID, "items": []any{map[string]any{"product_id": f.PersonalProduct.ID, "quantity": 1, "price": 650, "cost_price": 310}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, AddOrder, "POST", "/orders", "/orders", body)
	database.DB.Model(&models.Order{}).Count(&after)
	if r.Code != 409 || before != after {
		t.Fatalf("stale cost wrote order: %d %s", r.Code, r.Body)
	}
	createCostedOrderForTest(t, f, 320)
	row := order.Items[0]
	body = map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{map[string]any{"id": row.ID, "product_id": row.ProductID, "quantity": 2, "price": 650, "cost_price": 310}}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", fmt.Sprintf("/orders/%d", order.ID), body)
	if r.Code != 200 {
		t.Fatalf("historical update: %d %s", r.Code, r.Body)
	}
	var saved models.OrderItem
	database.DB.First(&saved, row.ID)
	if saved.Cost_price != 310 || saved.Quantity != 2 || saved.CostSnapshot.Amount != 310 {
		t.Fatal("historical amount changed")
	}
}
func TestHistoricalProvenanceSurvivesProductModeChangesAndDeletion(t *testing.T) {
	f := setupCostingBusiness(t)
	legacy := f.PersonalOrder
	var old models.OrderItem
	database.DB.Where("order_id = ?", legacy.ID).First(&old)
	enableCalculatedProduct(t, f)
	body := map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{map[string]any{"id": old.ID, "product_id": old.ProductID, "quantity": 3, "price": 650}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", fmt.Sprintf("/orders/%d", legacy.ID), body)
	if r.Code != 200 {
		t.Fatalf("legacy->recipe: %d %s", r.Code, r.Body)
	}
	var saved models.OrderItem
	database.DB.First(&saved, old.ID)
	if saved.Cost_price != 5 || saved.CostSnapshot != nil {
		t.Fatal("legacy cost inferred from current recipe mode")
	}
	calculated := createCostedOrderForTest(t, f, 310)
	database.DB.Model(&models.Product{}).Where("id = ?", f.PersonalProduct.ID).Updates(map[string]any{"cost_mode": "manual", "cost": 999, "cost_recipe_id": nil})
	database.DB.Delete(&models.Product{}, f.PersonalProduct.ID)
	row := calculated.Items[0]
	body = map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{map[string]any{"id": row.ID, "product_id": row.ProductID, "quantity": 2, "price": 650}}}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", fmt.Sprintf("/orders/%d", calculated.ID), body)
	if r.Code != 200 {
		t.Fatalf("calculated->manual/deleted: %d %s", r.Code, r.Body)
	}
	var calculatedSaved models.OrderItem
	database.DB.First(&calculatedSaved, row.ID)
	if calculatedSaved.Cost_price != 310 || calculatedSaved.CostSnapshot == nil {
		t.Fatal("calculated snapshot was downgraded")
	}
}
func TestCalculatedHistoryCannotBeOverriddenAfterManualSwitch(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	order := createCostedOrderForTest(t, f, 310)
	database.DB.Model(&models.Product{}).Where("id = ?", f.PersonalProduct.ID).Updates(map[string]any{"cost_mode": "manual", "cost_recipe_id": nil})
	row := order.Items[0]
	body := map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{map[string]any{"id": row.ID, "product_id": row.ProductID, "quantity": 1, "price": 650, "cost_price": 0}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", fmt.Sprintf("/orders/%d", order.ID), body)
	if r.Code != 409 {
		t.Fatalf("historical override accepted: %d %s", r.Code, r.Body)
	}
}

func TestHistoricalDuplicateIDsAndAmbiguousLegacyRowsAreRejected(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	order := createCostedOrderForTest(t, f, 310)
	row := order.Items[0]
	item := map[string]any{"id": row.ID, "product_id": row.ProductID, "quantity": 1, "price": 650, "cost_price": 310}
	body := map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{item, item}}
	path := fmt.Sprintf("/orders/%d", order.ID)
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", path, body)
	if r.Code != 400 {
		t.Fatalf("duplicate IDs accepted: %d %s", r.Code, r.Body)
	}
	copy := row
	copy.ID = 0
	if err := database.DB.Omit("Product", "Order").Create(&copy).Error; err != nil {
		t.Fatal(err)
	}
	database.DB.Model(&models.Product{}).Where("id = ?", row.ProductID).Updates(map[string]any{"cost_mode": "manual", "cost_recipe_id": nil})
	delete(item, "id")
	body["items"] = []any{item}
	r = runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", path, body)
	if r.Code != 409 {
		t.Fatalf("ambiguous calculated history accepted: %d %s", r.Code, r.Body)
	}
	var count int64
	database.DB.Model(&models.OrderItem{}).Where("order_id = ?", order.ID).Count(&count)
	if count != 2 {
		t.Fatal("rejected update removed saved rows")
	}
}

func TestLegacyManualRowAcceptsExplicitZeroAfterProductOptIn(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	var row models.OrderItem
	database.DB.Where("order_id = ?", f.PersonalOrder.ID).First(&row)
	body := map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{map[string]any{"id": row.ID, "product_id": row.ProductID, "quantity": 1, "price": 650, "cost_price": 0}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", fmt.Sprintf("/orders/%d", f.PersonalOrder.ID), body)
	if r.Code != 200 {
		t.Fatalf("legacy manual edit rejected: %d %s", r.Code, r.Body)
	}
	var saved models.OrderItem
	database.DB.First(&saved, row.ID)
	if saved.Cost_price != 0 || saved.CostSnapshot == nil || saved.CostSnapshot.Source != "manual" {
		t.Fatalf("manual provenance lost: %+v", saved)
	}
}

func TestCalculatedSameRoundedAmountStoresFreshBreakdown(t *testing.T) {
	f := setupCostingBusiness(t)
	enableCalculatedProduct(t, f)
	if err := database.DB.Model(&models.Recipe{}).Where("id = ?", f.PersonalRecipe.ID).Update("planned_yield_g", "600").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Model(&models.Price{}).Where("ingredient_id = ? AND workspace_id = ?", f.Ingredient.ID, f.PersonalWorkspace.ID).Update("price", 1800).Error; err != nil {
		t.Fatal(err)
	}
	order := createCostedOrderForTest(t, f, 310)
	e := order.Items[0].CostSnapshot.Estimate
	if e == nil || len(e.Components) != 1 || e.Components[0].RecipeCost == nil || *e.Components[0].RecipeCost != 1800 || e.Components[0].ContentCost == nil || *e.Components[0].ContentCost != 300 {
		t.Fatalf("same-total assertion did not capture fresh inputs: %+v", e)
	}
}

func TestManualCostEditPreservesCapturedProvenance(t *testing.T) {
	f := setupCostingBusiness(t)
	order := createCostedOrderForTest(t, f, 42)
	row := order.Items[0]
	if row.CostSnapshot == nil || row.CostSnapshot.Provenance == nil {
		t.Fatalf("missing provenance: %+v", row.CostSnapshot)
	}
	name := row.CostSnapshot.Provenance.ProductName
	if err := database.DB.Model(&models.Product{}).Where("id = ?", row.ProductID).Update("name", "Changed").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.DB.Delete(&models.Product{}, row.ProductID).Error; err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"client_id": f.PersonalClient.ID, "status": "ready", "items": []any{map[string]any{"id": row.ID, "product_id": row.ProductID, "quantity": 2, "price": 650, "cost_price": 77}}}
	r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, UpdateOrder, "PUT", "/orders/:id", fmt.Sprintf("/orders/%d", order.ID), body)
	if r.Code != 200 {
		t.Fatalf("update: %d %s", r.Code, r.Body)
	}
	var saved models.OrderItem
	if err := database.DB.First(&saved, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.CostSnapshot == nil || saved.CostSnapshot.Amount != 77 || saved.CostSnapshot.Provenance == nil || saved.CostSnapshot.Provenance.ProductName != name {
		t.Fatalf("provenance changed: %+v", saved.CostSnapshot)
	}
}
