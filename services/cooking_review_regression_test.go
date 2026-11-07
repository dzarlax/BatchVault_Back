package services

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"mobile-backend-go/models"
)

func addCookingOpeningLot(t *testing.T, service CookingService, recipeID, productID uint, quantity string) models.CookingStockLot {
	t.Helper()
	stock, err := service.Stock()
	if err != nil {
		t.Fatal(err)
	}
	input := models.CookingAdjustmentInput{
		OperationID:  uuid.NewString(),
		StockVersion: stock.Version,
		Kind:         "initial",
		Reason:       "Synthetic regression fixture",
		RecipeID:     recipeID,
		ProductID:    productID,
		Quantity:     quantity,
	}
	if _, err = service.Adjust(input); err != nil {
		t.Fatal(err)
	}
	stock, err = service.Stock()
	if err != nil {
		t.Fatal(err)
	}
	for i := len(stock.Lots) - 1; i >= 0; i-- {
		lot := stock.Lots[i]
		if lot.RecipeID == recipeID && lot.ProductID == productID {
			return lot
		}
	}
	t.Fatalf("opening stock lot for recipe %d and product %d was not created", recipeID, productID)
	return models.CookingStockLot{}
}

func updateCookingOrder(t *testing.T, service CookingService, orderID uint, status string, mutate func(*gorm.DB) error) {
	t.Helper()
	err := CookingOrderTransaction(service.DB, service.WorkspaceID, func(tx *gorm.DB) error {
		if mutate != nil {
			if err := mutate(tx); err != nil {
				return err
			}
		}
		if err := tx.Model(&models.Order{}).Where("id = ? AND workspace_id = ?", orderID, service.WorkspaceID).Update("status", status).Error; err != nil {
			return err
		}
		return SyncCookingOrder(tx, service.WorkspaceID, service.UserID, orderID, status, false)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func cookingRunFromResult(t *testing.T, body []byte, err error) models.CookingRun {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var run models.CookingRun
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatal(err)
	}
	return run
}

func cookingFlavorAmount(t *testing.T, plan models.CookingPlan, recipeID uint) string {
	t.Helper()
	for _, flavor := range plan.Flavors {
		if flavor.RecipeID == recipeID {
			return flavor.TargetFinishedG
		}
	}
	t.Fatalf("recipe %d is missing from plan", recipeID)
	return ""
}

func TestCookingConsumedPackageCoverageDoesNotFollowReplacedOrderProduct(t *testing.T) {
	service, _ := cookingStoreFixture(t, false)
	classicLot := addCookingOpeningLot(t, service, 0, 101, "2")

	firstInput := models.CookingPlanInput{
		OrderIDs:  []uint{104},
		Extras:    []models.CookingExtraInput{},
		StockUses: []models.CookingStockUse{{LotID: classicLot.ID, Quantity: "2"}},
	}
	firstPreview, err := service.Plan(firstInput)
	if err != nil {
		t.Fatal(err)
	}
	firstBody, err := service.Create(models.CookingCreateInput{
		CookingPlanInput: firstInput,
		Title:            "Issue classic packages",
		Date:             "2026-11-07",
		SourceSignature:  firstPreview.SourceSignature,
		StockVersion:     firstPreview.StockVersion,
		OperationID:      uuid.NewString(),
	})
	first := cookingRunFromResult(t, firstBody, err)

	// Finishing the order consumes the held classic packages; reopening it leaves
	// the consumed allocation as history for its original product.
	updateCookingOrder(t, service, 104, "finished", nil)
	updateCookingOrder(t, service, 104, "new", func(tx *gorm.DB) error {
		if err := tx.Model(&models.OrderItem{}).Where("id = ? AND order_id = ?", 1, 104).Update("product_id", 102).Error; err != nil {
			return err
		}
		return tx.Delete(&models.OrderItem{}, "id = ? AND order_id = ?", 2, 104).Error
	})

	spicyLot := addCookingOpeningLot(t, service, 0, 102, "4")
	currentInput := models.CookingPlanInput{OrderIDs: []uint{104}, Extras: []models.CookingExtraInput{}}
	currentPreview, err := service.Plan(currentInput)
	if err != nil {
		t.Fatal(err)
	}
	if got := cookingFlavorAmount(t, currentPreview.Plan, 2); got != "400.000" {
		t.Fatalf("replacement product demand inherited consumed classic coverage: spicy target = %s, want 400.000", got)
	}

	currentInput.StockUses = []models.CookingStockUse{{LotID: spicyLot.ID, Quantity: "4"}}
	currentInput.AcknowledgedRunIDs = []uint{first.ID}
	currentPreview, err = service.Plan(currentInput)
	if err != nil {
		t.Fatal(err)
	}
	if got := cookingFlavorAmount(t, currentPreview.Plan, 2); got != "0.000" {
		t.Fatalf("four spicy packages did not cover four spicy orders: target = %s", got)
	}
	secondBody, err := service.Create(models.CookingCreateInput{
		CookingPlanInput: currentInput,
		Title:            "Issue spicy packages",
		Date:             "2026-11-07",
		SourceSignature:  currentPreview.SourceSignature,
		StockVersion:     currentPreview.StockVersion,
		OperationID:      uuid.NewString(),
	})
	second := cookingRunFromResult(t, secondBody, err)

	var allocation models.CookingStockAllocation
	if err := service.DB.Where("workspace_id = ? AND run_id = ? AND order_item_id = ? AND kind = ? AND state = ?", service.WorkspaceID, second.ID, 1, "order", "held").First(&allocation).Error; err != nil {
		t.Fatal(err)
	}
	var lot models.CookingStockLot
	if err := service.DB.Where("workspace_id = ? AND id = ?", service.WorkspaceID, allocation.LotID).First(&lot).Error; err != nil {
		t.Fatal(err)
	}
	if lot.ProductID != 102 || allocation.Quantity != "4.000" {
		t.Fatalf("replacement order was not reserved from spicy stock: lot=%+v allocation=%+v", lot, allocation)
	}
	updateCookingOrder(t, service, 104, "finished", nil)
	if err := service.DB.Where("workspace_id = ? AND run_id = ? AND order_item_id = ? AND lot_id = ? AND kind = ? AND state = ?", service.WorkspaceID, second.ID, 1, lot.ID, "order", "consumed").First(&allocation).Error; err != nil {
		t.Fatalf("replacement spicy packages were not issued: %v", err)
	}
}

func TestCookingReplanUsesRemainingPackageHoldsAndPreservesMakeAhead(t *testing.T) {
	service, _ := cookingStoreFixture(t, false)
	openingLot := addCookingOpeningLot(t, service, 0, 101, "2")
	input := models.CookingPlanInput{
		OrderIDs:  []uint{104},
		Extras:    []models.CookingExtraInput{{RecipeID: 1, FinishedG: "150"}},
		StockUses: []models.CookingStockUse{{LotID: openingLot.ID, Quantity: "2"}},
	}
	preview, err := service.Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	runBody, err := service.Create(models.CookingCreateInput{
		CookingPlanInput: input,
		Title:            "Order and make-ahead batch",
		Date:             "2026-11-07",
		SourceSignature:  preview.SourceSignature,
		StockVersion:     preview.StockVersion,
		OperationID:      uuid.NewString(),
	})
	run := cookingRunFromResult(t, runBody, err)
	updateCookingOrder(t, service, 104, "finished", nil)
	updateCookingOrder(t, service, 104, "new", nil)

	current, err := service.Run(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.PlanInput.StockUses) != 0 {
		t.Fatalf("issued opening packages remained reusable in refreshed plan input: %+v", current.PlanInput.StockUses)
	}
	if len(current.PlanInput.Extras) != 1 || current.PlanInput.Extras[0] != input.Extras[0] {
		t.Fatalf("make-ahead extra was not preserved: %+v", current.PlanInput.Extras)
	}

	replanInput := current.PlanInput
	replanRunID := run.ID
	replanInput.RunID = &replanRunID
	refreshed, err := service.Plan(replanInput)
	if err != nil {
		t.Fatalf("preview using remaining package holds failed: %v", err)
	}
	if got := cookingFlavorAmount(t, refreshed.Plan, 1); got != "350.000" {
		t.Fatalf("refreshed classic demand = %s, want 350.000 including make-ahead extra", got)
	}
	savedBody, err := service.Replan(run.ID, models.CookingCreateInput{
		Version:          current.Version,
		CookingPlanInput: current.PlanInput,
		Title:            current.Title,
		SourceSignature:  refreshed.SourceSignature,
		StockVersion:     refreshed.StockVersion,
		OperationID:      uuid.NewString(),
	})
	saved := cookingRunFromResult(t, savedBody, err)
	if got := cookingFlavorAmount(t, saved.Plan, 1); got != cookingFlavorAmount(t, refreshed.Plan, 1) {
		t.Fatalf("replan save differs from preview: saved %s, preview %s", got, cookingFlavorAmount(t, refreshed.Plan, 1))
	}
	if len(saved.PlanInput.StockUses) != 0 || len(saved.PlanInput.Extras) != 1 || saved.PlanInput.Extras[0] != input.Extras[0] {
		t.Fatalf("saved replan did not retain only remaining holds and original extra: %+v", saved.PlanInput)
	}
}

func TestCookingMixedBulkAndPackageSourcesRejectInEitherInputOrder(t *testing.T) {
	orders := [][]models.CookingStockUse{
		{{LotID: 0, Quantity: "100"}, {LotID: 0, Quantity: "4"}},
		{{LotID: 0, Quantity: "4"}, {LotID: 0, Quantity: "100"}},
	}
	for _, uses := range orders {
		service, _ := cookingStoreFixture(t, false)
		stock := addCookingOpeningLot(t, service, 0, 101, "4")
		bulk := addCookingOpeningLot(t, service, 1, 0, "100")
		uses[0].LotID = bulk.ID
		uses[1].LotID = stock.ID
		_, err := service.Plan(models.CookingPlanInput{
			OrderIDs:  []uint{104},
			Extras:    []models.CookingExtraInput{},
			StockUses: uses,
		})
		if err == nil {
			t.Fatalf("bulk and packages both covered the same flavor demand in source order %+v", uses)
		}
	}
}
