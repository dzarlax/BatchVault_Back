package services

import (
	"math/big"
	"strings"
	"testing"

	"mobile-backend-go/models"
)

func dec(s string) *models.Decimal { v := models.Decimal(s); return &v }

func cookingFixture() ([]models.Order, []models.Product, []models.Recipe) {
	ingredient := func(id uint, name, typ string) models.Ingredient {
		return models.Ingredient{ID: id, Name: name, Type: typ}
	}
	recipe := func(id uint, name string, yield string, ingredients ...models.RecipeIngredient) models.Recipe {
		r := models.Recipe{ID: id, Name: name, RecipeIngredients: ingredients}
		if yield != "" {
			r.PlannedYieldG = dec(yield)
		}
		return r
	}
	classic := recipe(1, "Classic", "500", models.RecipeIngredient{ID: 11, IngredientID: 1, Quantity: "1", Unit: "kg", Ingredient: ingredient(1, "Meat", "base")}, models.RecipeIngredient{ID: 12, IngredientID: 2, Quantity: "20", Unit: "g", Ingredient: ingredient(2, "Salt", "spice")})
	spicy := recipe(2, "Spicy", "500", models.RecipeIngredient{ID: 21, IngredientID: 3, Quantity: "1", Unit: "kg", Ingredient: ingredient(3, "Meat", "base")}, models.RecipeIngredient{ID: 22, IngredientID: 4, Quantity: "20", Unit: "g", Ingredient: ingredient(4, "Salt", "spice")}, models.RecipeIngredient{ID: 23, IngredientID: 5, Quantity: "50", Unit: "ml", Ingredient: ingredient(5, "Soy", "liquid")}, models.RecipeIngredient{ID: 24, IngredientID: 6, Quantity: "10", Unit: "g", Ingredient: ingredient(6, "Chili", "spice")})
	teriyaki := recipe(3, "Teriyaki", "500", models.RecipeIngredient{ID: 31, IngredientID: 7, Quantity: "1", Unit: "kg", Ingredient: ingredient(7, "Meat", "base")})
	products := []models.Product{
		{ID: 101, Name: "Classic 100", ProductType: "standard", CostRecipeID: uintPtr(1), Package: models.Package{NetWeightG: dec("100")}, ConfigurationVersion: 1},
		{ID: 102, Name: "Spicy 100", ProductType: "standard", CostRecipeID: uintPtr(2), Package: models.Package{NetWeightG: dec("100")}, ConfigurationVersion: 1},
		{ID: 103, Name: "Teriyaki 100", ProductType: "standard", CostRecipeID: uintPtr(3), Package: models.Package{NetWeightG: dec("100")}, ConfigurationVersion: 1},
		{ID: 104, Name: "Assortment 150", ProductType: "assortment", CompositionBasis: stringPtr("products"), Package: models.Package{NetWeightG: dec("150")}, ConfigurationVersion: 4, AssortmentComponents: []models.ProductAssortmentComponent{{ComponentProductID: 101, QuantityG: "50"}, {ComponentProductID: 102, QuantityG: "50"}, {ComponentProductID: 103, QuantityG: "50"}}},
	}
	orders := []models.Order{
		{ID: 104, Items: []models.OrderItem{{ID: 1, ProductID: 101, Quantity: 4}, {ID: 2, ProductID: 102, Quantity: 2}}},
		{ID: 105, Items: []models.OrderItem{{ID: 3, ProductID: 101, Quantity: 2}, {ID: 4, ProductID: 103, Quantity: 2}}},
		{ID: 106, Items: []models.OrderItem{{ID: 5, ProductID: 104, Quantity: 2}}},
	}
	return orders, products, []models.Recipe{classic, spicy, teriyaki}
}

func uintPtr(v uint) *uint       { return &v }
func stringPtr(v string) *string { return &v }

func TestBuildCookingPlanAggregatesOrderAndAssortmentWeightsWithoutPrices(t *testing.T) {
	orders, products, recipes := cookingFixture()
	plan, err := BuildCookingPlan(orders, products, recipes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint]string{1: "700.000", 2: "300.000", 3: "300.000"}
	for _, f := range plan.Flavors {
		if f.TargetFinishedG != want[f.RecipeID] {
			t.Errorf("recipe %d target = %s, want %s", f.RecipeID, f.TargetFinishedG, want[f.RecipeID])
		}
		if f.SuggestedMeatG == nil || *f.SuggestedMeatG != map[uint]string{1: "1400.000", 2: "600.000", 3: "600.000"}[f.RecipeID] {
			t.Errorf("recipe %d suggested meat = %v", f.RecipeID, f.SuggestedMeatG)
		}
	}
	if len(plan.OrderLines) != 5 {
		t.Fatalf("order lines = %d", len(plan.OrderLines))
	}
	if len(plan.Products) != 4 {
		t.Fatalf("products = %d", len(plan.Products))
	}
}

func TestCookingPlanBaseSelectionYieldAndValidation(t *testing.T) {
	orders, products, recipes := cookingFixture()
	plan, err := BuildCookingPlan(nil, nil, recipes[:1], []models.CookingExtraInput{{RecipeID: 1, FinishedG: "1.001"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Flavors) != 1 || plan.Flavors[0].TargetFinishedG != "1.001" {
		t.Fatalf("fractional extra not preserved: %#v", plan.Flavors)
	}
	// A missing yield keeps the plan usable while making only the suggestion unknown.
	recipes[0].PlannedYieldG = nil
	plan, err = BuildCookingPlan(nil, nil, recipes[:1], []models.CookingExtraInput{{RecipeID: 1, FinishedG: "0"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Flavors[0].SuggestedMeatG != nil {
		t.Fatal("missing yield produced a meat suggestion")
	}
	if !hasIssue(plan.Flavors[0].Issues, "planned_yield_missing") {
		t.Fatal("missing yield issue not reported")
	}
	// Ambiguous base rows require an explicit valid selection.
	recipes[0].RecipeIngredients = append(recipes[0].RecipeIngredients, models.RecipeIngredient{ID: 13, IngredientID: 8, Quantity: "1", Unit: "kg", Ingredient: models.Ingredient{ID: 8, Name: "Second meat", Type: "base"}})
	plan, err = BuildCookingPlan(nil, nil, recipes[:1], []models.CookingExtraInput{{RecipeID: 1, FinishedG: "10"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Flavors[0].BaseRecipeIngredientID != 0 || !hasIssue(plan.Issues, "base_selection_required") {
		t.Fatal("ambiguous base was guessed")
	}
	plan, err = BuildCookingPlan(nil, nil, recipes[:1], []models.CookingExtraInput{{RecipeID: 1, FinishedG: "10"}}, map[uint]uint{1: 11})
	if err != nil || plan.Flavors[0].BaseRecipeIngredientID != 11 {
		t.Fatalf("valid base selection: plan=%#v err=%v", plan, err)
	}
	_, err = BuildCookingPlan(orders, products, recipes, []models.CookingExtraInput{{RecipeID: 999, FinishedG: "1"}}, nil)
	if err == nil {
		t.Fatal("unknown recipe accepted")
	}
	_, err = BuildCookingPlan(nil, nil, recipes, []models.CookingExtraInput{{RecipeID: 1, FinishedG: "-1"}}, nil)
	if err == nil {
		t.Fatal("negative extra accepted")
	}
	_, err = BuildCookingPlan(nil, nil, recipes, []models.CookingExtraInput{{RecipeID: 1, FinishedG: "0.0001"}}, nil)
	if err == nil {
		t.Fatal("excess precision accepted")
	}
}

func hasIssue(issues []models.CookingIssue, code string) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

func TestCalculateCookingPreparationScalesAndAggregatesByUnit(t *testing.T) {
	orders, products, recipes := cookingFixture()
	plan, err := BuildCookingPlan(orders, products, recipes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	actual := []models.CookingPreparationFlavorInput{{RecipeID: 1, ActualMeatG: "1500"}, {RecipeID: 2, ActualMeatG: "700"}, {RecipeID: 3, ActualMeatG: "700"}}
	out, err := CalculateCookingPreparation(plan, models.CookingPreparationInput{PreparedMeatG: "3000", Flavors: actual})
	if err != nil {
		t.Fatal(err)
	}
	if out.AllocatedMeatG != "2900.000" || out.UnallocatedMeatG != "100.000" {
		t.Fatalf("mass balance: %#v", out)
	}
	spicy := out.Flavors[1]
	got := map[string]string{}
	for _, i := range spicy.Ingredients {
		got[i.Name] = i.Quantity + " " + i.Unit
	}
	if got["Salt"] != "14.000 g" || got["Soy"] != "35.000 ml" || got["Chili"] != "7.000 g" {
		t.Fatalf("scaled ingredients = %#v", got)
	}
	if len(out.IngredientsTotal) == 0 {
		t.Fatal("missing aggregate ingredient totals")
	}
	_, err = CalculateCookingPreparation(plan, models.CookingPreparationInput{PreparedMeatG: "1", Flavors: []models.CookingPreparationFlavorInput{{RecipeID: 1, ActualMeatG: "1.001"}}})
	if err == nil {
		t.Fatal("over-allocation accepted")
	}
	_, err = CalculateCookingPreparation(plan, models.CookingPreparationInput{PreparedMeatG: "1", Flavors: []models.CookingPreparationFlavorInput{{RecipeID: 999, ActualMeatG: "0"}}})
	if err == nil {
		t.Fatal("unknown flavor accepted")
	}
}

func TestCookingIngredientNormalizationAndRounding(t *testing.T) {
	recipe := models.CookingRecipeSnapshot{ID: 8, Name: "Fractional", Ingredients: []models.CookingIngredientAmount{
		{RecipeIngredientID: 1, IngredientID: 1, Name: "Base", Type: "base", Unit: "g", Quantity: "3"},
		{RecipeIngredientID: 2, IngredientID: 2, Name: "Salt", Type: "spice", Unit: "kg", Quantity: "0.001"},
		{RecipeIngredientID: 3, IngredientID: 2, Name: "Salt", Type: "spice", Unit: "g", Quantity: "1"},
	}}
	rows, err := scaleIngredients(recipe, 1, bigRat("1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Quantity != "0.667" || rows[0].Unit != "g" {
		t.Fatalf("normalized rounded row = %#v", rows)
	}
	if _, err := cookingRat("99999999999999.999", "q", true); err == nil {
		t.Fatal("overflow accepted")
	}
}

func TestCalculateCookingPackingBalancesWeightsAndRejectsInsufficientOutput(t *testing.T) {
	products := []models.CookingProductSnapshot{
		{ID: 1, Components: []models.CookingComponent{{RecipeID: 1, QuantityG: "100"}}},
		{ID: 2, Components: []models.CookingComponent{{RecipeID: 2, QuantityG: "100"}}},
		{ID: 3, Components: []models.CookingComponent{{RecipeID: 3, QuantityG: "100"}}},
		{ID: 4, Components: []models.CookingComponent{{RecipeID: 1, QuantityG: "50"}, {RecipeID: 2, QuantityG: "50"}, {RecipeID: 3, QuantityG: "50"}}},
	}
	lines := []models.CookingPackingLineInput{{ProductID: 1, Quantity: 6}, {ProductID: 2, Quantity: 2}, {ProductID: 3, Quantity: 2}, {ProductID: 4, Quantity: 2}}
	out, err := CalculateCookingPacking(products, map[uint]string{1: "730", 2: "330", 3: "340"}, lines)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint]string{1: "30.000", 2: "30.000", 3: "40.000"}
	for _, r := range out.Remaining {
		if r.QuantityG != want[r.RecipeID] {
			t.Errorf("remaining recipe %d = %s", r.RecipeID, r.QuantityG)
		}
	}
	if out.TotalPacks != 12 {
		t.Errorf("total packs=%d", out.TotalPacks)
	}
	_, err = CalculateCookingPacking(products, map[uint]string{1: "1", 2: "0", 3: "0"}, lines)
	if err == nil {
		t.Fatal("overspend accepted")
	}
	_, err = CalculateCookingPacking(products, map[uint]string{1: "1", 2: "1", 3: "1"}, []models.CookingPackingLineInput{{ProductID: 1, Quantity: -1}})
	if err == nil {
		t.Fatal("negative pack count accepted")
	}
}

func TestCookingErrorsAreStructured(t *testing.T) {
	_, err := cookingRat("1.0001", "actual_meat_g", true)
	ce, ok := err.(*CookingCalculationError)
	if !ok || ce.Code != "invalid_quantity" || ce.Field != "actual_meat_g" {
		t.Fatalf("structured error = %#v", err)
	}
	if !strings.Contains(ce.Error(), "precision") {
		t.Fatalf("error message = %q", ce.Error())
	}
}

func bigRat(s string) *big.Rat { r, _ := ParseCostDecimal(s); return r }

func TestCookingPreparationRequiresUniqueCompleteFlavorsAndMassBase(t *testing.T) {
	orders, products, recipes := cookingFixture()
	plan, err := BuildCookingPlan(orders, products, recipes, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	valid := models.CookingPreparationInput{PreparedMeatG: "3000", Flavors: []models.CookingPreparationFlavorInput{{RecipeID: 1, ActualMeatG: "1500"}, {RecipeID: 2, ActualMeatG: "700"}, {RecipeID: 3, ActualMeatG: "700"}}}
	duplicate := valid
	duplicate.Flavors = append(append([]models.CookingPreparationFlavorInput{}, valid.Flavors...), valid.Flavors[0])
	if _, err = CalculateCookingPreparation(plan, duplicate); err == nil {
		t.Fatal("duplicate flavor accepted")
	}
	missing := valid
	missing.Flavors = missing.Flavors[:2]
	if _, err = CalculateCookingPreparation(plan, missing); err == nil {
		t.Fatal("missing flavor implicitly treated as zero")
	}
	invalid := valid
	invalid.Flavors = append([]models.CookingPreparationFlavorInput{}, valid.Flavors...)
	invalid.Flavors[0].BaseRecipeIngredientID = 12
	if _, err = CalculateCookingPreparation(plan, invalid); err == nil {
		t.Fatal("salt accepted as meat scaling basis")
	}
}
