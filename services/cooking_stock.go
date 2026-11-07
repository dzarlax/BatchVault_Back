package services

import (
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"math/big"
	"mobile-backend-go/models"
	"strings"
)

func (s CookingService) holdPlanStock(tx *gorm.DB, run *models.CookingRun, uses []models.CookingStockUse) error {
	for _, use := range uses {
		var lot models.CookingStockLot
		if e := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, use.LotID).First(&lot).Error; e != nil {
			return e
		}
		available, e := subtractStock(lot.Quantity, lot.Reserved)
		if e != nil {
			return e
		}
		if _, e = subtractStock(available, use.Quantity); e != nil {
			return e
		}
		if lot.Kind == "packaged" {
			if e = s.reservePackages(tx, run, lot, use.Quantity); e != nil {
				return e
			}
		} else {
			lot.Reserved, e = addStock(lot.Reserved, use.Quantity)
			if e != nil {
				return e
			}
			if e = tx.Save(&lot).Error; e != nil {
				return e
			}
			a := models.CookingStockAllocation{WorkspaceID: s.WorkspaceID, RunID: run.ID, LotID: lot.ID, Quantity: use.Quantity, Kind: "source", State: "held"}
			if e = tx.Create(&a).Error; e != nil {
				return e
			}
		}
	}
	return nil
}
func (s CookingService) reservePackages(tx *gorm.DB, run *models.CookingRun, lot models.CookingStockLot, quantity string) error {
	remaining, e := stockNumber(quantity)
	if e != nil {
		return e
	}
	for _, line := range run.Plan.OrderLines {
		if line.ProductID != lot.ProductID || remaining.Sign() == 0 {
			continue
		}
		var item models.OrderItem
		if e = tx.Where("id = ? AND order_id = ?", line.OrderItemID, line.OrderID).First(&item).Error; errors.Is(e, gorm.ErrRecordNotFound) {
			continue
		} else if e != nil {
			return e
		}
		var order models.Order
		if e = tx.Where("id = ? AND workspace_id = ?", line.OrderID, s.WorkspaceID).First(&order).Error; e != nil {
			return e
		}
		if order.Status == "finished" || order.Status == "canceled" || item.ProductID != lot.ProductID {
			continue
		}
		held := new(big.Rat)
		var prior []models.CookingStockAllocation
		if e = tx.Table("cooking_stock_allocations AS a").Select("a.*").Joins("JOIN cooking_stock_lots AS l ON l.id = a.lot_id AND l.workspace_id = a.workspace_id").Where("a.workspace_id = ? AND a.order_item_id = ? AND a.kind = ? AND a.state IN ? AND l.product_id = ?", s.WorkspaceID, item.ID, "order", []string{"held", "consumed"}, item.ProductID).Find(&prior).Error; e != nil {
			return e
		}
		for _, a := range prior {
			n, e := stockNumber(a.Quantity)
			if e != nil {
				return e
			}
			held.Add(held, n)
		}
		needed := new(big.Rat).Sub(big.NewRat(int64(item.Quantity), 1), held)
		if needed.Sign() <= 0 {
			continue
		}
		if needed.Cmp(remaining) > 0 {
			needed.Set(remaining)
		}
		a := models.CookingStockAllocation{WorkspaceID: s.WorkspaceID, RunID: run.ID, LotID: lot.ID, OrderID: order.ID, OrderItemID: item.ID, Quantity: stockText(needed), Kind: "order", State: "held"}
		if e = tx.Create(&a).Error; e != nil {
			return e
		}
		lot.Reserved, e = addStock(lot.Reserved, a.Quantity)
		if e != nil {
			return e
		}
		remaining.Sub(remaining, needed)
	}
	return tx.Model(&models.CookingStockLot{}).Where("id = ? AND workspace_id = ?", lot.ID, s.WorkspaceID).Update("reserved", lot.Reserved).Error
}
func (s CookingService) bulkSources(tx *gorm.DB, run models.CookingRun, sources []models.CookingStockUse) ([]models.CookingStockLot, map[uint]string, error) {
	var lots []models.CookingStockLot
	if e := tx.Where("workspace_id = ? AND kind = ?", s.WorkspaceID, "bulk").Order("id ASC").Find(&lots).Error; e != nil {
		return nil, nil, e
	}
	own := map[uint]*big.Rat{}
	var holds []models.CookingStockAllocation
	if e := tx.Where("workspace_id = ? AND run_id = ? AND kind = ? AND state = ?", s.WorkspaceID, run.ID, "source", "held").Order("id ASC").Find(&holds).Error; e != nil {
		return nil, nil, e
	}
	for _, a := range holds {
		n, e := stockNumber(a.Quantity)
		if e != nil {
			return nil, nil, e
		}
		if own[a.LotID] == nil {
			own[a.LotID] = new(big.Rat)
		}
		own[a.LotID].Add(own[a.LotID], n)
	}
	requested := map[uint]string{}
	for _, a := range sources {
		if a.LotID == 0 || requested[a.LotID] != "" {
			return nil, nil, cookingError("invalid_source", "sources", "Source lots must be unique")
		}
		if _, e := cookingRat(a.Quantity, "sources.quantity", false); e != nil {
			return nil, nil, e
		}
		requested[a.LotID] = a.Quantity
	}
	selected := []models.CookingStockLot{}
	available := map[uint]string{}
	for _, lot := range lots {
		if len(sources) == 0 && lot.RunID != run.ID && own[lot.ID] == nil {
			continue
		}
		if len(sources) > 0 && requested[lot.ID] == "" {
			continue
		}
		free, e := subtractStock(lot.Quantity, lot.Reserved)
		if e != nil {
			return nil, nil, e
		}
		n, e := stockNumber(free)
		if e != nil {
			return nil, nil, e
		}
		if own[lot.ID] != nil {
			n.Add(n, own[lot.ID])
		}
		if len(sources) > 0 {
			want, _ := stockNumber(requested[lot.ID])
			if want.Cmp(n) > 0 {
				return nil, nil, cookingConflict("insufficient_stock", "Selected source weight is no longer available")
			}
			n = want
			delete(requested, lot.ID)
		}
		lot.Available = stockText(n)
		selected = append(selected, lot)
		total := available[lot.RecipeID]
		if total == "" {
			total = "0"
		}
		available[lot.RecipeID], e = addStock(total, lot.Available)
		if e != nil {
			return nil, nil, e
		}
	}
	if len(requested) > 0 {
		return nil, nil, gorm.ErrRecordNotFound
	}
	return selected, available, nil
}
func validateCookingOutput(run models.CookingRun, output []models.CookingComponent) ([]models.CookingComponent, error) {
	if run.Preparation == nil {
		return nil, cookingConflict("state_conflict", "Save preparation before recording cooked output")
	}
	expected := map[uint]bool{}
	for _, f := range run.Preparation.Flavors {
		expected[f.RecipeID] = true
	}
	seen := map[uint]bool{}
	result := []models.CookingComponent{}
	for _, v := range output {
		if !expected[v.RecipeID] || seen[v.RecipeID] {
			return nil, cookingError("invalid_output", "actual_usable_g", "Output must list every prepared flavor exactly once")
		}
		seen[v.RecipeID] = true
		n, e := cookingRat(v.QuantityG, "actual_usable_g.quantity_g", true)
		if e != nil {
			return nil, e
		}
		result = append(result, models.CookingComponent{RecipeID: v.RecipeID, QuantityG: stockText(n)})
	}
	if len(seen) != len(expected) {
		return nil, cookingError("missing_output", "actual_usable_g", "Enter actual usable output, including an explicit zero, for every flavor")
	}
	return result, nil
}
func (s CookingService) PackingPreview(id uint, input models.CookingPackingInput) (models.CookingPackingPreview, error) {
	run, e := s.Run(id)
	if e != nil {
		return models.CookingPackingPreview{}, e
	}
	_, available, e := s.bulkSources(s.DB, run, input.Sources)
	if e != nil {
		return models.CookingPackingPreview{}, e
	}
	if len(input.ActualUsableG) > 0 {
		if run.State == "cooked" {
			return models.CookingPackingPreview{}, cookingConflict("state_conflict", "Cooked output is already recorded")
		}
		output, e := validateCookingOutput(run, input.ActualUsableG)
		if e != nil {
			return models.CookingPackingPreview{}, e
		}
		for _, v := range output {
			n := available[v.RecipeID]
			if n == "" {
				n = "0"
			}
			available[v.RecipeID], e = addStock(n, v.QuantityG)
			if e != nil {
				return models.CookingPackingPreview{}, e
			}
		}
	}
	return CalculateCookingPacking(run.Plan.Products, available, input.Lines)
}
func (s CookingService) Results(id uint, input models.CookingResultInput) (json.RawMessage, error) {
	request := struct {
		RunID uint
		Input models.CookingResultInput
	}{id, input}
	return s.mutate("result", input.OperationID, request, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		var run models.CookingRun
		if e := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, id).First(&run).Error; e != nil {
			return 0, e
		}
		if e := versionCooking(run.Version, input.Version); e != nil {
			return 0, e
		}
		if e := versionCooking(state.Version, input.StockVersion); e != nil {
			return 0, e
		}
		if run.State != "prepared" {
			return 0, cookingConflict("state_conflict", "Output can only be recorded once after preparation")
		}
		output, e := validateCookingOutput(run, input.ActualUsableG)
		if e != nil {
			return 0, e
		}
		names := map[uint]string{}
		for _, f := range run.Plan.Flavors {
			names[f.RecipeID] = f.Name
		}
		outputSources := make([]models.CookingStockUse, 0, len(output))
		for _, v := range output {
			lot := models.CookingStockLot{WorkspaceID: s.WorkspaceID, RunID: run.ID, Kind: "bulk", RecipeID: v.RecipeID, Name: names[v.RecipeID], Quantity: v.QuantityG, Reserved: "0", Unit: "g", Composition: []models.CookingComponent{{RecipeID: v.RecipeID, QuantityG: "1"}}}
			if e = tx.Create(&lot).Error; e != nil {
				return 0, e
			}
			quantity, err := stockNumber(v.QuantityG)
			if err != nil {
				return 0, err
			}
			if quantity.Sign() > 0 {
				outputSources = append(outputSources, models.CookingStockUse{LotID: lot.ID, Quantity: lot.Quantity})
			}
		}
		run.ActualOutput = output
		run.State = "cooked"
		run.Version++
		if e = tx.Omit("Events", "Lots").Save(&run).Error; e != nil {
			return 0, e
		}
		if input.Packing != nil {
			packing := *input.Packing
			if len(packing.Sources) > 0 {
				// Preview combines explicit external sources with this new output.
				// Its new lot IDs are server-owned and cannot be selected by the caller.
				packing.Sources = append(append([]models.CookingStockUse(nil), packing.Sources...), outputSources...)
			}
			if e = s.applyPacking(tx, &run, packing); e != nil {
				return 0, e
			}
		}
		return id, nil
	})
}
func (s CookingService) Pack(id uint, input models.CookingPackingInput) (json.RawMessage, error) {
	request := struct {
		RunID uint
		Input models.CookingPackingInput
	}{id, input}
	return s.mutate("packing", input.OperationID, request, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		var run models.CookingRun
		if e := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, id).First(&run).Error; e != nil {
			return 0, e
		}
		if e := versionCooking(run.Version, input.Version); e != nil {
			return 0, e
		}
		if e := versionCooking(state.Version, input.StockVersion); e != nil {
			return 0, e
		}
		if run.State != "cooked" {
			return 0, cookingConflict("state_conflict", "Record cooked output before packing")
		}
		if e := s.applyPacking(tx, &run, input); e != nil {
			return 0, e
		}
		run.Version++
		if e := tx.Omit("Events", "Lots").Save(&run).Error; e != nil {
			return 0, e
		}
		return id, nil
	})
}
func (s CookingService) applyPacking(tx *gorm.DB, run *models.CookingRun, input models.CookingPackingInput) error {
	lots, available, e := s.bulkSources(tx, *run, input.Sources)
	if e != nil {
		return e
	}
	preview, e := CalculateCookingPacking(run.Plan.Products, available, input.Lines)
	if e != nil {
		return e
	}
	if preview.TotalPacks == 0 {
		return cookingError("empty_packing", "lines", "Enter at least one whole package")
	}
	for _, v := range preview.Consumption {
		needed, _ := stockNumber(v.QuantityG)
		for _, lot := range lots {
			if lot.RecipeID != v.RecipeID || needed.Sign() == 0 {
				continue
			}
			free, _ := stockNumber(lot.Available)
			take := new(big.Rat).Set(needed)
			if take.Cmp(free) > 0 {
				take.Set(free)
			}
			if take.Sign() == 0 {
				continue
			}
			takeText := stockText(take)
			// Consume held sources before unreserved grams, retaining any unused hold.
			var holds []models.CookingStockAllocation
			if e = tx.Where("workspace_id = ? AND run_id = ? AND lot_id = ? AND kind = ? AND state = ?", s.WorkspaceID, run.ID, lot.ID, "source", "held").Order("id ASC").Find(&holds).Error; e != nil {
				return e
			}
			release := new(big.Rat).Set(take)
			for _, hold := range holds {
				n, _ := stockNumber(hold.Quantity)
				used := new(big.Rat).Set(release)
				if used.Cmp(n) > 0 {
					used.Set(n)
				}
				if used.Sign() == 0 {
					continue
				}
				hold.Quantity = stockText(new(big.Rat).Sub(n, used))
				if hold.Quantity == "0.000" {
					hold.State = "consumed"
				}
				if e = tx.Save(&hold).Error; e != nil {
					return e
				}
				lot.Reserved, e = subtractStock(lot.Reserved, stockText(used))
				if e != nil {
					return e
				}
				release.Sub(release, used)
			}
			lot.Quantity, e = subtractStock(lot.Quantity, takeText)
			if e != nil {
				return e
			}
			if e = tx.Omit("Available").Save(&lot).Error; e != nil {
				return e
			}
			if e = tx.Create(&models.CookingStockAllocation{WorkspaceID: s.WorkspaceID, RunID: run.ID, LotID: lot.ID, Quantity: takeText, Kind: "bulk_use", State: "consumed"}).Error; e != nil {
				return e
			}
			needed.Sub(needed, take)
		}
		if needed.Sign() != 0 {
			return cookingConflict("insufficient_stock", "Insufficient flavor stock")
		}
	}
	specs := map[uint]models.CookingProductSnapshot{}
	for _, v := range run.Plan.Products {
		specs[v.ID] = v
	}
	for _, line := range preview.Lines {
		if line.Quantity == 0 {
			continue
		}
		p := specs[line.ProductID]
		lot := models.CookingStockLot{WorkspaceID: s.WorkspaceID, RunID: run.ID, Kind: "packaged", ProductID: p.ID, Name: p.Name, Quantity: stockText(big.NewRat(int64(line.Quantity), 1)), Reserved: "0", Unit: "pcs", Composition: p.Components, ConfigurationVersion: p.ConfigurationVersion}
		if e = tx.Create(&lot).Error; e != nil {
			return e
		}
		if e = s.reservePackages(tx, run, lot, lot.Quantity); e != nil {
			return e
		}
	}
	return nil
}
func (s CookingService) Archive(id uint, input models.CookingArchiveInput) (json.RawMessage, error) {
	request := struct {
		RunID uint
		Input models.CookingArchiveInput
	}{id, input}
	return s.mutate("archive", input.OperationID, request, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		var run models.CookingRun
		if e := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, id).First(&run).Error; e != nil {
			return 0, e
		}
		if e := versionCooking(run.Version, input.Version); e != nil {
			return 0, e
		}
		var holds []models.CookingStockAllocation
		if e := tx.Where("workspace_id = ? AND run_id = ? AND kind = ? AND state = ?", s.WorkspaceID, id, "source", "held").Find(&holds).Error; e != nil {
			return 0, e
		}
		for _, a := range holds {
			if e := s.releaseAllocation(tx, &a, a.Quantity); e != nil {
				return 0, e
			}
		}
		run.State = "archived"
		run.Version++
		if e := tx.Omit("Events", "Lots").Save(&run).Error; e != nil {
			return 0, e
		}
		return id, nil
	})
}
func (s CookingService) releaseAllocation(tx *gorm.DB, a *models.CookingStockAllocation, amount string) error {
	var lot models.CookingStockLot
	if e := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, a.LotID).First(&lot).Error; e != nil {
		return e
	}
	var e error
	lot.Reserved, e = subtractStock(lot.Reserved, amount)
	if e != nil {
		return e
	}
	if e = tx.Save(&lot).Error; e != nil {
		return e
	}
	n, e := subtractStock(a.Quantity, amount)
	if e != nil {
		return e
	}
	if n == "0.000" {
		a.State = "released"
	} else {
		a.Quantity = n
	}
	return tx.Save(a).Error
}
func (s CookingService) Adjust(input models.CookingAdjustmentInput) (json.RawMessage, error) {
	return s.mutate("adjustment", input.OperationID, input, func(tx *gorm.DB, state *models.CookingWorkspaceState) (uint, error) {
		if e := versionCooking(state.Version, input.StockVersion); e != nil {
			return 0, e
		}
		if strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 1000 {
			return 0, cookingError("reason_required", "reason", "Enter a correction reason of at most 1000 characters")
		}
		if input.Kind != "initial" && input.Kind != "correction" && input.Kind != "waste" && input.Kind != "return" {
			return 0, cookingError("invalid_adjustment", "kind", "Unknown stock adjustment")
		}
		if input.Kind == "initial" || input.Kind == "return" {
			if input.ProductID != 0 {
				if e := s.sourceLocks(tx, nil); e != nil {
					return 0, e
				}
			}
			qty, e := cookingRat(input.Quantity, "quantity", false)
			if e != nil {
				return 0, e
			}
			lot := models.CookingStockLot{WorkspaceID: s.WorkspaceID, Kind: "bulk", Quantity: stockText(qty), Reserved: "0", Unit: "g", RunID: 0}
			if input.RecipeID != 0 && input.ProductID == 0 {
				var recipe models.Recipe
				if e = tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, input.RecipeID).First(&recipe).Error; e != nil {
					return 0, e
				}
				lot.RecipeID = recipe.ID
				lot.Name = recipe.Name
				lot.Composition = []models.CookingComponent{{RecipeID: recipe.ID, QuantityG: "1"}}
			} else if input.ProductID != 0 && input.RecipeID == 0 {
				var products []models.Product
				var recipes []models.Recipe
				if e = tx.Where("workspace_id = ?", s.WorkspaceID).Preload("Package").Preload("CostComponents").Preload("AssortmentComponents").Find(&products).Error; e != nil {
					return 0, e
				}
				if e = tx.Where("workspace_id = ?", s.WorkspaceID).Preload("RecipeIngredients.Ingredient").Find(&recipes).Error; e != nil {
					return 0, e
				}
				v, e := BuildCookingPlan([]models.Order{{Items: []models.OrderItem{{ProductID: input.ProductID, Quantity: 1}}}}, products, recipes, nil, nil)
				if e != nil {
					return 0, e
				}
				if !qty.IsInt() {
					return 0, cookingError("invalid_quantity", "quantity", "Packages must be whole numbers")
				}
				p := v.Products[0]
				lot.Kind = "packaged"
				lot.Unit = "pcs"
				lot.ProductID = p.ID
				lot.Name = p.Name
				lot.Composition = p.Components
				lot.ConfigurationVersion = p.ConfigurationVersion
			} else {
				return 0, cookingError("invalid_adjustment", "recipe_id", "Choose exactly one recipe or product")
			}
			return 0, tx.Create(&lot).Error
		}
		var lot models.CookingStockLot
		if e := tx.Where("workspace_id = ? AND id = ?", s.WorkspaceID, input.LotID).First(&lot).Error; e != nil {
			return 0, e
		}
		delta, e := ParseCostDecimal(input.Quantity)
		if e != nil {
			return 0, cookingError("invalid_quantity", "quantity", "Enter a valid correction amount")
		}
		absolute := new(big.Rat).Abs(new(big.Rat).Set(delta))
		scaled := new(big.Rat).Mul(absolute, big.NewRat(1000, 1))
		if !scaled.IsInt() || scaled.Num().Cmp(big.NewInt(99999999999999)) > 0 {
			return 0, cookingError("invalid_quantity", "quantity", "Correction must fit numeric(14,3)")
		}
		if _, e = cookingRat(stockText(absolute), "quantity", false); e != nil {
			return 0, e
		}
		if input.Kind == "waste" {
			delta.Neg(absolute)
		}
		if lot.Kind == "packaged" && !delta.IsInt() {
			return 0, cookingError("invalid_quantity", "quantity", "Packages must be whole numbers")
		}
		current, e := stockNumber(lot.Quantity)
		if e != nil {
			return 0, e
		}
		current.Add(current, delta)
		reserved, _ := stockNumber(lot.Reserved)
		if current.Cmp(reserved) < 0 {
			return 0, cookingConflict("insufficient_stock", "Correction cannot remove reserved or already consumed stock")
		}
		text, e := cookingString(current)
		if e != nil {
			return 0, e
		}
		lot.Quantity = text
		if e = tx.Save(&lot).Error; e != nil {
			return 0, e
		}
		return lot.RunID, nil
	})
}
