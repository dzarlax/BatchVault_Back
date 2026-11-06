package models

import (
	"encoding/json"
	"time"
)

const (
	CostModeManual     = "manual"
	CostModeRecipe     = "recipe"
	CostModeAssortment = "assortment"
)

type CostIssue struct {
	Code               string `json:"code"`
	Message            string `json:"message"`
	RecipeID           uint   `json:"recipe_id,omitempty"`
	IngredientID       uint   `json:"ingredient_id,omitempty"`
	ComponentProductID uint   `json:"component_product_id,omitempty"`
}
type CostPriceReference struct {
	ID           uint      `json:"id"`
	IngredientID uint      `json:"ingredient_id"`
	Date         time.Time `json:"date"`
}
type CostComponentEstimate struct {
	ComponentProductID *uint    `json:"component_product_id,omitempty"`
	RecipeID           uint     `json:"recipe_id"`
	Name               string   `json:"name"`
	QuantityG          Decimal  `json:"quantity_g" swaggertype:"number"`
	PlannedYieldG      *Decimal `json:"planned_yield_g" swaggertype:"number"`
	RecipeCost         *float64 `json:"recipe_cost"`
	CostPerGram        *float64 `json:"cost_per_gram"`
	ContentCost        *float64 `json:"content_cost"`
}
type CompositionDetail struct {
	ComponentProductID *uint       `json:"component_product_id"`
	ProductName        string      `json:"product_name"`
	RecipeID           *uint       `json:"recipe_id"`
	RecipeName         *string     `json:"recipe_name"`
	QuantityG          Decimal     `json:"quantity_g"`
	Position           int         `json:"position"`
	Status             string      `json:"status"`
	CostPerGram        *float64    `json:"cost_per_gram"`
	ContentCost        *float64    `json:"content_cost"`
	Issues             []CostIssue `json:"issues"`
}

type CostProvenancePackage struct {
	ID         uint     `json:"id"`
	Name       string   `json:"name"`
	NetWeightG *Decimal `json:"net_weight_g"`
	UnitCost   *Decimal `json:"unit_cost"`
}
type CostProvenance struct {
	ProductType      string                `json:"product_type"`
	ProductName      string                `json:"product_name"`
	CompositionBasis *string               `json:"composition_basis"`
	Components       []CompositionDetail   `json:"components"`
	Package          CostProvenancePackage `json:"package"`
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
	CompositionDetails  []CompositionDetail     `json:"composition_details,omitempty"`
	Components          []CostComponentEstimate `json:"components"`
	Prices              []CostPriceReference    `json:"prices"`
	Issues              []CostIssue             `json:"issues"`
	CalculatedAt        time.Time               `json:"calculated_at"`
}
type CostSnapshot struct {
	Provenance   *CostProvenance `json:"provenance,omitempty"`
	Version      int             `json:"version"`
	Source       string          `json:"source"`
	Amount       float64         `json:"amount"`
	Estimate     *CostEstimate   `json:"estimate,omitempty"`
	CalculatedAt time.Time       `json:"calculated_at"`
}
type ProductCostComponentInput struct {
	RecipeID  uint    `json:"recipe_id"`
	QuantityG Decimal `json:"quantity_g" swaggertype:"number"`
}
type ProductCostPreviewInput struct {
	ProductType          *string                      `json:"product_type"`
	CompositionBasis     *string                      `json:"composition_basis"`
	RecipeID             json.RawMessage              `json:"recipe_id" swaggertype:"integer"`
	AssortmentComponents []ProductAssortmentComponent `json:"assortment_components"`
	CostMode             string                       `json:"cost_mode"`
	CostRecipeID         json.RawMessage              `json:"cost_recipe_id" swaggertype:"integer"`
	PackageID            uint                         `json:"package_id"`
	Cost                 float64                      `json:"cost"`
	Components           []ProductCostComponentInput  `json:"components"`
}
