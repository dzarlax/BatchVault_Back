package services

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"mobile-backend-go/models"
)

// CookingCalculationError is a safe, field-addressable validation failure.
type CookingCalculationError struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e *CookingCalculationError) Error() string { return e.Message }

func cookingError(code, field, message string) error {
	return &CookingCalculationError{Code: code, Field: field, Message: message}
}

var cookingScale = big.NewInt(1000)

func cookingRat(text, field string, allowZero bool) (*big.Rat, error) {
	r, err := ParseCostDecimal(text)
	if err != nil || r.Sign() < 0 || (!allowZero && r.Sign() == 0) {
		return nil, cookingError("invalid_quantity", field, "Quantity must be a non-negative decimal with at most three fractional digits")
	}
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(cookingScale))
	if !scaled.IsInt() || new(big.Int).Abs(new(big.Int).Set(scaled.Num())).Cmp(big.NewInt(99999999999999)) > 0 {
		return nil, cookingError("invalid_quantity", field, "Quantity exceeds supported numeric(14,3) precision")
	}
	return r, nil
}

func cookingString(value *big.Rat) (string, error) {
	n := new(big.Int).Mul(value.Num(), cookingScale)
	negative := n.Sign() < 0
	n.Abs(n)
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, value.Denom(), rem)
	if new(big.Int).Lsh(rem, 1).Cmp(value.Denom()) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if negative {
		q.Neg(q)
	}
	if !q.IsInt64() || new(big.Int).Abs(new(big.Int).Set(q)).Cmp(big.NewInt(99999999999999)) > 0 {
		return "", cookingError("quantity_overflow", "", "Calculated quantity exceeds supported precision")
	}
	i := q.Int64()
	if i < 0 {
		return fmt.Sprintf("-%d.%03d", -i/1000, -i%1000), nil
	}
	return fmt.Sprintf("%d.%03d", i/1000, i%1000), nil
}

func ratString(s string) (*big.Rat, error) { return ParseCostDecimal(s) }

func cloneRat(r *big.Rat) *big.Rat { return new(big.Rat).Set(r) }

func ingredientAmounts(recipe models.Recipe) []models.CookingIngredientAmount {
	rows := make([]models.CookingIngredientAmount, 0, len(recipe.RecipeIngredients))
	for _, ri := range recipe.RecipeIngredients {
		rows = append(rows, models.CookingIngredientAmount{
			RecipeIngredientID: ri.ID, IngredientID: ri.IngredientID,
			Name: ri.Ingredient.Name, Unit: strings.TrimSpace(ri.Unit),
			Quantity: strings.TrimSpace(ri.Quantity), Type: strings.TrimSpace(ri.Ingredient.Type),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].RecipeIngredientID < rows[j].RecipeIngredientID })
	return rows
}

func recipeSnapshot(recipe models.Recipe) models.CookingRecipeSnapshot {
	snapshot := models.CookingRecipeSnapshot{ID: recipe.ID, Name: recipe.Name, Ingredients: ingredientAmounts(recipe)}
	if recipe.PlannedYieldG != nil {
		v := string(*recipe.PlannedYieldG)
		snapshot.PlannedYieldG = &v
	}
	return snapshot
}

func recipeBaseRows(snapshot models.CookingRecipeSnapshot) []models.CookingIngredientAmount {
	rows := []models.CookingIngredientAmount{}
	for _, row := range snapshot.Ingredients {
		if strings.EqualFold(strings.TrimSpace(row.Type), "base") {
			if _, ok := massUnit(row.Unit); ok {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func massUnit(unit string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "gram", "grams":
		return "g", true
	case "kg", "kilogram", "kilograms":
		return "kg", true
	default:
		return "", false
	}
}

func amountInGrams(quantity, unit, field string) (*big.Rat, error) {
	r, err := cookingRat(quantity, field, false)
	if err != nil {
		return nil, err
	}
	if u, ok := massUnit(unit); ok && u == "kg" {
		r.Mul(r, big.NewRat(1000, 1))
		return r, nil
	}
	if u, ok := massUnit(unit); !ok || u != "g" {
		return nil, cookingError("unsupported_base_unit", field, "Base ingredient must use grams or kilograms")
	}
	return r, nil
}

func componentRecipeWeights(product models.Product, products map[uint]models.Product, recipes map[uint]models.Recipe) (map[uint]*big.Rat, error) {
	result := map[uint]*big.Rat{}
	if product.ProductType == "assortment" {
		if product.CompositionBasis == nil {
			return nil, cookingError("composition_unavailable", "product_id", "Assortment composition basis is missing")
		}
		switch *product.CompositionBasis {
		case "products":
			total := new(big.Rat)
			for _, component := range product.AssortmentComponents {
				weight, err := cookingRat(string(component.QuantityG), "assortment_components", false)
				if err != nil {
					return nil, err
				}
				total.Add(total, weight)
				source, ok := products[component.ComponentProductID]
				if !ok {
					return nil, cookingError("unknown_component_product", "assortment_components", "Assortment references an unknown component product")
				}
				if source.ProductType == "assortment" {
					return nil, cookingError("nested_assortment", "assortment_components", "Nested assortments cannot be planned for cooking")
				}
				if source.CostRecipeID == nil || *source.CostRecipeID == 0 {
					return nil, cookingError("recipe_missing", "assortment_components", "Every assortment component must reference a recipe")
				}
				if _, ok := recipes[*source.CostRecipeID]; !ok {
					return nil, cookingError("unknown_recipe", "assortment_components", "Assortment references an unknown recipe")
				}
				addRat(result, *source.CostRecipeID, weight)
			}
			if product.Package.NetWeightG == nil {
				return nil, cookingError("package_weight_missing", "package_id", "Assortment package net weight is required")
			}
			pkg, err := cookingRat(string(*product.Package.NetWeightG), "package.net_weight_g", false)
			if err != nil {
				return nil, err
			}
			if total.Cmp(pkg) != 0 {
				return nil, cookingError("assortment_weight_mismatch", "assortment_components", "Assortment component weights must equal package net weight")
			}
			return result, nil
		case "legacy_recipes":
			for _, component := range product.CostComponents {
				weight, err := cookingRat(string(component.QuantityG), "components", false)
				if err != nil {
					return nil, err
				}
				if _, ok := recipes[component.RecipeID]; !ok {
					return nil, cookingError("unknown_recipe", "components", "Assortment references an unknown recipe")
				}
				addRat(result, component.RecipeID, weight)
			}
			if len(result) == 0 {
				return nil, cookingError("composition_unavailable", "components", "Assortment recipe composition is empty")
			}
			return result, nil
		default:
			return nil, cookingError("composition_unavailable", "composition_basis", "Assortment composition basis is unsupported")
		}
	}
	rid := product.CostRecipeID
	if rid == nil {
		rid = product.RecipeID
	}
	if rid == nil || *rid == 0 {
		return nil, cookingError("recipe_missing", "product_id", "Standard product has no cooking recipe")
	}
	if _, ok := recipes[*rid]; !ok {
		return nil, cookingError("unknown_recipe", "product_id", "Product references an unknown recipe")
	}
	if product.Package.NetWeightG == nil {
		return nil, cookingError("package_weight_missing", "package_id", "Product package net weight is required")
	}
	weight, err := cookingRat(string(*product.Package.NetWeightG), "package.net_weight_g", false)
	if err != nil {
		return nil, err
	}
	result[*rid] = weight
	return result, nil
}

func addRat(m map[uint]*big.Rat, id uint, v *big.Rat) {
	if m[id] == nil {
		m[id] = new(big.Rat)
	}
	m[id].Add(m[id], v)
}

func BuildCookingPlan(orders []models.Order, products []models.Product, recipes []models.Recipe, extras []models.CookingExtraInput, baseSelections map[uint]uint) (models.CookingPlan, error) {
	productMap, recipeMap := map[uint]models.Product{}, map[uint]models.Recipe{}
	for _, p := range products {
		productMap[p.ID] = p
	}
	for _, r := range recipes {
		recipeMap[r.ID] = r
	}
	plan := models.CookingPlan{Flavors: []models.CookingFlavorPlan{}, Products: []models.CookingProductSnapshot{}, OrderLines: []models.CookingOrderLine{}, Issues: []models.CookingIssue{}}
	productIDs := map[uint]bool{}
	byRecipe := map[uint]*big.Rat{}
	for _, order := range orders {
		for _, item := range order.Items {
			if item.Quantity <= 0 {
				return plan, cookingError("invalid_quantity", "items.quantity", "Order quantity must be a positive integer")
			}
			p, ok := productMap[item.ProductID]
			if !ok {
				return plan, cookingError("unknown_product", "items.product_id", "Order references an unknown product")
			}
			weights, err := componentRecipeWeights(p, productMap, recipeMap)
			if err != nil {
				return plan, err
			}
			for rid, weight := range weights {
				addRat(byRecipe, rid, new(big.Rat).Mul(weight, big.NewRat(int64(item.Quantity), 1)))
			}
			productIDs[p.ID] = true
			plan.OrderLines = append(plan.OrderLines, models.CookingOrderLine{OrderID: order.ID, OrderItemID: item.ID, ProductID: p.ID, Quantity: item.Quantity})
		}
	}
	for _, extra := range extras {
		if _, ok := recipeMap[extra.RecipeID]; !ok {
			return plan, cookingError("unknown_recipe", "extras.recipe_id", "Extra input references an unknown recipe")
		}
		weight, err := cookingRat(extra.FinishedG, "extras.finished_g", true)
		if err != nil {
			return plan, err
		}
		addRat(byRecipe, extra.RecipeID, weight)
	}
	for id := range productIDs {
		p := productMap[id]
		weights, _ := componentRecipeWeights(p, productMap, recipeMap)
		components := make([]models.CookingComponent, 0, len(weights))
		for rid, weight := range weights {
			s, _ := cookingString(weight)
			components = append(components, models.CookingComponent{RecipeID: rid, QuantityG: s})
		}
		sort.Slice(components, func(i, j int) bool { return components[i].RecipeID < components[j].RecipeID })
		plan.Products = append(plan.Products, models.CookingProductSnapshot{ID: p.ID, Name: p.Name, NetWeightG: derefDecimal(p.Package.NetWeightG), ConfigurationVersion: p.ConfigurationVersion, Components: components})
	}
	sort.Slice(plan.Products, func(i, j int) bool { return plan.Products[i].ID < plan.Products[j].ID })
	ids := make([]uint, 0, len(byRecipe))
	for id := range byRecipe {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		r := recipeMap[id]
		snap := recipeSnapshot(r)
		bases := recipeBaseRows(snap)
		flavor := models.CookingFlavorPlan{RecipeID: id, Name: r.Name, RecipeSnapshot: snap, TargetFinishedG: "", Issues: []models.CookingIssue{}}
		target, _ := cookingString(byRecipe[id])
		flavor.TargetFinishedG = target
		if len(bases) == 1 {
			flavor.BaseRecipeIngredientID = bases[0].RecipeIngredientID
			flavor.RecipeSnapshot.BaseRecipeIngredientID = bases[0].RecipeIngredientID
		}
		if len(bases) != 1 {
			issue := models.CookingIssue{Code: "base_selection_required", Message: "Choose exactly one mass-based base ingredient", Field: "base_recipe_ingredient_id", RecipeID: id}
			flavor.Issues = append(flavor.Issues, issue)
			plan.Issues = append(plan.Issues, issue)
		}
		if chosen := baseSelections[id]; chosen != 0 {
			found := false
			for _, row := range bases {
				if row.RecipeIngredientID == chosen {
					found = true
					break
				}
			}
			if !found {
				return plan, cookingError("invalid_base_selection", "base_recipe_ingredient_id", "Selected base ingredient is not a mass-based base ingredient in this recipe")
			}
			flavor.BaseRecipeIngredientID = chosen
			flavor.RecipeSnapshot.BaseRecipeIngredientID = chosen
		}
		if r.PlannedYieldG == nil {
			issue := models.CookingIssue{Code: "planned_yield_missing", Message: "Recipe has no planned finished yield; suggested main ingredient weight cannot be calculated", Field: "planned_yield_g", RecipeID: id}
			flavor.Issues = append(flavor.Issues, issue)
			plan.Issues = append(plan.Issues, issue)
		} else if flavor.BaseRecipeIngredientID != 0 {
			yield, err := cookingRat(string(*r.PlannedYieldG), "planned_yield_g", false)
			if err != nil {
				return plan, err
			}
			baseRow := models.CookingIngredientAmount{}
			for _, row := range bases {
				if row.RecipeIngredientID == flavor.BaseRecipeIngredientID {
					baseRow = row
				}
			}
			base, err := amountInGrams(baseRow.Quantity, baseRow.Unit, "base.quantity")
			if err != nil {
				return plan, err
			}
			suggestion := new(big.Rat).Quo(new(big.Rat).Mul(byRecipe[id], base), yield)
			s, err := cookingString(suggestion)
			if err != nil {
				return plan, err
			}
			flavor.SuggestedMeatG = &s
		}
		plan.Flavors = append(plan.Flavors, flavor)
	}
	return plan, nil
}

func derefDecimal(value *models.Decimal) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func normalizedUnit(unit string) (string, *big.Rat, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "gram", "grams":
		return "g", big.NewRat(1, 1), true
	case "kg", "kilogram", "kilograms":
		return "g", big.NewRat(1000, 1), true
	case "mg", "milligram", "milligrams":
		return "g", big.NewRat(1, 1000), true
	case "ml", "milliliter", "milliliters", "millilitre", "millilitres":
		return "ml", big.NewRat(1, 1), true
	case "l", "liter", "liters", "litre", "litres":
		return "ml", big.NewRat(1000, 1), true
	default:
		return strings.ToLower(strings.TrimSpace(unit)), big.NewRat(1, 1), false
	}
}

type ingredientAggregate struct {
	id              uint
	name, unit, typ string
	quantity        *big.Rat
}

func scaleIngredients(recipe models.CookingRecipeSnapshot, baseID uint, meat *big.Rat) ([]models.CookingIngredientAmount, error) {
	aggs, err := scaledIngredientAggregates(recipe, baseID, meat)
	if err != nil {
		return nil, err
	}
	rows := make([]models.CookingIngredientAmount, 0, len(aggs))
	for _, a := range aggs {
		s, err := cookingString(a.quantity)
		if err != nil {
			return nil, err
		}
		rows = append(rows, models.CookingIngredientAmount{IngredientID: a.id, Name: a.name, Unit: a.unit, Quantity: s, Type: a.typ})
	}
	return rows, nil
}

func scaledIngredientAggregates(recipe models.CookingRecipeSnapshot, baseID uint, meat *big.Rat) ([]*ingredientAggregate, error) {
	var base *models.CookingIngredientAmount
	for i := range recipe.Ingredients {
		row := &recipe.Ingredients[i]
		if row.RecipeIngredientID == baseID {
			base = row
			break
		}
	}
	if base == nil {
		return nil, cookingError("base_selection_required", "base_recipe_ingredient_id", "Select a base ingredient before calculating marinade")
	}
	baseQty, err := amountInGrams(base.Quantity, base.Unit, "base.quantity")
	if err != nil {
		return nil, err
	}
	ratio := new(big.Rat).Quo(meat, baseQty)
	aggs := map[string]*ingredientAggregate{}
	for _, row := range recipe.Ingredients {
		if row.RecipeIngredientID == baseID {
			continue
		}
		if strings.EqualFold(row.Type, "packaging") || strings.EqualFold(row.Type, "electricity") || strings.EqualFold(row.Type, "labor") {
			continue
		}
		q, err := cookingRat(row.Quantity, "recipe_ingredients.quantity", true)
		if err != nil {
			return nil, err
		}
		unit, factor, known := normalizedUnit(row.Unit)
		scaled := new(big.Rat).Mul(q, ratio)
		scaled.Mul(scaled, factor)
		key := fmt.Sprintf("%d/%s/%s", row.IngredientID, unit, strings.ToLower(strings.TrimSpace(row.Type)))
		if aggs[key] == nil {
			aggs[key] = &ingredientAggregate{id: row.IngredientID, name: row.Name, unit: unit, typ: row.Type, quantity: new(big.Rat)}
		}
		aggs[key].quantity.Add(aggs[key].quantity, scaled)
		if !known {
			aggs[key].unit = strings.TrimSpace(row.Unit)
		}
	}
	keys := make([]string, 0, len(aggs))
	for k := range aggs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]*ingredientAggregate, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, aggs[k])
	}
	return rows, nil
}

func CalculateCookingPreparation(plan models.CookingPlan, input models.CookingPreparationInput) (models.CookingPreparation, error) {
	prepared, err := cookingRat(input.PreparedMeatG, "prepared_meat_g", true)
	if err != nil {
		return models.CookingPreparation{}, err
	}
	byID := map[uint]models.CookingFlavorPlan{}
	for _, f := range plan.Flavors {
		byID[f.RecipeID] = f
	}
	out := models.CookingPreparation{Flavors: []models.CookingPreparedFlavor{}, IngredientsTotal: []models.CookingIngredientAmount{}}
	seenFlavors := map[uint]bool{}
	allocated := new(big.Rat)
	totals := map[string]*ingredientAggregate{}
	for _, f := range input.Flavors {
		if seenFlavors[f.RecipeID] {
			return out, cookingError("duplicate_flavor", "flavors", "Each recipe must occur exactly once")
		}
		seenFlavors[f.RecipeID] = true
		planned, ok := byID[f.RecipeID]
		if !ok {
			return out, cookingError("unknown_recipe", "flavors.recipe_id", "Preparation references a recipe outside the current plan")
		}
		meat, err := cookingRat(f.ActualMeatG, "actual_meat_g", true)
		if err != nil {
			return out, err
		}
		allocated.Add(allocated, meat)
		baseID := f.BaseRecipeIngredientID
		if baseID == 0 {
			baseID = planned.BaseRecipeIngredientID
		}
		if baseID == 0 {
			return out, cookingError("base_selection_required", "base_recipe_ingredient_id", "Select exactly one mass-based base ingredient")
		}
		snapshot := planned.RecipeSnapshot
		validBase := false
		for _, row := range recipeBaseRows(snapshot) {
			if row.RecipeIngredientID == baseID {
				validBase = true
			}
		}
		if !validBase {
			return out, cookingError("invalid_base_selection", "base_recipe_ingredient_id", "Choose a mass-based base ingredient from this recipe")
		}
		snapshot.BaseRecipeIngredientID = baseID
		ingredients, err := scaleIngredients(snapshot, baseID, meat)
		if err != nil {
			return out, err
		}
		var expected *string
		if snapshot.PlannedYieldG != nil {
			yield, err := cookingRat(*snapshot.PlannedYieldG, "planned_yield_g", false)
			if err != nil {
				return out, err
			}
			baseRow := models.CookingIngredientAmount{}
			for _, r := range snapshot.Ingredients {
				if r.RecipeIngredientID == baseID {
					baseRow = r
				}
			}
			base, err := amountInGrams(baseRow.Quantity, baseRow.Unit, "base.quantity")
			if err != nil {
				return out, err
			}
			v, err := cookingString(new(big.Rat).Quo(new(big.Rat).Mul(meat, yield), base))
			if err != nil {
				return out, err
			}
			expected = &v
		}
		out.Flavors = append(out.Flavors, models.CookingPreparedFlavor{RecipeID: f.RecipeID, Name: planned.Name, ActualMeatG: f.ActualMeatG, ExpectedYieldG: expected, Ingredients: ingredients, RecipeSnapshot: snapshot})
		exactIngredients, err := scaledIngredientAggregates(snapshot, baseID, meat)
		if err != nil {
			return out, err
		}
		for _, row := range exactIngredients {
			key := fmt.Sprintf("%d/%s/%s", row.id, row.unit, strings.ToLower(row.typ))
			if totals[key] == nil {
				totals[key] = &ingredientAggregate{id: row.id, name: row.name, unit: row.unit, typ: row.typ, quantity: new(big.Rat)}
			}
			totals[key].quantity.Add(totals[key].quantity, row.quantity)
		}

		// The combined material list includes the actual main ingredient; per-flavor
		// ingredient rows remain the marinade only.
		for _, baseRow := range snapshot.Ingredients {
			if baseRow.RecipeIngredientID != baseID {
				continue
			}
			key := fmt.Sprintf("%d/g/%s", baseRow.IngredientID, strings.ToLower(baseRow.Type))
			if totals[key] == nil {
				totals[key] = &ingredientAggregate{id: baseRow.IngredientID, name: baseRow.Name, unit: "g", typ: baseRow.Type, quantity: new(big.Rat)}
			}
			totals[key].quantity.Add(totals[key].quantity, meat)
		}
	}
	if len(seenFlavors) != len(byID) {
		return out, cookingError("missing_flavor", "flavors", "Enter the actual main ingredient amount, including zero, for each flavor")
	}
	if allocated.Cmp(prepared) > 0 {
		return out, cookingError("prepared_meat_exceeded", "flavors.actual_meat_g", "Flavor allocations cannot exceed the prepared main ingredient")
	}
	out.PreparedMeatG, err = cookingString(prepared)
	if err != nil {
		return out, err
	}
	out.AllocatedMeatG, err = cookingString(allocated)
	if err != nil {
		return out, err
	}
	out.UnallocatedMeatG, err = cookingString(new(big.Rat).Sub(prepared, allocated))
	if err != nil {
		return out, err
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := totals[k]
		s, err := cookingString(a.quantity)
		if err != nil {
			return out, err
		}
		out.IngredientsTotal = append(out.IngredientsTotal, models.CookingIngredientAmount{IngredientID: a.id, Name: a.name, Unit: a.unit, Quantity: s, Type: a.typ})
	}
	return out, nil
}

func CalculateCookingPacking(products []models.CookingProductSnapshot, available map[uint]string, lines []models.CookingPackingLineInput) (models.CookingPackingPreview, error) {
	productMap := map[uint]models.CookingProductSnapshot{}
	for _, p := range products {
		productMap[p.ID] = p
	}
	out := models.CookingPackingPreview{Lines: append([]models.CookingPackingLineInput(nil), lines...), Consumption: []models.CookingComponent{}, Remaining: []models.CookingComponent{}}
	consumed := map[uint]*big.Rat{}
	remaining := map[uint]*big.Rat{}
	for id, value := range available {
		r, err := cookingRat(value, "available", true)
		if err != nil {
			return out, err
		}
		remaining[id] = r
	}
	for _, line := range lines {
		if line.Quantity < 0 {
			return out, cookingError("invalid_quantity", "lines.quantity", "Pack quantity must be a non-negative integer")
		}
		if line.Quantity > int(^uint(0)>>1)-out.TotalPacks {
			return out, cookingError("quantity_overflow", "lines.quantity", "Total pack quantity exceeds supported range")
		}
		p, ok := productMap[line.ProductID]
		if !ok {
			return out, cookingError("unknown_product", "lines.product_id", "Packing references an unknown product")
		}
		out.TotalPacks += line.Quantity
		for _, component := range p.Components {
			q, err := cookingRat(component.QuantityG, "components.quantity_g", false)
			if err != nil {
				return out, err
			}
			amount := new(big.Rat).Mul(q, big.NewRat(int64(line.Quantity), 1))
			addRat(consumed, component.RecipeID, amount)
			addRat(remaining, component.RecipeID, new(big.Rat).Neg(amount))
		}
	}
	for id, r := range remaining {
		if r.Sign() < 0 {
			return out, cookingError("stock_insufficient", "available", "Packing exceeds available usable output")
		}
		s, err := cookingString(r)
		if err != nil {
			return out, err
		}
		out.Remaining = append(out.Remaining, models.CookingComponent{RecipeID: id, QuantityG: s})
	}
	for id, r := range consumed {
		s, err := cookingString(r)
		if err != nil {
			return out, err
		}
		out.Consumption = append(out.Consumption, models.CookingComponent{RecipeID: id, QuantityG: s})
	}
	sort.Slice(out.Remaining, func(i, j int) bool { return out.Remaining[i].RecipeID < out.Remaining[j].RecipeID })
	sort.Slice(out.Consumption, func(i, j int) bool { return out.Consumption[i].RecipeID < out.Consumption[j].RecipeID })
	return out, nil
}
