package services

import (
	"encoding/json"
	"gorm.io/gorm"
	"mobile-backend-go/models"
	"strings"
)

// Replan requires a fresh explicit quote and cannot rewrite cooked output.
func (s CookingService) Replan(id uint, input models.CookingCreateInput) (json.RawMessage, error) {
	request := struct {
		RunID uint
		Input models.CookingCreateInput
	}{id, input}
	return s.mutate("replan", input.OperationID, request, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		var run models.CookingRun
		if e := tx.Where("id = ? AND workspace_id = ?", id, s.WorkspaceID).First(&run).Error; e != nil {
			return 0, e
		}
		if e := versionCooking(run.Version, input.Version); e != nil {
			return 0, e
		}
		if e := versionCooking(state.Version, input.StockVersion); e != nil {
			return 0, e
		}
		if run.State == "cooked" || run.State == "archived" {
			return 0, cookingConflict("state_conflict", "An already cooked run keeps its original specification")
		}
		if e := s.sourceLocks(tx, input.OrderIDs); e != nil {
			return 0, e
		}
		before := input.CookingPlanInput
		before.RunID = &id
		quote, e := s.loadPlan(tx, before)
		if e != nil {
			return 0, e
		}
		if quote.SourceSignature != input.SourceSignature {
			return 0, cookingConflict("stale_source", "The new plan is stale; calculate it again")
		}
		ack := map[uint]bool{}
		for _, rid := range input.AcknowledgedRunIDs {
			ack[rid] = true
		}
		for _, active := range quote.ActiveRuns {
			if !ack[active.ID] {
				return 0, cookingConflict("already_planned", "Acknowledge the other cooking plans before continuing")
			}
		}
		var held []models.CookingStockAllocation
		if e = tx.Where("workspace_id = ? AND run_id = ? AND state = ?", s.WorkspaceID, id, "held").Find(&held).Error; e != nil {
			return 0, e
		}
		for _, a := range held {
			if e = s.releaseAllocation(tx, &a, a.Quantity); e != nil {
				return 0, e
			}
		}
		rawInput := input.CookingPlanInput
		rawInput.RunID = nil
		current, e := s.loadPlan(tx, rawInput)
		if e != nil {
			return 0, e
		}
		run.PlanInput = rawInput
		run.Plan = current.Plan
		run.Preparation = nil
		run.SourceSignature = current.SourceSignature
		run.State = "draft"
		run.Version++
		if strings.TrimSpace(input.Title) != "" {
			if len(input.Title) > 200 {
				return 0, cookingError("invalid_title", "title", "Title must be at most 200 characters")
			}
			run.Title = input.Title
		}
		if e = tx.Omit("Events", "Lots").Save(&run).Error; e != nil {
			return 0, e
		}
		if e = tx.Where("workspace_id = ? AND run_id = ?", s.WorkspaceID, id).Delete(&models.CookingRunFlavor{}).Error; e != nil {
			return 0, e
		}
		if e = tx.Where("workspace_id = ? AND run_id = ?", s.WorkspaceID, id).Delete(&models.CookingRunOrder{}).Error; e != nil {
			return 0, e
		}
		for _, f := range run.Plan.Flavors {
			if e = tx.Create(&models.CookingRunFlavor{WorkspaceID: s.WorkspaceID, RunID: id, RecipeID: f.RecipeID, ActualMeatG: "0", RecipeSnapshot: f.RecipeSnapshot}).Error; e != nil {
				return 0, e
			}
		}
		for _, l := range run.Plan.OrderLines {
			if e = tx.Create(&models.CookingRunOrder{WorkspaceID: s.WorkspaceID, RunID: id, OrderID: l.OrderID, OrderItemID: l.OrderItemID, Quantity: l.Quantity}).Error; e != nil {
				return 0, e
			}
		}
		if e = s.holdPlanStock(tx, &run, rawInput.StockUses); e != nil {
			return 0, e
		}
		return id, nil
	})
}
