package models

// CookingIssue describes a non-fatal plan or preparation limitation.
type CookingIssue struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	RecipeID  uint   `json:"recipe_id,omitempty"`
	ProductID uint   `json:"product_id,omitempty"`
}

type CookingExtraInput struct {
	RecipeID  uint   `json:"recipe_id"`
	FinishedG string `json:"finished_g"`
}

type CookingComponent struct {
	RecipeID  uint   `json:"recipe_id"`
	QuantityG string `json:"quantity_g"`
}

type CookingIngredientAmount struct {
	RecipeIngredientID uint   `json:"recipe_ingredient_id"`
	IngredientID       uint   `json:"ingredient_id"`
	Name               string `json:"name"`
	Unit               string `json:"unit"`
	Quantity           string `json:"quantity"`
	Type               string `json:"type"`
}

type CookingRecipeSnapshot struct {
	ID                     uint                      `json:"id"`
	Name                   string                    `json:"name"`
	PlannedYieldG          *string                   `json:"planned_yield_g"`
	BaseRecipeIngredientID uint                      `json:"base_recipe_ingredient_id,omitempty"`
	Ingredients            []CookingIngredientAmount `json:"ingredients"`
}

type CookingProductSnapshot struct {
	ID                   uint               `json:"id"`
	Name                 string             `json:"name"`
	NetWeightG           string             `json:"net_weight_g"`
	ConfigurationVersion uint               `json:"configuration_version"`
	Components           []CookingComponent `json:"components"`
}

type CookingOrderLine struct {
	OrderID     uint `json:"order_id"`
	OrderItemID uint `json:"order_item_id"`
	ProductID   uint `json:"product_id"`
	Quantity    int  `json:"quantity"`
}

type CookingFlavorPlan struct {
	RecipeID               uint                  `json:"recipe_id"`
	Name                   string                `json:"name"`
	TargetFinishedG        string                `json:"target_finished_g"`
	SuggestedMeatG         *string               `json:"suggested_meat_g"`
	BaseRecipeIngredientID uint                  `json:"base_recipe_ingredient_id,omitempty"`
	RecipeSnapshot         CookingRecipeSnapshot `json:"recipe_snapshot"`
	Issues                 []CookingIssue        `json:"issues"`
}

type CookingPlan struct {
	Flavors    []CookingFlavorPlan      `json:"flavors"`
	Products   []CookingProductSnapshot `json:"products"`
	OrderLines []CookingOrderLine       `json:"order_lines"`
	Issues     []CookingIssue           `json:"issues"`
}

type CookingPreparationFlavorInput struct {
	RecipeID               uint   `json:"recipe_id"`
	ActualMeatG            string `json:"actual_meat_g"`
	BaseRecipeIngredientID uint   `json:"base_recipe_ingredient_id,omitempty"`
}

type CookingPreparationInput struct {
	RunID           *uint                           `json:"run_id,omitempty"`
	RecipeID        *uint                           `json:"recipe_id,omitempty"`
	PreparedMeatG   string                          `json:"prepared_meat_g"`
	Flavors         []CookingPreparationFlavorInput `json:"flavors"`
	SourceSignature string                          `json:"source_signature,omitempty"`
	Version         uint                            `json:"version,omitempty"`
	OperationID     string                          `json:"operation_id,omitempty"`
}

type CookingPreparedFlavor struct {
	RecipeID       uint                      `json:"recipe_id"`
	Name           string                    `json:"name"`
	ActualMeatG    string                    `json:"actual_meat_g"`
	ExpectedYieldG *string                   `json:"expected_yield_g"`
	Ingredients    []CookingIngredientAmount `json:"ingredients"`
	RecipeSnapshot CookingRecipeSnapshot     `json:"recipe_snapshot"`
}

type CookingPreparation struct {
	PreparedMeatG    string                    `json:"prepared_meat_g"`
	AllocatedMeatG   string                    `json:"allocated_meat_g"`
	UnallocatedMeatG string                    `json:"unallocated_meat_g"`
	Flavors          []CookingPreparedFlavor   `json:"flavors"`
	IngredientsTotal []CookingIngredientAmount `json:"ingredients_total"`
}

type CookingPackingLineInput struct {
	ProductID uint `json:"product_id"`
	Quantity  int  `json:"quantity"`
}

type CookingPackingPreview struct {
	Lines       []CookingPackingLineInput `json:"lines"`
	TotalPacks  int                       `json:"total_packs"`
	Consumption []CookingComponent        `json:"consumption"`
	Remaining   []CookingComponent        `json:"remaining"`
}
