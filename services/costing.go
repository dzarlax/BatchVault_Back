package services

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
	"mobile-backend-go/models"
	"mobile-backend-go/utils"
)

type recipeCost struct {
	recipe   models.Recipe
	batch    *big.Rat
	rate     *big.Rat
	estimate models.CostEstimate
}

// Costing loads a workspace's costing graph once within its caller's transaction.
type Costing struct {
	db          *gorm.DB
	workspaceID uint
	recipes     map[uint]*recipeCost
	packages    map[uint]models.Package
	now         time.Time
}

func NewCosting(db *gorm.DB, workspaceID uint) *Costing {
	return &Costing{db: db, workspaceID: workspaceID, recipes: map[uint]*recipeCost{}, packages: map[uint]models.Package{}, now: time.Now().UTC()}
}

func (s *Costing) emptyEstimate(source string) models.CostEstimate {
	return models.CostEstimate{Status: "incomplete", Source: source, Components: []models.CostComponentEstimate{}, Prices: []models.CostPriceReference{}, Issues: []models.CostIssue{}, CalculatedAt: s.now}
}
func issue(e *models.CostEstimate, code, message string, recipeID, ingredientID uint) {
	e.Issues = append(e.Issues, models.CostIssue{Code: code, Message: message, RecipeID: recipeID, IngredientID: ingredientID})
}

func (s *Costing) loadRecipes(ids []uint) error {
	missing := []uint{}
	seen := map[uint]bool{}
	for _, id := range ids {
		if id != 0 && s.recipes[id] == nil && !seen[id] {
			missing = append(missing, id)
			seen[id] = true
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var recipes []models.Recipe
	if err := s.db.Where("workspace_id = ? AND id IN ?", s.workspaceID, missing).Preload("RecipeIngredients.Ingredient").Find(&recipes).Error; err != nil {
		return err
	}
	ingredientIDs := []uint{}
	ingredientSeen := map[uint]bool{}
	for _, recipe := range recipes {
		for _, ri := range recipe.RecipeIngredients {
			if !ingredientSeen[ri.IngredientID] {
				ingredientIDs = append(ingredientIDs, ri.IngredientID)
				ingredientSeen[ri.IngredientID] = true
			}
		}
	}
	prices := map[uint]models.Price{}
	if len(ingredientIDs) > 0 {
		ranked := s.db.Model(&models.Price{}).Select("id, ROW_NUMBER() OVER (PARTITION BY ingredient_id ORDER BY date DESC, created_at DESC, id DESC) AS price_rank").Where("workspace_id = ? AND ingredient_id IN ?", s.workspaceID, ingredientIDs)
		var latest []models.Price
		if err := s.db.Where("id IN (?)", s.db.Table("(?) AS ranked_prices", ranked).Select("id").Where("price_rank = 1")).Find(&latest).Error; err != nil {
			return err
		}
		for _, price := range latest {
			prices[price.IngredientID] = price
		}
	}
	for _, recipe := range recipes {
		r := &recipeCost{recipe: recipe, batch: new(big.Rat), estimate: s.emptyEstimate(models.CostModeRecipe)}
		r.estimate.PlannedYieldG = recipe.PlannedYieldG
		if len(recipe.RecipeIngredients) == 0 {
			issue(&r.estimate, "empty_recipe", "Recipe has no ingredients", recipe.ID, 0)
		}
		for i, ri := range recipe.RecipeIngredients {
			r.recipe.RecipeIngredients[i].CostStatus = "incomplete"
			if ri.Ingredient.ID == 0 {
				issue(&r.estimate, "missing_ingredient", "Recipe ingredient is unavailable", recipe.ID, ri.IngredientID)
			}
			if strings.EqualFold(strings.TrimSpace(ri.Ingredient.Type), "packaging") {
				issue(&r.estimate, "packaging_in_recipe", "Use a content-only recipe without packaging ingredients", recipe.ID, ri.IngredientID)
			}
			price, exists := prices[ri.IngredientID]
			if !exists {
				issue(&r.estimate, "missing_price", "Ingredient has no recorded price", recipe.ID, ri.IngredientID)
				continue
			}
			r.recipe.RecipeIngredients[i].Ingredient.Prices = []models.Price{price}
			quantity, err := ParseCostDecimal(ri.Quantity)
			if err != nil || quantity.Sign() <= 0 {
				issue(&r.estimate, "invalid_quantity", "Ingredient quantity must be positive", recipe.ID, ri.IngredientID)
				continue
			}
			purchase, err := costFloat(price.Quantity)
			if err != nil || purchase.Sign() <= 0 {
				issue(&r.estimate, "invalid_price_quantity", "Purchased quantity must be positive", recipe.ID, ri.IngredientID)
				continue
			}
			amount, err := costFloat(price.Price)
			if err != nil || amount.Sign() < 0 {
				issue(&r.estimate, "invalid_price", "Ingredient price must be non-negative", recipe.ID, ri.IngredientID)
				continue
			}
			priceDimension, priceScale := utils.IngredientUnitScale(price.Unit)
			recipeDimension, recipeScale := utils.IngredientUnitScale(ri.Unit)
			if priceDimension != recipeDimension {
				issue(&r.estimate, "incompatible_units", "Ingredient and purchase units are incompatible", recipe.ID, ri.IngredientID)
				continue
			}
			cost := new(big.Rat).Mul(amount, quantity)
			cost.Mul(cost, big.NewRat(recipeScale, priceScale)).Quo(cost, purchase)
			displayCost := approximateCost(cost)
			if displayCost == nil {
				issue(&r.estimate, "amount_out_of_range", "Ingredient cost exceeds supported range", recipe.ID, ri.IngredientID)
				continue
			}
			r.batch.Add(r.batch, cost)
			r.recipe.RecipeIngredients[i].CalculatedCost = *displayCost
			r.recipe.RecipeIngredients[i].CostStatus = "complete"
			r.estimate.Prices = append(r.estimate.Prices, models.CostPriceReference{ID: price.ID, IngredientID: ri.IngredientID, Date: price.Date})
		}
		if known := approximateCost(r.batch); known != nil {
			r.recipe.TotalCost = *known
		} else {
			issue(&r.estimate, "amount_out_of_range", "Recipe cost exceeds supported range", recipe.ID, 0)
		}
		r.estimate.KnownIngredientCost = r.recipe.TotalCost
		yield, err := decimalValue(recipe.PlannedYieldG)
		if err != nil || yield.Sign() <= 0 {
			issue(&r.estimate, "missing_yield", "Set the planned finished recipe yield in grams", recipe.ID, 0)
		}
		if len(r.estimate.Issues) == 0 {
			r.rate = new(big.Rat).Quo(r.batch, yield)
			if approximateCost(r.rate) == nil {
				issue(&r.estimate, "amount_out_of_range", "Finished content rate exceeds supported range", recipe.ID, 0)
				r.rate = nil
			}
		}
		if len(r.estimate.Issues) == 0 {
			r.estimate.Status = "complete"
			r.estimate.TotalCost = approximateCost(r.batch)
			r.estimate.ContentCost = approximateCost(r.batch)
			r.estimate.CostPerGram = approximateCost(r.rate)
		}
		r.recipe.CostEstimate = &r.estimate
		s.recipes[recipe.ID] = r
	}
	return nil
}

func (s *Costing) AttachRecipes(recipes []models.Recipe) error {
	ids := make([]uint, 0, len(recipes))
	for _, recipe := range recipes {
		ids = append(ids, recipe.ID)
	}
	if err := s.loadRecipes(ids); err != nil {
		return err
	}
	for i := range recipes {
		if r := s.recipes[recipes[i].ID]; r != nil {
			recipes[i] = r.recipe
		}
	}
	return nil
}

func (s *Costing) AttachProducts(products []models.Product) error {
	assortmentIDs, recipeIDs, packageIDs := []uint{}, []uint{}, []uint{}
	for _, p := range products {
		if p.CostMode == models.CostModeAssortment && p.ID != 0 {
			assortmentIDs = append(assortmentIDs, p.ID)
		}
	}
	components := map[uint][]models.ProductCostComponent{}
	if len(assortmentIDs) > 0 {
		var rows []models.ProductCostComponent
		if err := s.db.Where("workspace_id = ? AND product_id IN ?", s.workspaceID, assortmentIDs).Order("position, id").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			components[row.ProductID] = append(components[row.ProductID], row)
		}
	}
	for i := range products {
		p := &products[i]
		if p.CostMode == models.CostModeAssortment && p.ID != 0 {
			p.CostComponents = components[p.ID]
		}
		if p.CostMode != models.CostModeRecipe && p.CostMode != models.CostModeAssortment {
			continue
		}
		packageIDs = append(packageIDs, p.PackageID)
		if p.CostRecipeID != nil {
			recipeIDs = append(recipeIDs, *p.CostRecipeID)
		}
		for _, component := range p.CostComponents {
			recipeIDs = append(recipeIDs, component.RecipeID)
		}
	}
	if err := s.loadRecipes(recipeIDs); err != nil {
		return err
	}
	if len(packageIDs) > 0 {
		var packages []models.Package
		if err := s.db.Where("workspace_id = ? AND id IN ?", s.workspaceID, packageIDs).Find(&packages).Error; err != nil {
			return err
		}
		for _, p := range packages {
			s.packages[p.ID] = p
		}
	}
	for i := range products {
		products[i].CostEstimate = s.calculateProduct(products[i])
	}
	return nil
}

func (s *Costing) calculateProduct(product models.Product) *models.CostEstimate {
	mode := product.CostMode
	if mode == "" {
		mode = models.CostModeManual
	}
	e := s.emptyEstimate(mode)
	if mode == models.CostModeManual {
		if amount, err := costFloat(product.Cost); err != nil || amount.Sign() < 0 {
			issue(&e, "invalid_manual_cost", "Manual cost is invalid", 0, 0)
			return &e
		}
		e.Status, e.TotalCost = "complete", &product.Cost
		return &e
	}
	pkg, ok := s.packages[product.PackageID]
	e.PackageID, e.NetWeightG = product.PackageID, pkg.NetWeightG
	if !ok {
		issue(&e, "missing_package", "Product package is unavailable", 0, 0)
	}
	weight, err := decimalValue(pkg.NetWeightG)
	if err != nil || weight.Sign() <= 0 {
		issue(&e, "missing_package_weight", "Set the package net product weight", 0, 0)
	}
	packageCost, err := decimalValue(pkg.UnitCost)
	if err != nil || packageCost.Sign() < 0 {
		issue(&e, "missing_package_cost", "Set the cost of one package", 0, 0)
	} else {
		e.PackagingCost = approximateCost(packageCost)
	}
	content, sum := new(big.Rat), new(big.Rat)
	rows := product.CostComponents
	if mode == models.CostModeRecipe {
		rows = nil
		if product.CostRecipeID == nil {
			issue(&e, "missing_recipe", "Select one costing recipe", 0, 0)
		} else if weight != nil {
			rows = []models.ProductCostComponent{{RecipeID: *product.CostRecipeID, QuantityG: models.Decimal(weight.FloatString(3))}}
		}
	} else if mode != models.CostModeAssortment {
		issue(&e, "invalid_cost_mode", "Unknown costing mode", 0, 0)
	}
	if mode == models.CostModeAssortment && len(rows) < 2 {
		issue(&e, "invalid_composition", "Assortment requires at least two flavours", 0, 0)
	}
	seen := map[uint]bool{}
	for _, component := range rows {
		qty, err := ParseCostDecimal(string(component.QuantityG))
		if err != nil || qty.Sign() <= 0 || seen[component.RecipeID] {
			issue(&e, "invalid_composition", "Component grams must be positive and recipes distinct", component.RecipeID, 0)
			continue
		}
		seen[component.RecipeID] = true
		sum.Add(sum, qty)
		r := s.recipes[component.RecipeID]
		row := models.CostComponentEstimate{RecipeID: component.RecipeID, QuantityG: component.QuantityG}
		if r == nil {
			issue(&e, "missing_recipe", "Component recipe is unavailable in this workspace", component.RecipeID, 0)
		} else {
			row.Name, row.PlannedYieldG, row.RecipeCost = r.recipe.Name, r.recipe.PlannedYieldG, approximateCost(r.batch)
			e.Prices = append(e.Prices, r.estimate.Prices...)
			e.Issues = append(e.Issues, r.estimate.Issues...)
			if r.rate != nil {
				cost := new(big.Rat).Mul(r.rate, qty)
				content.Add(content, cost)
				row.ContentCost = approximateCost(cost)
				row.CostPerGram = approximateCost(r.rate)
			}
		}
		e.Components = append(e.Components, row)
	}
	if mode == models.CostModeAssortment && weight != nil && sum.Cmp(weight) != 0 {
		issue(&e, "weight_mismatch", "Component grams must equal the package net weight", 0, 0)
	}
	if len(e.Issues) == 0 {
		total := new(big.Rat).Add(content, packageCost)
		amount, err := roundedCost(total)
		if err != nil {
			issue(&e, "amount_out_of_range", err.Error(), 0, 0)
		} else {
			e.Status, e.TotalCost, e.ContentCost = "complete", amount, approximateCost(content)
		}
	}
	return &e
}

// CostConfigError identifies invalid client configuration rather than a database failure.
type CostConfigError struct{ Message string }

func (e *CostConfigError) Error() string     { return e.Message }
func invalidCostConfig(message string) error { return &CostConfigError{Message: message} }

// ValidateProductConfig checks structure and ownership; missing readiness inputs are allowed.
func (s *Costing) ValidateProductConfig(product models.Product) error {
	if product.CostMode == "" {
		product.CostMode = models.CostModeManual
	}
	if product.CostMode != models.CostModeManual && product.CostMode != models.CostModeRecipe && product.CostMode != models.CostModeAssortment {
		return invalidCostConfig("invalid cost_mode")
	}
	var pkg models.Package
	if err := s.db.Where("id = ? AND workspace_id = ?", product.PackageID, s.workspaceID).First(&pkg).Error; err != nil {
		return err
	}
	s.packages[pkg.ID] = pkg
	if product.CostMode == models.CostModeManual {
		if product.CostRecipeID != nil || len(product.CostComponents) > 0 {
			return invalidCostConfig("manual costing cannot have a costing recipe or components")
		}
		return nil
	}
	ids := []uint{}
	if product.CostMode == models.CostModeRecipe {
		if product.CostRecipeID == nil || *product.CostRecipeID == 0 || len(product.CostComponents) > 0 {
			return invalidCostConfig("recipe costing requires exactly one cost_recipe_id and no components")
		}
		ids = append(ids, *product.CostRecipeID)
	} else {
		if product.CostRecipeID != nil || len(product.CostComponents) < 2 {
			return invalidCostConfig("assortment requires at least two components and no cost_recipe_id")
		}
		seen, sum := map[uint]bool{}, new(big.Rat)
		for _, row := range product.CostComponents {
			if row.RecipeID == 0 || seen[row.RecipeID] || ValidateCostDecimal(&row.QuantityG, 3, false) != nil {
				return invalidCostConfig("components require distinct recipe IDs and positive grams with up to three decimals")
			}
			seen[row.RecipeID] = true
			q, _ := ParseCostDecimal(string(row.QuantityG))
			sum.Add(sum, q)
			ids = append(ids, row.RecipeID)
		}
		if weight, err := decimalValue(pkg.NetWeightG); err == nil && sum.Cmp(weight) != 0 {
			return invalidCostConfig("component grams must equal the package net weight")
		}
	}
	if err := s.loadRecipes(ids); err != nil {
		return err
	}
	for _, id := range ids {
		if s.recipes[id] == nil {
			return invalidCostConfig("costing recipe does not belong to this workspace")
		}
	}
	return nil
}

func NewCostSnapshot(source string, amount float64, estimate *models.CostEstimate) *models.CostSnapshot {
	return &models.CostSnapshot{Version: 1, Source: source, Amount: amount, Estimate: estimate, CalculatedAt: time.Now().UTC()}
}

func IsCalculatedSnapshot(snapshot *models.CostSnapshot) (bool, error) {
	if snapshot == nil {
		return false, nil
	}
	if snapshot.Version != 1 {
		return false, fmt.Errorf("unsupported cost snapshot version")
	}
	switch snapshot.Source {
	case models.CostModeManual:
		return false, nil
	case models.CostModeRecipe, models.CostModeAssortment:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported cost snapshot source")
	}
}
