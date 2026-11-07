package controllers

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"mobile-backend-go/database"
	"mobile-backend-go/models"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupCookingController(t *testing.T) workspaceBusinessFixture {
	t.Helper()
	t.Setenv("COOKING_ENABLED", "true")
	f := setupCostingBusiness(t)
	if e := database.DB.Model(&models.Order{}).Where("id = ?", f.PersonalOrder.ID).Update("status", "new").Error; e != nil {
		t.Fatal(e)
	}
	if e := database.DB.AutoMigrate(&models.WorkspaceIngredient{}); e != nil {
		t.Fatal(e)
	}
	if e := database.MigrateCooking(database.DB); e != nil {
		t.Fatal(e)
	}
	for _, op := range []func() error{
		func() error {
			return database.DB.Model(&models.Ingredient{}).Where("id = ?", f.Ingredient.ID).Update("type", "base").Error
		},
		func() error {
			return database.DB.Model(&models.Product{}).Where("id = ?", f.PersonalProduct.ID).Updates(map[string]any{"cost_recipe_id": f.PersonalRecipe.ID, "product_type": "standard"}).Error
		},
		func() error {
			return database.DB.Model(&models.OrderItem{}).Where("order_id = ?", f.PersonalOrder.ID).Update("quantity", 1).Error
		},
		func() error {
			return database.DB.Create(&models.WorkspaceIngredient{WorkspaceID: f.PersonalWorkspace.ID, IngredientID: f.Ingredient.ID, Active: true}).Error
		},
	} {
		if e := op(); e != nil {
			t.Fatal(e)
		}
	}
	return f
}
func TestCookingHTTPFlowAndBothOrderMutationPaths(t *testing.T) {
	f := setupCookingController(t)
	request := func(handler func(*gin.Context), method, pattern, path string, body any) *httptest.ResponseRecorder {
		return runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, handler, method, pattern, path, body)
	}
	planBody := map[string]any{"order_ids": []uint{f.PersonalOrder.ID}, "extras": []any{}}
	r := request(PreviewCookingPlan, "POST", "/cooking/plan-preview", "/cooking/plan-preview", planBody)
	if r.Code != 200 {
		t.Fatalf("plan: %d %s", r.Code, r.Body)
	}
	var preview models.CookingPlanPreview
	if e := json.Unmarshal(r.Body.Bytes(), &preview); e != nil {
		t.Fatal(e)
	}
	var lotCount int64
	database.DB.Model(&models.CookingStockLot{}).Count(&lotCount)
	if lotCount != 0 {
		t.Fatal("preview created stock")
	}
	planBody["source_signature"] = preview.SourceSignature
	planBody["stock_version"] = preview.StockVersion
	planBody["operation_id"] = uuid.NewString()
	planBody["date"] = "2026-11-07"
	r = request(CreateCookingRun, "POST", "/cooking-runs", "/cooking-runs", planBody)
	if r.Code != 200 {
		t.Fatalf("create: %d %s", r.Code, r.Body)
	}
	var run models.CookingRun
	json.Unmarshal(r.Body.Bytes(), &run)
	prep := map[string]any{"version": run.Version, "run_id": run.ID, "operation_id": uuid.NewString(), "source_signature": run.SourceSignature, "prepared_meat_g": "1000", "flavors": []any{map[string]any{"recipe_id": f.PersonalRecipe.ID, "actual_meat_g": "1000"}}}
	path := fmt.Sprintf("/cooking-runs/%d/preparation", run.ID)
	r = request(SaveCookingPreparation, "PUT", "/cooking-runs/:id/preparation", path, prep)
	if r.Code != 200 {
		t.Fatalf("preparation: %d %s", r.Code, r.Body)
	}
	json.Unmarshal(r.Body.Bytes(), &run)
	out := map[string]any{"version": run.Version, "stock_version": run.StockVersion, "operation_id": uuid.NewString(), "actual_usable_g": []any{map[string]any{"recipe_id": f.PersonalRecipe.ID, "quantity_g": "300"}}, "packing": map[string]any{"lines": []any{map[string]any{"product_id": f.PersonalProduct.ID, "quantity": 2}}}}
	r = request(SaveCookingResult, "POST", "/cooking-runs/:id/results", fmt.Sprintf("/cooking-runs/%d/results", run.ID), out)
	if r.Code != 200 {
		t.Fatalf("result: %d %s", r.Code, r.Body)
	}
	var eventCount int64
	database.DB.Model(&models.CookingEvent{}).Count(&eventCount)
	r = request(SaveCookingResult, "POST", "/cooking-runs/:id/results", fmt.Sprintf("/cooking-runs/%d/results", run.ID), out)
	if r.Code != 200 {
		t.Fatalf("result replay: %d %s", r.Code, r.Body)
	}
	var after int64
	database.DB.Model(&models.CookingEvent{}).Count(&after)
	if eventCount != after {
		t.Fatal("replay duplicated event")
	}
	var item models.OrderItem
	database.DB.Where("order_id = ?", f.PersonalOrder.ID).First(&item)
	full := map[string]any{"client_id": f.PersonalClient.ID, "status": "finished", "items": []any{map[string]any{"id": item.ID, "product_id": item.ProductID, "quantity": item.Quantity, "price": item.Price, "cost_price": item.Cost_price}}}
	r = request(UpdateOrder, http.MethodPut, "/orders/:id", fmt.Sprintf("/orders/%d", f.PersonalOrder.ID), full)
	if r.Code != 200 {
		t.Fatalf("full order issue: %d %s", r.Code, r.Body)
	}
	check := func(want string) {
		t.Helper()
		var lot models.CookingStockLot
		if e := database.DB.Where("workspace_id = ? AND kind = ?", f.PersonalWorkspace.ID, "packaged").First(&lot).Error; e != nil {
			t.Fatal(e)
		}
		if lot.Quantity != want {
			t.Fatalf("package stock=%s want%s", lot.Quantity, want)
		}
	}
	check("1")
	for _, status := range []string{"finished", "ready", "finished"} {
		r = request(UpdateOrderStatus, http.MethodPut, "/orders/:id/status", fmt.Sprintf("/orders/%d/status", f.PersonalOrder.ID), map[string]any{"status": status})
		if r.Code != 200 {
			t.Fatalf("status %s: %d %s", status, r.Code, r.Body)
		}
		check("1")
	}
	r = runWorkspaceJSONRequest(f.User.ID, f.SecondWorkspace.ID, GetCookingRun, "GET", "/cooking-runs/:id", fmt.Sprintf("/cooking-runs/%d", run.ID), nil)
	if r.Code != 404 {
		t.Fatal("cross-workspace cooking exposed", r.Code)
	}
}

func TestCookingViewerCannotMutateTrackedOrderThroughLegacyRoutes(t *testing.T) {
	f := setupCookingController(t)
	var item models.OrderItem
	if e := database.DB.Where("order_id = ?", f.PersonalOrder.ID).First(&item).Error; e != nil {
		t.Fatal(e)
	}
	link := models.CookingRunOrder{WorkspaceID: f.PersonalWorkspace.ID, RunID: 1, OrderID: f.PersonalOrder.ID, OrderItemID: item.ID, Quantity: 1}
	if e := database.DB.Create(&link).Error; e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		method, pattern, path string
		handler               gin.HandlerFunc
		body                  any
	}{
		{"PUT", "/orders/:id/status", fmt.Sprintf("/orders/%d/status", f.PersonalOrder.ID), UpdateOrderStatus, map[string]any{"status": "finished"}},
		{"PUT", "/orders/:id", fmt.Sprintf("/orders/%d", f.PersonalOrder.ID), UpdateOrder, map[string]any{"client_id": f.PersonalClient.ID, "status": "canceled", "items": []any{map[string]any{"id": item.ID, "product_id": item.ProductID, "quantity": 1, "price": item.Price, "cost_price": item.Cost_price}}}},
		{"DELETE", "/orders/:id", fmt.Sprintf("/orders/%d", f.PersonalOrder.ID), DeleteOrder, nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.pattern, func(t *testing.T) {
			r := runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, func(c *gin.Context) { c.Set("workspaceRole", "viewer"); tc.handler(c) }, tc.method, tc.pattern, tc.path, tc.body)
			if r.Code != 403 {
				t.Fatalf("viewer mutation = %d %s", r.Code, r.Body)
			}
			var order models.Order
			if e := database.DB.First(&order, f.PersonalOrder.ID).Error; e != nil {
				t.Fatal(e)
			}
			if order.Status != "new" {
				t.Fatal("viewer changed tracked order", order.Status)
			}
		})
	}
}

func TestCookingFeatureOffBlocksNewStockButAllowsExistingLedgerCorrection(t *testing.T) {
	f := setupCookingController(t)
	request := func(body any) *httptest.ResponseRecorder {
		return runWorkspaceJSONRequest(f.User.ID, f.PersonalWorkspace.ID, AdjustCookingStock, "POST", "/cooking-stock/adjustments", "/cooking-stock/adjustments", body)
	}
	r := request(map[string]any{"kind": "initial", "reason": "Synthetic opening", "recipe_id": f.PersonalRecipe.ID, "quantity": "10", "stock_version": 1, "operation_id": uuid.NewString()})
	if r.Code != 200 {
		t.Fatalf("initial=%d %s", r.Code, r.Body)
	}
	var stock models.CookingStock
	if e := json.Unmarshal(r.Body.Bytes(), &stock); e != nil {
		t.Fatal(e)
	}
	t.Setenv("COOKING_ENABLED", "false")
	for _, kind := range []string{"initial", "return"} {
		r = request(map[string]any{"kind": kind, "reason": "Synthetic", "recipe_id": f.PersonalRecipe.ID, "quantity": "5", "stock_version": stock.Version, "operation_id": uuid.NewString()})
		if r.Code != 503 {
			t.Fatalf("feature-off %s=%d %s", kind, r.Code, r.Body)
		}
	}
	r = request(map[string]any{"kind": "correction", "reason": "Synthetic count delta", "lot_id": stock.Lots[0].ID, "quantity": "-1", "stock_version": stock.Version, "operation_id": uuid.NewString()})
	if r.Code != 200 {
		t.Fatalf("existing correction=%d %s", r.Code, r.Body)
	}
	if e := json.Unmarshal(r.Body.Bytes(), &stock); e != nil {
		t.Fatal(e)
	}
	if stock.Lots[0].Quantity != "9" {
		t.Fatal("signed correction did not subtract", stock.Lots)
	}
}
