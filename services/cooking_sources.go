package services

import (
	"gorm.io/gorm"
	"math/big"
	"mobile-backend-go/models"
)

// refreshPlanUses exposes only remaining holds, never already-issued packages as reusable sources.
func (s CookingService) refreshPlanUses(tx *gorm.DB, run *models.CookingRun) error {
	ids := []uint{}
	for _, use := range run.PlanInput.StockUses {
		ids = append(ids, use.LotID)
	}
	run.PlanInput.StockUses = []models.CookingStockUse{}
	if len(ids) == 0 {
		return nil
	}
	var holds []models.CookingStockAllocation
	if err := tx.Where("workspace_id = ? AND run_id = ? AND lot_id IN ? AND state = ?", s.WorkspaceID, run.ID, ids, "held").Order("lot_id ASC,id ASC").Find(&holds).Error; err != nil {
		return err
	}
	byLot := map[uint]*big.Rat{}
	order := []uint{}
	for _, a := range holds {
		if byLot[a.LotID] == nil {
			byLot[a.LotID] = new(big.Rat)
			order = append(order, a.LotID)
		}
		n, err := stockNumber(a.Quantity)
		if err != nil {
			return err
		}
		byLot[a.LotID].Add(byLot[a.LotID], n)
	}
	for _, id := range order {
		run.PlanInput.StockUses = append(run.PlanInput.StockUses, models.CookingStockUse{LotID: id, Quantity: stockText(byLot[id])})
	}
	return nil
}
