package models

import "time"

const (
	CostModeManual     = "manual"
	CostModeRecipe     = "recipe"
	CostModeAssortment = "assortment"
)

type CostIssue struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	RecipeID     uint   `json:"recipe_id,omitempty"`
	IngredientID uint   `json:"ingredient_id,omitempty"`
}
type CostPriceReference struct {
	ID           uint      `json:"id"`
	IngredientID uint      `json:"ingredient_id"`
	Date         time.Time `json:"date"`
}
type CostComponentEstimate struct {
	RecipeID      uint     `json:"recipe_id"`
	Name          string   `json:"name"`
	QuantityG     Decimal  `json:"quantity_g" swaggertype:"number"`
	PlannedYieldG *Decimal `json:"planned_yield_g" swaggertype:"number"`
	RecipeCost    *float64 `json:"recipe_cost"`
	CostPerGram   *float64 `json:"cost_per_gram"`
	ContentCost   *float64 `json:"content_cost"`
}
type CostEstimate struct {
	Status              string                  `json:"status"`
	Source              string                  `json:"source"`
	TotalCost           *float64                `json:"total_cost"`
	ContentCost         *float64                `json:"content_cost"`
	PackagingCost       *float64                `json:"packaging_cost"`
	KnownIngredientCost float64                 `json:"known_ingredient_cost"`
	CostPerGram         *float64                `json:"cost_per_gram"`
	PlannedYieldG       *Decimal                `json:"planned_yield_g,omitempty" swaggertype:"number"`
	PackageID           uint                    `json:"package_id,omitempty"`
	NetWeightG          *Decimal                `json:"net_weight_g,omitempty" swaggertype:"number"`
	Components          []CostComponentEstimate `json:"components"`
	Prices              []CostPriceReference    `json:"prices"`
	Issues              []CostIssue             `json:"issues"`
	CalculatedAt        time.Time               `json:"calculated_at"`
}
type CostSnapshot struct {
	Version      int           `json:"version"`
	Source       string        `json:"source"`
	Amount       float64       `json:"amount"`
	Estimate     *CostEstimate `json:"estimate,omitempty"`
	CalculatedAt time.Time     `json:"calculated_at"`
}
type ProductCostComponentInput struct {
	RecipeID  uint    `json:"recipe_id"`
	QuantityG Decimal `json:"quantity_g" swaggertype:"number"`
}
type ProductCostPreviewInput struct {
	CostMode     string                      `json:"cost_mode"`
	CostRecipeID *uint                       `json:"cost_recipe_id"`
	PackageID    uint                        `json:"package_id"`
	Cost         float64                     `json:"cost"`
	Components   []ProductCostComponentInput `json:"components"`
}
